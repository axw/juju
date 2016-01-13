// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/utils"
	"github.com/juju/utils/featureflag"
	"gopkg.in/juju/charm.v6-unstable"
	"launchpad.net/gnuflag"

	apiblock "github.com/juju/juju/api/block"
	"github.com/juju/juju/apiserver"
	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/cmd/envcmd"
	"github.com/juju/juju/cmd/juju/block"
	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/configstore"
	"github.com/juju/juju/feature"
	"github.com/juju/juju/instance"
	"github.com/juju/juju/juju"
	"github.com/juju/juju/juju/osenv"
	"github.com/juju/juju/network"
	"github.com/juju/juju/provider"
	"github.com/juju/juju/version"
)

// provisionalProviders is the names of providers that are hidden behind
// feature flags.
var provisionalProviders = map[string]string{
	"vsphere": feature.VSphereProvider,
}

const bootstrapDoc = `
bootstrap starts a new environment of the current type (it will return an error
if the environment has already been bootstrapped).  Bootstrapping an environment
will provision a new machine in the environment and run the juju state server on
that machine.

If constraints are specified in the bootstrap command, they will apply to the
machine provisioned for the juju state server.  They will also be set as default
constraints on the environment for all future machines, exactly as if the
constraints were set with juju set-constraints.

It is possible to override constraints and the automatic machine selection
algorithm by using the "--to" flag. The value associated with "--to" is a
"placement directive", which tells Juju how to identify the first machine to use.
For more information on placement directives, see "juju help placement".

Bootstrap initialises the cloud environment synchronously and displays information
about the current installation steps.  The time for bootstrap to complete varies
across cloud providers from a few seconds to several minutes.  Once bootstrap has
completed, you can run other juju commands against your environment. You can change
the default timeout and retry delays used during the bootstrap by changing the
following settings in your environments.yaml (all values represent number of seconds):

    # How long to wait for a connection to the state server.
    bootstrap-timeout: 600 # default: 10 minutes
    # How long to wait between connection attempts to a state server address.
    bootstrap-retry-delay: 5 # default: 5 seconds
    # How often to refresh state server addresses from the API server.
    bootstrap-addresses-delay: 10 # default: 10 seconds

Private clouds may need to specify their own custom image metadata, and
possibly upload Juju tools to cloud storage if no outgoing Internet access is
available. In this case, use the --metadata-source parameter to point
bootstrap to a local directory from which to upload tools and/or image
metadata.

If agent-version is specifed, this is the default tools version to use when running the Juju agents.
Only the numeric version is relevant. To enable ease of scripting, the full binary version
is accepted (eg 1.24.4-trusty-amd64) but only the numeric version (eg 1.24.4) is used.
An alias for bootstrapping Juju with the exact same version as the client is to use the
--no-auto-upgrade parameter.

See Also:
   juju help switch
   juju help constraints
   juju help set-constraints
   juju help placement
`

func newBootstrapCommand() cmd.Command {
	return envcmd.Wrap(&bootstrapCommand{})
}

// bootstrapCommand is responsible for launching the first machine in a juju
// environment, and setting up everything necessary to continue working.
type bootstrapCommand struct {
	envcmd.EnvCommandBase
	Constraints           constraints.Value
	UploadTools           bool
	Series                []string
	seriesOld             []string
	MetadataSource        string
	Placement             string
	KeepBrokenEnvironment bool
	NoAutoUpgrade         bool
	AgentVersionParam     string
	AgentVersion          *version.Number

	ControllerName string
	CredentialName string
	Cloud          string
	Region         string
}

func (c *bootstrapCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "bootstrap",
		Purpose: "start up an environment from scratch",
		Doc:     bootstrapDoc,
	}
}

func (c *bootstrapCommand) SetFlags(f *gnuflag.FlagSet) {
	f.Var(constraints.ConstraintsValue{Target: &c.Constraints}, "constraints", "set environment constraints")
	f.BoolVar(&c.UploadTools, "upload-tools", false, "upload local version of tools before bootstrapping")
	f.Var(newSeriesValue(nil, &c.Series), "upload-series", "upload tools for supplied comma-separated series list (OBSOLETE)")
	f.Var(newSeriesValue(nil, &c.seriesOld), "series", "see --upload-series (OBSOLETE)")
	f.StringVar(&c.MetadataSource, "metadata-source", "", "local path to use as tools and/or metadata source")
	f.StringVar(&c.Placement, "to", "", "a placement directive indicating an instance to bootstrap")
	f.BoolVar(&c.KeepBrokenEnvironment, "keep-broken", false, "do not destroy the environment if bootstrap fails")
	f.BoolVar(&c.NoAutoUpgrade, "no-auto-upgrade", false, "do not upgrade to newer tools on first bootstrap")
	f.StringVar(&c.AgentVersionParam, "agent-version", "", "the version of tools to initially use for Juju agents")
	f.StringVar(&c.CredentialName, "credential", "", "the credentials to use when bootstrapping")
}

func (c *bootstrapCommand) Init(args []string) (err error) {
	if len(c.Series) > 0 && !c.UploadTools {
		return fmt.Errorf("--upload-series requires --upload-tools")
	}
	if len(c.seriesOld) > 0 && !c.UploadTools {
		return fmt.Errorf("--series requires --upload-tools")
	}
	if len(c.Series) > 0 && len(c.seriesOld) > 0 {
		return fmt.Errorf("--upload-series and --series can't be used together")
	}
	if c.AgentVersionParam != "" && c.UploadTools {
		return fmt.Errorf("--agent-version and --upload-tools can't be used together")
	}
	if c.AgentVersionParam != "" && c.NoAutoUpgrade {
		return fmt.Errorf("--agent-version and --no-auto-upgrade can't be used together")
	}

	// Parse the placement directive. Bootstrap currently only
	// supports provider-specific placement directives.
	if c.Placement != "" {
		_, err = instance.ParsePlacement(c.Placement)
		if err != instance.ErrPlacementScopeMissing {
			// We only support unscoped placement directives for bootstrap.
			return fmt.Errorf("unsupported bootstrap placement directive %q", c.Placement)
		}
	}
	if c.NoAutoUpgrade {
		vers := version.Current
		c.AgentVersion = &vers
	} else if c.AgentVersionParam != "" {
		if vers, err := version.ParseBinary(c.AgentVersionParam); err == nil {
			c.AgentVersion = &vers.Number
		} else if vers, err := version.Parse(c.AgentVersionParam); err == nil {
			c.AgentVersion = &vers
		} else {
			return err
		}
	}
	if c.AgentVersion != nil && (c.AgentVersion.Major != version.Current.Major || c.AgentVersion.Minor != version.Current.Minor) {
		return fmt.Errorf("requested agent version major.minor mismatch")
	}

	// The user must specify two positional arguments: the controller name,
	// and the cloud name (optionally with region specified).
	if len(args) < 2 {
		return errors.New("controller name and cloud name are required")
	}
	c.ControllerName = args[0]
	c.Cloud = args[1]
	if i := strings.IndexRune(c.Cloud, '/'); i > 0 {
		c.Cloud, c.Region = c.Cloud[:i], c.Cloud[i+1:]
	}
	return cmd.CheckEmpty(args[2:])
}

type seriesValue struct {
	*cmd.StringsValue
}

// newSeriesValue is used to create the type passed into the gnuflag.FlagSet Var function.
func newSeriesValue(defaultValue []string, target *[]string) *seriesValue {
	v := seriesValue{(*cmd.StringsValue)(target)}
	*(v.StringsValue) = defaultValue
	return &v
}

// Implements gnuflag.Value Set.
func (v *seriesValue) Set(s string) error {
	if err := v.StringsValue.Set(s); err != nil {
		return err
	}
	for _, name := range *(v.StringsValue) {
		if !charm.IsValidSeries(name) {
			v.StringsValue = nil
			return fmt.Errorf("invalid series name %q", name)
		}
	}
	return nil
}

// bootstrap functionality that Run calls to support cleaner testing
type BootstrapInterface interface {
	EnsureNotBootstrapped(env environs.Environ) error
	Bootstrap(ctx environs.BootstrapContext, environ environs.Environ, args bootstrap.BootstrapParams) error
}

type bootstrapFuncs struct{}

func (b bootstrapFuncs) EnsureNotBootstrapped(env environs.Environ) error {
	return bootstrap.EnsureNotBootstrapped(env)
}

func (b bootstrapFuncs) Bootstrap(ctx environs.BootstrapContext, env environs.Environ, args bootstrap.BootstrapParams) error {
	return bootstrap.Bootstrap(ctx, env, args)
}

var getBootstrapFuncs = func() BootstrapInterface {
	return &bootstrapFuncs{}
}

// Run connects to the environment specified on the command line and bootstraps
// a juju in that environment if none already exists. If there is as yet no environments.yaml file,
// the user is informed how to create one.
func (c *bootstrapCommand) Run(ctx *cmd.Context) (resultErr error) {
	bootstrapFuncs := getBootstrapFuncs()

	if len(c.seriesOld) > 0 {
		fmt.Fprintln(ctx.Stderr, "Use of --series is obsolete. --upload-tools now expands to all supported series of the same operating system.")
	}
	if len(c.Series) > 0 {
		fmt.Fprintln(ctx.Stderr, "Use of --upload-series is obsolete. --upload-tools now expands to all supported series of the same operating system.")
	}

	// Get the cloud definition identified by c.Cloud. If c.Cloud does not
	// identify a cloud in clouds.yaml, but is the name of a provider, we
	// synthesise a Cloud structure with a single region and no auth-types.
	cloud, err := c.getCloud()
	if errors.IsNotFound(err) {
		ctx.Verbosef("cloud %q not found, trying as a provider name", c.Cloud)
		_, err := environs.Provider(c.Cloud)
		if errors.IsNotFound(err) {
			return errors.NotFoundf("cloud %s", c.Cloud)
		} else if err != nil {
			return errors.Trace(err)
		}
		cloud = &jujucloud.Cloud{
			Type: c.Cloud,
			Regions: map[string]jujucloud.Region{
				c.Cloud: jujucloud.Region{},
			},
		}
	} else if err != nil {
		return errors.Trace(err)
	}
	if err := checkProviderType(cloud.Type); errors.IsNotFound(err) {
		// This error will get handled later.
	} else if err != nil {
		return errors.Trace(err)
	}

	// Get the credentials and region name. If there are no credentials,
	// we pass "empty" credentials to the provider so that it can obtain
	// them from environment variables, files, etc.
	credential, regionName, err := c.getCredentials()
	if errors.IsNotFound(err) {
		credential = jujucloud.EmptyCredentials{}
		regionName = c.Region
		if regionName == "" {
			regionName = c.Cloud
		}
	} else if err != nil {
		return errors.Trace(err)
	}
	region, ok := cloud.Regions[regionName]
	if !ok {
		var regionNames []string
		for name := range cloud.Regions {
			regionNames = append(regionNames, name)
		}
		return errors.NotFoundf(
			"region %q in cloud %q (expected one of %q)",
			regionName, c.Cloud, regionNames,
		)
	}

	// Create an environment config from the cloud and credentials.
	cfg, err := config.New(config.UseDefaults, map[string]interface{}{
		"type": cloud.Type,
		"name": c.ControllerName,
	})
	if err != nil {
		return errors.Annotate(err, "creating environment configuration")
	}
	store, err := configstore.Default()
	if err != nil {
		return errors.Trace(err)
	}
	environ, err := environs.Prepare(envcmd.BootstrapContext(ctx), store, environs.PrepareForBootstrapParams{
		Config:        cfg,
		CloudRegion:   regionName,
		CloudEndpoint: region.Endpoint,
		Credentials:   credential,
	})
	if err != nil {
		return errors.Annotate(err, "preparing bootstrap environment configuration")
	}

	// If we error out for any reason, clean up the environment.
	defer func() {
		if resultErr != nil {
			if c.KeepBrokenEnvironment {
				logger.Warningf(`
bootstrap failed but --keep-broken was specified so environment is not being destroyed.
When you are finished diagnosing the problem, remember to run juju destroy-environment --force
to clean up the environment.`[1:])
			} else {
				handleBootstrapError(ctx, resultErr, func() {
					// TODO(axw) copy logic that was in common.go.
					// Don't destroy if env was previously bootstrapped.
					environ.Destroy()
				})
			}
		}
	}()

	// Block interruption during bootstrap. Providers may also
	// register for interrupt notification so they can exit early.
	interrupted := make(chan os.Signal, 1)
	defer close(interrupted)
	ctx.InterruptNotify(interrupted)
	defer ctx.StopInterruptNotify(interrupted)
	go func() {
		for _ = range interrupted {
			ctx.Infof("Interrupt signalled: waiting for bootstrap to exit")
		}
	}()

	// If --metadata-source is specified, override the default tools metadata source so
	// SyncTools can use it, and also upload any image metadata.
	var metadataDir string
	if c.MetadataSource != "" {
		metadataDir = ctx.AbsPath(c.MetadataSource)
	}

	// TODO (wallyworld): 2013-09-20 bug 1227931
	// We can set a custom tools data source instead of doing an
	// unnecessary upload.
	if environ.Config().Type() == provider.Local {
		c.UploadTools = true
	}

	err = bootstrapFuncs.Bootstrap(envcmd.BootstrapContext(ctx), environ, bootstrap.BootstrapParams{
		Constraints:  c.Constraints,
		Placement:    c.Placement,
		UploadTools:  c.UploadTools,
		AgentVersion: c.AgentVersion,
		MetadataDir:  metadataDir,
	})
	if err != nil {
		return errors.Annotate(err, "failed to bootstrap environment")
	}

	c.SetEnvName(c.ControllerName)
	err = c.SetBootstrapEndpointAddress(environ)
	if err != nil {
		return errors.Annotate(err, "saving bootstrap endpoint address")
	}

	err = envcmd.SetCurrentEnvironment(ctx, c.ControllerName)
	if err != nil {
		return errors.Trace(err)
	}

	// To avoid race conditions when running scripted bootstraps, wait
	// for the state server's machine agent to be ready to accept commands
	// before exiting this bootstrap command.
	return c.waitForAgentInitialisation(ctx)
}

func (c *bootstrapCommand) getCloud() (*jujucloud.Cloud, error) {
	// First, read in cloud metadata and extract the cloud the user wants
	// to bootstrap. Ensure the cloud type is usable with the current client
	// configuration.
	clouds, _, err := jujucloud.PublicCloudMetadata(jujucloud.JujuPublicClouds())
	if err != nil {
		return nil, errors.Trace(err)
	}
	cloud, ok := clouds.Clouds[c.Cloud]
	if !ok {
		return nil, errors.NotFoundf("cloud %s", c.Cloud)
	}
	return &cloud, nil
}

func (c *bootstrapCommand) getCredentials() (_ jujucloud.Credential, region string, _ error) {
	credentialsData, err := ioutil.ReadFile(jujucloud.JujuCredentials())
	if os.IsNotExist(err) {
		return nil, "", errors.NotFoundf("credentials file")
	} else if err != nil {
		return nil, "", errors.Trace(err)
	}
	credentials, err := jujucloud.ParseCredentials(credentialsData)
	if err != nil {
		return nil, "", errors.Annotate(err, "parsing credentials")
	}
	cloudCredentials, ok := credentials.Credentials[c.Cloud]
	if !ok {
		return nil, "", errors.NotFoundf("credentials for cloud %q", c.Cloud)
	}
	credentialName := c.CredentialName
	if credentialName == "" {
		credentialName = cloudCredentials.DefaultCredential
	}
	credential, ok := cloudCredentials.AuthCredentials[credentialName]
	if !ok {
		return nil, "", errors.NotFoundf(
			"%q credential for cloud %q", credentialName, c.Cloud,
		)
	}
	regionName := c.Region
	if regionName == "" {
		regionName = cloudCredentials.DefaultRegion
	}
	if regionName == "" {
		regionName = c.Cloud
	}
	return credential, regionName, nil
}

var (
	bootstrapReadyPollDelay = 1 * time.Second
	bootstrapReadyPollCount = 60
	blockAPI                = getBlockAPI
)

// getBlockAPI returns a block api for listing blocks.
func getBlockAPI(c *envcmd.EnvCommandBase) (block.BlockListAPI, error) {
	root, err := c.NewAPIRoot()
	if err != nil {
		return nil, err
	}
	return apiblock.NewClient(root), nil
}

// waitForAgentInitialisation polls the bootstrapped state server with a read-only
// command which will fail until the state server is fully initialised.
// TODO(wallyworld) - add a bespoke command to maybe the admin facade for this purpose.
func (c *bootstrapCommand) waitForAgentInitialisation(ctx *cmd.Context) (err error) {
	attempts := utils.AttemptStrategy{
		Min:   bootstrapReadyPollCount,
		Delay: bootstrapReadyPollDelay,
	}
	var client block.BlockListAPI
	for attempt := attempts.Start(); attempt.Next(); {
		client, err = blockAPI(&c.EnvCommandBase)
		if err != nil {
			return err
		}
		_, err = client.List()
		client.Close()
		if err == nil {
			ctx.Infof("Bootstrap complete")
			return nil
		}
		// As the API server is coming up, it goes through a number of steps.
		// Initially the upgrade steps run, but the api server allows some
		// calls to be processed during the upgrade, but not the list blocks.
		// It is also possible that the underlying database causes connections
		// to be dropped as it is initialising, or reconfiguring. These can
		// lead to EOF or "connection is shut down" error messages. We skip
		// these too, hoping that things come back up before the end of the
		// retry poll count.
		errorMessage := err.Error()
		if strings.Contains(errorMessage, apiserver.UpgradeInProgressError.Error()) ||
			strings.HasSuffix(errorMessage, "EOF") ||
			strings.HasSuffix(errorMessage, "connection is shut down") {
			ctx.Infof("Waiting for API to become available")
			continue
		}
		return err
	}
	return err
}

// checkProviderType ensures the provider type is okay.
func checkProviderType(envType string) error {
	featureflag.SetFlagsFromEnvironment(osenv.JujuFeatureFlagEnvKey)
	flag, ok := provisionalProviders[envType]
	if ok && !featureflag.Enabled(flag) {
		msg := `the %q provider is provisional in this version of Juju. To use it anyway, set JUJU_DEV_FEATURE_FLAGS="%s" in your shell environment`
		return errors.Errorf(msg, envType, flag)
	}
	return nil
}

// handleBootstrapError is called to clean up if bootstrap fails.
func handleBootstrapError(ctx *cmd.Context, err error, cleanup func()) {
	ch := make(chan os.Signal, 1)
	ctx.InterruptNotify(ch)
	defer ctx.StopInterruptNotify(ch)
	defer close(ch)
	go func() {
		for _ = range ch {
			fmt.Fprintln(ctx.GetStderr(), "Cleaning up failed bootstrap")
		}
	}()
	cleanup()
}

var allInstances = func(environ environs.Environ) ([]instance.Instance, error) {
	return environ.AllInstances()
}

var prepareEndpointsForCaching = juju.PrepareEndpointsForCaching

// SetBootstrapEndpointAddress writes the API endpoint address of the
// bootstrap server into the connection information. This should only be run
// once directly after Bootstrap. It assumes that there is just one instance
// in the environment - the bootstrap instance.
func (c *bootstrapCommand) SetBootstrapEndpointAddress(environ environs.Environ) error {
	instances, err := allInstances(environ)
	if err != nil {
		return errors.Trace(err)
	}
	length := len(instances)
	if length == 0 {
		return errors.Errorf("found no instances, expected at least one")
	}
	if length > 1 {
		logger.Warningf("expected one instance, got %d", length)
	}
	bootstrapInstance := instances[0]
	cfg := environ.Config()
	info, err := envcmd.ConnectionInfoForName(c.ConnectionName())
	if err != nil {
		return errors.Annotate(err, "failed to get connection info")
	}

	// Don't use c.ConnectionEndpoint as it attempts to contact the state
	// server if no addresses are found in connection info.
	endpoint := info.APIEndpoint()
	netAddrs, err := bootstrapInstance.Addresses()
	if err != nil {
		return errors.Annotate(err, "failed to get bootstrap instance addresses")
	}
	apiPort := cfg.APIPort()
	apiHostPorts := network.AddressesWithPort(netAddrs, apiPort)
	addrs, hosts, addrsChanged := prepareEndpointsForCaching(
		info, [][]network.HostPort{apiHostPorts}, network.HostPort{},
	)
	if !addrsChanged {
		// Something's wrong we already have cached addresses?
		return errors.Annotate(err, "cached API endpoints unexpectedly exist")
	}
	endpoint.Addresses = addrs
	endpoint.Hostnames = hosts
	writer, err := c.ConnectionWriter()
	if err != nil {
		return errors.Annotate(err, "failed to get connection writer")
	}
	writer.SetAPIEndpoint(endpoint)
	err = writer.Write()
	if err != nil {
		return errors.Annotate(err, "failed to write API endpoint to connection info")
	}
	return nil
}
