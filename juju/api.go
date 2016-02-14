// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package juju

import (
	"fmt"
	"io"
	"time"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"
	"github.com/juju/utils/parallel"
	"gopkg.in/macaroon-bakery.v1/httpbakery"

	"github.com/juju/juju/api"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/jujuclient"
	"github.com/juju/juju/network"
)

var logger = loggo.GetLogger("juju.juju")

// The following are variables so that they can be
// changed by tests.
var (
	providerConnectDelay = 2 * time.Second
)

type apiStateCachedInfo struct {
	api.Connection
	// If cachedInfo is non-nil, it indicates that the info has been
	// newly retrieved, and should be cached in the config store.
	cachedInfo *api.Info
}

var errAborted = fmt.Errorf("aborted")

// NewAPIState creates an api.State object from an Environ
// This is almost certainly the wrong thing to do as it assumes
// the old admin password (stored as admin-secret in the config).
//
// TODO(axw) delete or move this; it's used only in tests.
func NewAPIState(user names.Tag, environ environs.Environ, dialOpts api.DialOpts) (api.Connection, error) {
	info, err := environAPIInfo(environ, user)
	if err != nil {
		return nil, err
	}
	st, err := api.Open(info, dialOpts)
	if err != nil {
		return nil, err
	}
	return st, nil
}

// NewAPIConnection returns an api.Connection connected to the Juju controller
// with the specified name, optionally scoped to the model with the specified
// name.
func NewAPIConnection(
	controllerName, modelName string,
	store jujuclient.ClientStore,
	bClient *httpbakery.Client,
) (api.Connection, error) {
	return newAPIClient(controllerName, modelName, store, bClient)
}

var defaultAPIOpen = api.Open

func newAPIClient(controllerName, modelName string, store jujuclient.ClientStore, bClient *httpbakery.Client) (api.Connection, error) {
	st, err := newAPIFromStore(controllerName, modelName, store, defaultAPIOpen, bClient)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return st, nil
}

// serverAddress returns the given string address:port as network.HostPort.
var serverAddress = func(hostPort string) (network.HostPort, error) {
	addrConnectedTo, err := network.ParseHostPorts(hostPort)
	if err != nil {
		// Should never happen, since we've just connected with it.
		return network.HostPort{}, errors.Annotatef(err, "invalid API address %q", hostPort)
	}
	return addrConnectedTo[0], nil
}

// newAPIFromStore implements the bulk of NewAPIConnection
// but is separate for testing purposes.
func newAPIFromStore(
	controllerName, modelName string,
	store jujuclient.ClientStore,
	apiOpen api.OpenFunc, bClient *httpbakery.Client,
) (api.Connection, error) {
	// Try to connect to the API concurrently using two different
	// possible sources of truth for the API endpoint. Our
	// preference is for the API endpoint cached in the API info,
	// because we know that without needing to access any remote
	// provider. However, the addresses stored there may no longer
	// be current (and the network connection may take a very long
	// time to time out) so we also try to connect using information
	// found from the provider. We only start to make that
	// connection after some suitable delay, so that in the
	// hopefully usual case, we will make the connection to the API
	// and never hit the provider.
	chooseError := func(err0, err1 error) error {
		if err0 == nil {
			return err1
		}
		if errorImportance(err0) < errorImportance(err1) {
			err0, err1 = err1, err0
		}
		logger.Warningf("discarding API open error: %v", err1)
		return err0
	}
	try := parallel.NewTry(0, chooseError)

	controllerDetails, err := store.ControllerByName(controllerName)
	if err != nil {
		return nil, errors.Trace(err)
	}

	var modelDetails *jujuclient.ModelDetails
	if modelName != "" {
		modelDetails, err = store.ModelByName(controllerName, modelName)
		if err != nil {
			return nil, errors.Trace(err)
		}
	}

	// Get the account details for the controller. There may be no account,
	// in which case we'll try a macaroon login.
	var accountDetails *jujuclient.AccountDetails
	accountName, err := store.CurrentAccount(controllerName)
	if err == nil {
		accountDetails, err = store.AccountByName(controllerName, accountName)
		if err != nil && !errors.IsNotFound(err) {
			return nil, errors.Trace(err)
		}
	} else if !errors.IsNotFound(err) {
		return nil, errors.Trace(err)
	}

	var delay time.Duration
	if len(controllerDetails.APIEndpoints) > 0 {
		logger.Debugf(
			"trying cached API connection settings - endpoints %v",
			controllerDetails.APIEndpoints,
		)
		try.Start(func(stop <-chan struct{}) (io.Closer, error) {
			return apiInfoConnect(
				controllerDetails, accountDetails, modelDetails,
				apiOpen, stop, bClient,
			)
		})
		// Delay the config connection until we've spent
		// some time trying to connect to the cached info.
		delay = providerConnectDelay
	} else {
		logger.Debugf("no cached API connection settings found")
	}
	if false {
		try.Start(func(stop <-chan struct{}) (io.Closer, error) {
			cfg, err := getConfig(store)
			if err != nil {
				return nil, err
			}
			_ = cfg
			_ = delay
			// TODO(axw) support connecting with bootstrap config
			//return apiConfigConnect(cfg, apiOpen, stop, delay, environInfoUserTag(info))
			return nil, errors.NotImplementedf("connecting with bootstrap config")
		})
	}

	try.Close()
	val0, err := try.Result()
	if err != nil {
		if ierr, ok := err.(*infoConnectError); ok {
			// lose error encapsulation:
			err = ierr.error
		}
		return nil, err
	}

	// Update API addresses if they've changed. Error is non-fatal.
	st := val0.(api.Connection)
	addrConnectedTo, err := serverAddress(st.Addr())
	if err != nil {
		return nil, err
	}
	if localerr := cacheChangedAPIInfo(
		store, controllerName, controllerDetails,
		st.APIHostPorts(), addrConnectedTo,
	); localerr != nil {
		logger.Warningf("cannot cache API addresses: %v", localerr)
	}
	return st, nil
}

func errorImportance(err error) int {
	if err == nil {
		return 0
	}
	if errors.IsNotFound(err) {
		// An error from an actual connection attempt
		// is more interesting than the fact that there's
		// no environment info available.
		return 2
	}
	if _, ok := err.(*infoConnectError); ok {
		// A connection to a potentially stale cached address
		// is less important than a connection from fresh info.
		return 1
	}
	return 3
}

type infoConnectError struct {
	error
}

// apiInfoConnect looks for endpoint on the given environment and
// tries to connect to it, sending the result on the returned channel.
func apiInfoConnect(
	controllerDetails *jujuclient.ControllerDetails,
	accountDetails *jujuclient.AccountDetails,
	modelDetails *jujuclient.ModelDetails,
	apiOpen api.OpenFunc,
	stop <-chan struct{},
	bClient *httpbakery.Client,
) (api.Connection, error) {

	apiInfo := &api.Info{
		Addrs:  controllerDetails.APIEndpoints,
		CACert: controllerDetails.CACert,
	}
	if modelDetails != nil {
		apiInfo.ModelTag = names.NewModelTag(modelDetails.ModelUUID)
	}
	if accountDetails != nil {
		apiInfo.Tag = names.NewUserTag(accountDetails.User)
		apiInfo.Password = accountDetails.Password
	} else {
		apiInfo.UseMacaroons = true
	}

	logger.Infof("logging in with %+v", apiInfo)
	logger.Infof("connecting to API addresses: %v", controllerDetails.APIEndpoints)
	dialOpts := api.DefaultDialOpts()
	dialOpts.BakeryClient = bClient
	st, err := apiOpen(apiInfo, dialOpts)
	if err != nil {
		return nil, &infoConnectError{err}
	}
	return st, nil
}

// apiConfigConnect looks for configuration info on the given environment,
// and tries to use an Environ constructed from that to connect to
// its endpoint. It only starts the attempt after the given delay,
// to allow the faster apiInfoConnect to hopefully succeed first.
// It returns nil if there was no configuration information found.
func apiConfigConnect(cfg *config.Config, apiOpen api.OpenFunc, stop <-chan struct{}, delay time.Duration, user names.Tag) (api.Connection, error) {
	select {
	case <-time.After(delay):
	case <-stop:
		return nil, errAborted
	}
	environ, err := environs.New(cfg)
	if err != nil {
		return nil, err
	}
	apiInfo, err := environAPIInfo(environ, user)
	if err != nil {
		return nil, err
	}

	st, err := apiOpen(apiInfo, api.DefaultDialOpts())
	// TODO(rog): handle errUnauthorized when the API handles passwords.
	if err != nil {
		return nil, err
	}
	return apiStateCachedInfo{st, apiInfo}, nil
}

// getConfig looks for configuration info on the given environment
func getConfig(client jujuclient.ClientStore) (*config.Config, error) {
	// TODO(axw) get bootstrap config from client store
	return nil, errors.NotFoundf("bootstrap config")
}

func environAPIInfo(environ environs.Environ, user names.Tag) (*api.Info, error) {
	config := environ.Config()
	password := config.AdminSecret()
	info, err := environs.APIInfo(environ)
	if err != nil {
		return nil, err
	}
	info.Tag = user
	info.Password = password
	if info.Tag == nil {
		info.UseMacaroons = true
	}
	return info, nil
}

// cacheAPIInfo updates the client store with the provided apiInfo,
// assuming we've just successfully connected to the API server.
func cacheAPIInfo(
	st api.Connection,
	store jujuclient.ClientStore,
	controllerName string,
	controllerDetails *jujuclient.ControllerDetails,
	apiInfo *api.Info,
) (err error) {
	defer errors.DeferredAnnotatef(&err, "failed to cache API credentials")
	hostPorts, err := network.ParseHostPorts(apiInfo.Addrs...)
	if err != nil {
		return errors.Annotatef(err, "invalid API addresses %v", apiInfo.Addrs)
	}
	addrConnectedTo, err := network.ParseHostPorts(st.Addr())
	if err != nil {
		// Should never happen, since we've just connected with it.
		return errors.Annotatef(err, "invalid API address %q", st.Addr())
	}
	addrs, hostnames, addrsChanged := PrepareEndpointsForCaching(
		controllerDetails, [][]network.HostPort{hostPorts}, addrConnectedTo[0],
	)
	if !addrsChanged {
		return nil
	}
	controllerDetails.APIEndpoints = addrs
	controllerDetails.Servers = hostnames
	return errors.Annotate(
		store.UpdateController(controllerName, *controllerDetails),
		"could not update controller details",
	)
}

var resolveOrDropHostnames = network.ResolveOrDropHostnames

// PrepareEndpointsForCaching performs the necessary operations on the
// given API hostPorts so they are suitable for caching in the client
// store in the controller details, taking into account the addrConnectedTo
// and the existing controller details:
//
// 1. Collapses hostPorts into a single slice.
// 2. Filters out machine-local and link-local addresses.
// 3. Removes any duplicates
// 4. Call network.SortHostPorts() on the list, respecing prefer-ipv6
// flag.
// 5. Puts the addrConnectedTo on top.
// 6. Compares the result against controllerDetails.Servers.
// 7. If the addresses differ, call network.ResolveOrDropHostnames()
// on the list and perform all steps again from step 1.
// 8. Compare the list of resolved addresses against the cached
// controllerDetails.APIEndpoints, and if changed return both addresses
// and hostnames as strings (so they can be updated in the client sotre)
// and set haveChanged to true.
// 9. If the hostnames haven't changed, return two empty slices and set
// haveChanged to false. No DNS resolution is performed to save time.
//
// This is used right after bootstrap to cache the initial API
// endpoints, as well as on each API connection to verify if the
// cached endpoints need updating.
func PrepareEndpointsForCaching(
	controllerDetails *jujuclient.ControllerDetails,
	hostPorts [][]network.HostPort,
	addrConnectedTo network.HostPort,
) (addresses, hostnames []string, haveChanged bool) {

	processHostPorts := func(allHostPorts [][]network.HostPort) []network.HostPort {
		collapsedHPs := network.CollapseHostPorts(allHostPorts)
		filteredHPs := network.FilterUnusableHostPorts(collapsedHPs)
		uniqueHPs := network.DropDuplicatedHostPorts(filteredHPs)

		// Sort the result to prefer public IPs on top (when prefer-ipv6
		// is true, IPv6 addresses of the same scope will come before IPv4
		// ones).
		//
		// TODO(axw) we were relying on bootstrap config for this, but we
		// really shouldn't if it applies to each API connection. Do we
		// even need it anymore?
		const preferIPv6 = false
		network.SortHostPorts(uniqueHPs, preferIPv6)

		if addrConnectedTo.Value != "" {
			return network.EnsureFirstHostPort(addrConnectedTo, uniqueHPs)
		}
		// addrConnectedTo can be empty only right after bootstrap.
		return uniqueHPs
	}

	apiHosts := processHostPorts(hostPorts)
	hostsStrings := network.HostPortsToStrings(apiHosts)
	needResolving := false

	// Verify if the unresolved addresses have changed.
	if len(apiHosts) > 0 && len(controllerDetails.Servers) > 0 {
		if addrsChanged(hostsStrings, controllerDetails.Servers) {
			logger.Debugf(
				"API hostnames changed from %v to %v - resolving hostnames",
				controllerDetails.Servers, hostsStrings,
			)
			needResolving = true
		}
	} else if len(apiHosts) > 0 {
		// No cached hostnames, most likely right after bootstrap.
		logger.Debugf("API hostnames %v - resolving hostnames", hostsStrings)
		needResolving = true
	}
	if !needResolving {
		// We're done - nothing changed.
		logger.Debugf("API hostnames unchanged - not resolving")
		return nil, nil, false
	}
	// Perform DNS resolution and check against APIEndpoints.Addresses.
	resolved := resolveOrDropHostnames(apiHosts)
	apiAddrs := processHostPorts([][]network.HostPort{resolved})
	addrsStrings := network.HostPortsToStrings(apiAddrs)
	if len(apiAddrs) > 0 && len(controllerDetails.APIEndpoints) > 0 {
		if addrsChanged(addrsStrings, controllerDetails.APIEndpoints) {
			logger.Infof(
				"API addresses changed from %v to %v",
				controllerDetails.APIEndpoints, addrsStrings,
			)
			return addrsStrings, hostsStrings, true
		}
	} else if len(apiAddrs) > 0 {
		// No cached addresses, most likely right after bootstrap.
		logger.Infof("new API addresses to cache %v", addrsStrings)
		return addrsStrings, hostsStrings, true
	}
	// No changes.
	logger.Debugf("API addresses unchanged")
	return nil, nil, false
}

// cacheChangedAPIInfo updates the client store with the controller addresses
// if they have changed.
func cacheChangedAPIInfo(
	controllerStore jujuclient.ControllerStore,
	controllerName string, controllerDetails *jujuclient.ControllerDetails,
	hostPorts [][]network.HostPort, addrConnectedTo network.HostPort,
) error {
	addrs, hosts, addrsChanged := PrepareEndpointsForCaching(controllerDetails, hostPorts, addrConnectedTo)
	var changed bool
	if addrsChanged {
		controllerDetails.APIEndpoints = addrs
		controllerDetails.Servers = hosts
		changed = true
	}
	if !changed {
		return nil
	}
	if err := controllerStore.UpdateController(controllerName, *controllerDetails); err != nil {
		return errors.Trace(err)
	}
	logger.Infof("updated API addresses for controller %q: %v", controllerName, addrs)
	return nil
}

// addrsChanged returns true iff the two
// slices are not equal. Order is important.
func addrsChanged(a, b []string) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}
