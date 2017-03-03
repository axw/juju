// Copyright 2013, 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apihttphandler

import (
	"sync"
	"time"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/utils"
	"github.com/juju/utils/clock"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/apiserver/authentication"
	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/facade"
	"github.com/juju/juju/apiserver/observer"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/apiserver/presence"
	"github.com/juju/juju/permission"
	"github.com/juju/juju/rpc"
	"github.com/juju/juju/rpc/rpcreflect"
	"github.com/juju/juju/state"
	statepresence "github.com/juju/juju/state/presence"
	jujuversion "github.com/juju/juju/version"
)

const (
	// maxClientPingInterval defines the timeframe until the ping timeout
	// closes the monitored connection. TODO(mue): Idea by Roger:
	// Move to API (e.g. params) so that the pinging there may
	// depend on the interval.
	maxClientPingInterval = 3 * time.Minute
)

type adminAPIParams struct {
	state            *state.State
	statePool        *state.StatePool
	root             *apiHandler
	apiObserver      observer.Observer
	pingClock        clock.Clock
	allowModelAccess bool
	validator        LoginValidator
	limiter          utils.Limiter
	authenticator    authentication.EntityAuthenticator
	logger           loggo.Logger
	facades          FacadeRegistry
}

type adminAPIFactory func(adminAPIParams) interface{}

// admin is the only object that unlogged-in clients can access. It holds any
// methods that are needed to log in.
type admin struct {
	state            *state.State
	statePool        *state.StatePool
	root             *apiHandler
	apiObserver      observer.Observer
	pingClock        clock.Clock
	allowModelAccess bool
	validator        LoginValidator
	limiter          utils.Limiter
	authenticator    authentication.EntityAuthenticator
	logger           loggo.Logger
	facades          FacadeRegistry

	mu       sync.Mutex
	loggedIn bool
}

var AboutToRestoreError = errors.New("restore preparation in progress")
var RestoreInProgressError = errors.New("restore in progress")
var MaintenanceNoLoginError = errors.New("login failed - maintenance in progress")
var errAlreadyLoggedIn = errors.New("already logged in")

// login is the internal version of the Login API call.
func (a *admin) login(req params.LoginRequest, loginVersion int) (params.LoginResult, error) {
	var fail params.LoginResult

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loggedIn {
		// This can only happen if Login is called concurrently.
		return fail, errAlreadyLoggedIn
	}

	// apiRoot is the API root exposed to the client after authentication.
	var apiRoot rpc.Root = newAPIRoot(
		a.root.state,
		a.statePool,
		a.root.resources,
		facade.Authorizer(a.root),
		a.facades,
	)

	// Use the login validation function, if one was specified.
	if a.validator != nil {
		err := a.validator(req)
		switch err {
		case params.UpgradeInProgressError:
			apiRoot = restrictRoot(apiRoot, upgradeMethodsOnly)
		case AboutToRestoreError:
			apiRoot = restrictRoot(apiRoot, aboutToRestoreMethodsOnly)
		case RestoreInProgressError:
			apiRoot = restrictAll(apiRoot, restoreInProgressError)
		case nil:
			// in this case no need to wrap authed api so we do nothing
		default:
			return fail, errors.Trace(err)
		}
	}

	isUser := true
	kind := names.UserTagKind
	if req.AuthTag != "" {
		var err error
		kind, err = names.TagKind(req.AuthTag)
		if err != nil || kind != names.UserTagKind {
			isUser = false
			// Users are not rate limited, all other entities are.
			if !a.limiter.Acquire() {
				a.logger.Debugf("rate limiting for agent %s", req.AuthTag)
				return fail, common.ErrTryAgain
			}
			defer a.limiter.Release()
		}
	}

	controllerOnlyLogin := a.root.modelUUID == ""
	controllerMachineLogin := false

	entity, lastConnection, err := a.checkCreds(req, isUser)
	if err != nil {
		if err, ok := errors.Cause(err).(*common.DischargeRequiredError); ok {
			loginResult := params.LoginResult{
				DischargeRequired:       err.Macaroon,
				DischargeRequiredReason: err.Error(),
			}
			a.logger.Infof("login failed with discharge-required error: %v", err)
			return loginResult, nil
		}
		if a.maintenanceInProgress() {
			// An upgrade, restore or similar operation is in
			// progress. It is possible for logins to fail until this
			// is complete due to incomplete or updating data. Mask
			// transitory and potentially confusing errors from failed
			// logins with a more helpful one.
			return fail, MaintenanceNoLoginError
		}
		// Here we have a special case.  The machine agents that manage
		// models in the controller model need to be able to
		// open API connections to other models.  In those cases, we
		// need to look in the controller database to check the creds
		// against the machine if and only if the entity tag is a machine tag,
		// and the machine exists in the controller model, and the
		// machine has the manage state job.  If all those parts are valid, we
		// can then check the credentials against the controller model
		// machine.
		if kind != names.MachineTagKind {
			return fail, errors.Trace(err)
		}
		if errors.Cause(err) != common.ErrBadCreds {
			return fail, err
		}
		entity, err = a.checkControllerMachineCreds(req)
		if err != nil {
			return fail, errors.Trace(err)
		}
		// If we are here, then the entity will refer to a controller
		// machine in the controller model, and we don't need a pinger
		// for it as we already have one running in the machine agent api
		// worker for the controller model.
		controllerMachineLogin = true
	}
	a.root.entity = entity
	a.apiObserver.Login(entity.Tag(), a.root.state.ModelTag(), controllerMachineLogin, req.UserData)

	// We have authenticated the user; enable the appropriate API
	// to serve to them.
	a.loggedIn = true

	if !controllerMachineLogin {
		if err := startPingerIfAgent(
			a.pingClock,
			a.root,
			entity,
			a.logger,
		); err != nil {
			return fail, errors.Trace(err)
		}
	}

	var maybeUserInfo *params.AuthUserInfo
	// Send back user info if user
	if isUser {
		userTag := entity.Tag().(names.UserTag)
		maybeUserInfo, err = a.checkUserPermissions(userTag, controllerOnlyLogin)
		if err != nil {
			return fail, errors.Trace(err)
		}
		maybeUserInfo.LastConnection = lastConnection
	} else {
		if controllerOnlyLogin {
			a.logger.Debugf("controller login: %s", entity.Tag())
		} else {
			a.logger.Debugf("model login: %s for %s", entity.Tag(), a.root.state.ModelTag().Id())
		}
	}

	// Fetch the API server addresses from state.
	hostPorts, err := a.root.state.APIHostPorts()
	if err != nil {
		return fail, errors.Trace(err)
	}

	model, err := a.root.state.Model()
	if err != nil {
		return fail, errors.Trace(err)
	}

	if isUser {
		switch model.MigrationMode() {
		case state.MigrationModeImporting:
			// The user is not able to access a model that is currently being
			// imported until the model has been activated.
			apiRoot = restrictAll(apiRoot, errors.New("migration in progress, model is importing"))
		case state.MigrationModeExporting:
			// The user is not allowed to change anything in a model that is
			// currently being moved to another controller.
			apiRoot = restrictRoot(apiRoot, migrationClientMethodsOnly)
		}
	}

	loginResult := params.LoginResult{
		Servers:       params.FromNetworkHostsPorts(hostPorts),
		ControllerTag: model.ControllerTag().String(),
		UserInfo:      maybeUserInfo,
		ServerVersion: jujuversion.Current.String(),
	}

	if controllerOnlyLogin {
		loginResult.Facades = filterFacades(isControllerFacade)
		apiRoot = restrictRoot(apiRoot, controllerFacadesOnly)
	} else {
		loginResult.ModelTag = model.Tag().String()
		loginResult.Facades = filterFacades(isModelFacade)
		apiRoot = restrictRoot(apiRoot, modelFacadesOnly)
	}

	a.root.rpcConn.ServeRoot(apiRoot, serverError)

	return loginResult, nil
}

func (a *admin) checkUserPermissions(userTag names.UserTag, controllerOnlyLogin bool) (*params.AuthUserInfo, error) {

	modelAccess := permission.NoAccess

	// TODO(perrito666) remove the following section about everyone group
	// when groups are implemented, this accounts only for the lack of a local
	// ControllerUser when logging in from an external user that has not been granted
	// permissions on the controller but there are permissions for the special
	// everyone group.
	everyoneGroupAccess := permission.NoAccess
	if !userTag.IsLocal() {
		everyoneTag := names.NewUserTag(common.EveryoneTagName)
		everyoneGroupUser, err := state.ControllerAccess(a.root.state, everyoneTag)
		if err != nil && !errors.IsNotFound(err) {
			return nil, errors.Annotatef(err, "obtaining ControllerUser for everyone group")
		}
		everyoneGroupAccess = everyoneGroupUser.Access
	}

	controllerAccess := permission.NoAccess
	if controllerUser, err := state.ControllerAccess(a.root.state, userTag); err == nil {
		controllerAccess = controllerUser.Access
	} else if errors.IsNotFound(err) {
		controllerAccess = everyoneGroupAccess
	} else {
		return nil, errors.Annotatef(err, "obtaining ControllerUser for logged in user %s", userTag.Id())
	}
	if !controllerOnlyLogin {
		// Only grab modelUser permissions if this is not a controller only
		// login. In all situations, if the model user is not found, they have
		// no authorisation to access this model, unless the user is controller
		// admin.

		modelUser, err := a.root.state.UserAccess(userTag, a.root.state.ModelTag())
		if err != nil && controllerAccess != permission.SuperuserAccess {
			return nil, errors.Wrap(err, common.ErrPerm)
		}
		if err != nil && controllerAccess == permission.SuperuserAccess {
			modelAccess = permission.AdminAccess
		} else {
			modelAccess = modelUser.Access
		}
	}

	// It is possible that the everyoneGroup permissions are more capable than an
	// individuals. If they are, use them.
	if everyoneGroupAccess.GreaterControllerAccessThan(controllerAccess) {
		controllerAccess = everyoneGroupAccess
	}
	if controllerOnlyLogin || !a.allowModelAccess {
		// We're either explicitly logging into the controller or
		// we must check that the user has access to the controller
		// even though they're logging into a model.
		if controllerAccess == permission.NoAccess {
			return nil, errors.Trace(common.ErrPerm)
		}
	}
	if controllerOnlyLogin {
		a.logger.Debugf("controller login: user %s has %q access", userTag.Id(), controllerAccess)
	} else {
		a.logger.Debugf("model login: user %s has %q for controller; %q for model %s",
			userTag.Id(), controllerAccess, modelAccess, a.root.state.ModelTag().Id())
	}
	return &params.AuthUserInfo{
		Identity:         userTag.String(),
		ControllerAccess: string(controllerAccess),
		ModelAccess:      string(modelAccess),
	}, nil
}

func filterFacades(allowFacade func(name string) bool) []params.FacadeVersions {
	allFacades := DescribeFacades()
	out := make([]params.FacadeVersions, 0, len(allFacades))
	for _, facade := range allFacades {
		if allowFacade(facade.Name) {
			out = append(out, facade)
		}
	}
	return out
}

func (a *admin) checkCreds(req params.LoginRequest, lookForModelUser bool) (state.Entity, *time.Time, error) {
	return authentication.Login(a.root.state, req, lookForModelUser, a.authenticator)
}

func (a *admin) checkControllerMachineCreds(req params.LoginRequest) (state.Entity, error) {
	return authentication.LoginControllerMachine(a.state, req, a.authenticator)
}

func (a *admin) maintenanceInProgress() bool {
	if a.validator == nil {
		return false
	}
	// jujud's login validator will return an error for any user tag
	// if jujud is upgrading or restoring. The tag of the entity
	// trying to log in can't be used because jujud's login validator
	// will always return nil for the local machine agent and here we
	// need to know if maintenance is in progress irrespective of the
	// the authenticating entity.
	//
	// TODO(mjs): 2014-09-29 bug 1375110
	// This needs improving but I don't have the cycles right now.
	req := params.LoginRequest{
		AuthTag: names.NewUserTag("arbitrary").String(),
	}
	return a.validator(req) != nil
}

// presenceShim exists to represent a statepresence.Agent in a form
// convenient to the apiserver/presence package, which exists to work
// around the common.Resources infrastructure's lack of handling for
// failed resources.
type presenceShim struct {
	agent statepresence.Agent
}

// Start starts and returns a running presence.Pinger. The caller is
// responsible for stopping it when no longer required, and for handling
// any errors returned from Wait.
func (shim presenceShim) Start() (presence.Pinger, error) {
	pinger, err := shim.agent.SetAgentPresence()
	if err != nil {
		return nil, errors.Trace(err)
	}
	return pinger, nil
}

func startPingerIfAgent(
	clock clock.Clock,
	root *apiHandler,
	entity state.Entity,
	logger loggo.Logger,
) error {
	// worker runs presence.Pingers -- absence of which will cause
	// embarrassing "agent is lost" messages to show up in status --
	// until it's stopped. It's stored in resources purely for the
	// side effects: we don't record its id, and nobody else
	// retrieves it -- we just expect it to be stopped when the
	// connection is shut down.
	agent, ok := entity.(statepresence.Agent)
	if !ok {
		return nil
	}
	worker, err := presence.New(presence.Config{
		Identity:   entity.Tag(),
		Start:      presenceShim{agent}.Start,
		Clock:      clock,
		RetryDelay: 3 * time.Second,
	})
	if err != nil {
		return err
	}
	root.getResources().Register(worker)

	// pingTimeout, by contrast, *is* used by the Pinger facade to
	// stave off the call to action() that will shut down the agent
	// connection if it gets lackadaisical about sending keepalive
	// Pings.
	//
	// Do not confuse those (apiserver) Pings with those made by
	// presence.Pinger (which *do* happen as a result of the former,
	// but only as a relatively distant consequence).
	//
	// We should have picked better names...
	action := func() {
		logger.Debugf("closing connection due to ping timout")
		if err := root.getRpcConn().Close(); err != nil {
			logger.Errorf("error closing the RPC connection: %v", err)
		}
	}
	pingTimeout := newPingTimeout(action, clock, maxClientPingInterval)
	return root.getResources().RegisterNamed("pingTimeout", pingTimeout)
}

// errRoot implements the API that a client first sees
// when connecting to the API. It exposes the same API as initialRoot, except
// it returns the requested error when the client makes any request.
type errRoot struct {
	err error
}

// FindMethod conforms to the same API as initialRoot, but we'll always return (nil, err)
func (r *errRoot) FindMethod(rootName string, version int, methodName string) (rpcreflect.MethodCaller, error) {
	return nil, r.err
}

func (r *errRoot) Kill() {
}

func serverError(err error) error {
	if err := common.ServerError(err); err != nil {
		return err
	}
	return nil
}
