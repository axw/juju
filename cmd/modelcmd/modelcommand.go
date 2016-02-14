// Copyright 2013-2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelcmd

import (
	"io"
	"os"
	"strings"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/loggo"

	"launchpad.net/gnuflag"

	"github.com/juju/juju/api"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/configstore"
	"github.com/juju/juju/juju/osenv"
	"github.com/juju/juju/jujuclient"
)

var logger = loggo.GetLogger("juju.cmd.envcmd")

// ErrNoModelSpecified is returned by commands that operate on
// an environment if there is no current model, no model
// has been explicitly specified, and there is no default model.
var ErrNoModelSpecified = errors.New("no model specified")

// GetDefaultModel returns the name of the current Juju model.
//
// If $JUJU_MODEL is set, use that. Otherwise, get the current
// controller by reading $XDG_DATA_HOME/juju/current-controller,
// and then identifying the current model for that controller
// in models.yaml. If there is no current controller, or no
// current model for that controller, then an empty string is
// returned. It is not an error to have no default model.
func GetDefaultModel(store jujuclient.ClientStore) (string, error) {
	if model := os.Getenv(osenv.JujuModelEnvKey); model != "" {
		return model, nil
	}
	currentController, err := ReadCurrentController()
	if err != nil {
		return "", errors.Trace(err)
	}
	currentModel, err := store.CurrentModel(currentController)
	if errors.IsNotFound(err) {
		return "", nil
	} else if err != nil {
		return "", errors.Trace(err)
	}
	return currentModel, nil
}

// ModelCommand extends cmd.Command with a SetModelName method.
type ModelCommand interface {
	CommandBase

	// SetClientStore is called prior to the wrapped command's Init method
	// with the default controller store. It may also be called to override the
	// default controller store for testing.
	SetClientStore(jujuclient.ClientStore)

	// ClientStore returns the controller store that the command is
	// associated with.
	ClientStore() jujuclient.ClientStore

	// SetModelName sets the model name for this command. Setting the model
	// name will also set the related controller name.
	//
	// SetModelName is called prior to the wrapped command's Init method
	// with the active model name. The model name is guaranteed
	// to be non-empty at entry of Init.
	SetModelName(modelName string) error

	// ModelName returns the name of the model.
	ModelName() string

	// ControllerName returns the name of the controller that contains
	// the model returned by ModelName().
	ControllerName() string

	// SetAPIOpener allows the replacement of the default API opener,
	// which ends up calling NewAPIRoot
	SetAPIOpener(opener APIOpener)
}

// ModelCommandBase is a convenience type for embedding in commands
// that wish to implement ModelCommand.
type ModelCommandBase struct {
	JujuCommandBase

	// store is the client controller store that contains information
	// about controllers, models, etc.
	store jujuclient.ClientStore

	modelName      string
	controllerName string

	// opener is the strategy used to open the API connection.
	opener APIOpener

	envGetterClient ModelGetter
	envGetterErr    error
}

// SetClientStore implements the ModelCommand interface.
func (c *ModelCommandBase) SetClientStore(store jujuclient.ClientStore) {
	c.store = store
}

// ClientStore implements the ModelCommand interface.
func (c *ModelCommandBase) ClientStore() jujuclient.ClientStore {
	return c.store
}

// SetModelName implements the ModelCommand interface.
func (c *ModelCommandBase) SetModelName(modelName string) error {
	if i := strings.IndexRune(modelName, ':'); i > 0 {
		c.controllerName, c.modelName = modelName[:i], modelName[i+1:]
		return nil
	}
	currentController, err := ReadCurrentController()
	if err != nil {
		return errors.Trace(err)
	}
	c.controllerName, c.modelName = currentController, modelName
	return nil
}

// ModelName implements the ModelCommand interface.
func (c *ModelCommandBase) ModelName() string {
	return c.modelName
}

// ControllerName implements the ModelCommand interface.
func (c *ModelCommandBase) ControllerName() string {
	return c.controllerName
}

// SetAPIOpener specifies the strategy used by the command to open
// the API connection.
func (c *ModelCommandBase) SetAPIOpener(opener APIOpener) {
	c.opener = opener
}

func (c *ModelCommandBase) NewAPIClient() (*api.Client, error) {
	root, err := c.NewAPIRoot()
	if err != nil {
		return nil, errors.Trace(err)
	}
	return root.Client(), nil
}

// NewModelGetter returns a new object which implements the
// ModelGetter interface.
func (c *ModelCommandBase) NewModelGetter() (ModelGetter, error) {
	if c.envGetterErr != nil {
		return nil, c.envGetterErr
	}

	if c.envGetterClient != nil {
		return c.envGetterClient, nil
	}

	return c.NewAPIClient()
}

// NewAPIRoot returns a new connection to the API server for the environment.
func (c *ModelCommandBase) NewAPIRoot() (api.Connection, error) {
	// This is work in progress as we remove the ModelName from downstream code.
	// We want to be able to specify the environment in a number of ways, one of
	// which is the connection name on the client machine.
	if c.modelName == "" {
		return nil, errors.Trace(ErrNoModelSpecified)
	}
	opener := c.opener
	if opener == nil {
		opener = OpenFunc(c.JujuCommandBase.NewAPIRoot)
	}
	if c.modelName != "" {
		_, err := c.store.ModelByName(c.controllerName, c.modelName)
		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, errors.Trace(err)
			}
			// The model isn't known locally, so query the models
			// available in the controller, and cache them locally.
			if err := c.RefreshModels(c.store, c.controllerName); err != nil {
				return nil, errors.Annotate(err, "refreshing models")
			}
		}
	}
	return opener.Open(c.store, c.controllerName, c.modelName)
}

// ConnectionCredentials returns the credentials used to connect to the API for
// the specified environment.
func (c *ModelCommandBase) ConnectionCredentials() (configstore.APICredentials, error) {
	// TODO: the user may soon be specified through the command line
	// or through an environment setting, so return these when they are ready.
	var emptyCreds configstore.APICredentials
	if c.modelName == "" {
		return emptyCreds, errors.Trace(ErrNoModelSpecified)
	}
	info, err := ConnectionInfoForName(c.modelName)
	if err != nil {
		return emptyCreds, errors.Trace(err)
	}
	return info.APICredentials(), nil
}

// ConnectionEndpoint returns the end point information used to
// connect to the API for the specified environment.
func (c *ModelCommandBase) ConnectionEndpoint(refresh bool) (configstore.APIEndpoint, error) {
	// TODO: the endpoint information may soon be specified through the command line
	// or through an environment setting, so return these when they are ready.
	// NOTE: refresh when specified through command line should error.
	var emptyEndpoint configstore.APIEndpoint
	if c.modelName == "" {
		return emptyEndpoint, errors.Trace(ErrNoModelSpecified)
	}
	info, err := ConnectionInfoForName(c.modelName)
	if err != nil {
		return emptyEndpoint, errors.Trace(err)
	}
	endpoint := info.APIEndpoint()
	if !refresh && len(endpoint.Addresses) > 0 {
		logger.Debugf("found cached addresses, not connecting to API server")
		return endpoint, nil
	}

	// We need to connect to refresh our endpoint settings
	// The side effect of connecting is that we update the store with new API information
	refresher, err := endpointRefresher(c)
	if err != nil {
		return emptyEndpoint, err
	}
	refresher.Close()

	info, err = ConnectionInfoForName(c.modelName)
	if err != nil {
		return emptyEndpoint, err
	}
	return info.APIEndpoint(), nil
}

var endpointRefresher = func(c *ModelCommandBase) (io.Closer, error) {
	return c.NewAPIRoot()
}

var getConfigStore = func() (configstore.Storage, error) {
	store, err := configstore.Default()
	if err != nil {
		return nil, errors.Trace(err)
	}
	return store, nil
}

// ConnectionInfoForName reads the environment information for the named
// environment (modelName) and returns it.
func ConnectionInfoForName(modelName string) (configstore.EnvironInfo, error) {
	store, err := getConfigStore()
	if err != nil {
		return nil, errors.Trace(err)
	}
	info, err := store.ReadInfo(modelName)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return info, nil
}

// ConnectionName returns the name of the connection if there is one.
// It is possible that the name of the connection is empty if the
// connection information is supplied through command line arguments
// or environment variables.
func (c *ModelCommandBase) ConnectionName() string {
	return c.modelName
}

// WrapControllerOption sets various parameters of the
// ModelCommand wrapper.
type WrapEnvOption func(*modelCommandWrapper)

// ModelSkipFlags instructs the wrapper to skip --m and
// --model flag definition.
func ModelSkipFlags(w *modelCommandWrapper) {
	w.skipFlags = true
}

// ModelSkipDefault instructs the wrapper not to
// use the default model.
func ModelSkipDefault(w *modelCommandWrapper) {
	w.useDefaultModel = false
}

// EnvAPIOpener instructs the underlying environment command to use a
// different Opener strategy.
func EnvAPIOpener(opener APIOpener) WrapEnvOption {
	return func(w *modelCommandWrapper) {
		w.ModelCommand.SetAPIOpener(opener)
	}
}

// Wrap wraps the specified ModelCommand, returning a Command
// that proxies to each of the ModelCommand methods.
// Any provided options are applied to the wrapped command
// before it is returned.
func Wrap(c ModelCommand, options ...WrapEnvOption) cmd.Command {
	wrapper := &modelCommandWrapper{
		ModelCommand:    c,
		skipFlags:       false,
		useDefaultModel: true,
		allowEmptyEnv:   false,
	}
	for _, option := range options {
		option(wrapper)
	}
	return WrapBase(wrapper)
}

type modelCommandWrapper struct {
	ModelCommand

	skipFlags       bool
	useDefaultModel bool
	allowEmptyEnv   bool
	modelName       string
}

func (w *modelCommandWrapper) SetFlags(f *gnuflag.FlagSet) {
	if !w.skipFlags {
		f.StringVar(&w.modelName, "m", "", "juju model to operate in")
		f.StringVar(&w.modelName, "model", "", "")
	}
	w.ModelCommand.SetFlags(f)
}

func (w *modelCommandWrapper) Init(args []string) error {
	store := w.ClientStore()
	if store == nil {
		store = jujuclient.NewFileClientStore()
		w.SetClientStore(store)
	}
	if !w.skipFlags {
		if w.modelName == "" && w.useDefaultModel {
			// Look for the default.
			defaultModel, err := GetDefaultModel(store)
			if err != nil {
				return err
			}
			w.modelName = defaultModel
		}
		if w.modelName == "" && !w.useDefaultModel {
			if w.allowEmptyEnv {
				return w.ModelCommand.Init(args)
			} else {
				return errors.Trace(ErrNoModelSpecified)
			}
		}
	}
	if err := w.SetModelName(w.modelName); err != nil {
		return errors.Annotate(err, "setting model name")
	}
	return w.ModelCommand.Init(args)
}

type bootstrapContext struct {
	*cmd.Context
	verifyCredentials bool
}

// ShouldVerifyCredentials implements BootstrapContext.ShouldVerifyCredentials
func (ctx *bootstrapContext) ShouldVerifyCredentials() bool {
	return ctx.verifyCredentials
}

// BootstrapContext returns a new BootstrapContext constructed from a command Context.
func BootstrapContext(cmdContext *cmd.Context) environs.BootstrapContext {
	return &bootstrapContext{
		Context:           cmdContext,
		verifyCredentials: true,
	}
}

// BootstrapContextNoVerify returns a new BootstrapContext constructed from a command Context
// where the validation of credentials is false.
func BootstrapContextNoVerify(cmdContext *cmd.Context) environs.BootstrapContext {
	return &bootstrapContext{
		Context:           cmdContext,
		verifyCredentials: false,
	}
}

type ModelGetter interface {
	ModelGet() (map[string]interface{}, error)
	Close() error
}
