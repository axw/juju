package apihttphandler

import (
	"context"
	"net/http"
	"sync/atomic"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/utils"
	"github.com/juju/utils/clock"
	"golang.org/x/net/websocket"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/apiserver/authentication"
	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/observer"
	"github.com/juju/juju/rpc"
	"github.com/juju/juju/rpc/jsoncodec"
	"github.com/juju/juju/state"
)

// Config holds the configuration for New.
type Config struct {
	Logger           loggo.Logger
	StatePool        *state.StatePool
	ObserverFactory  observer.ObserverFactory
	PingClock        clock.Clock
	AllowModelAccess bool
	LoginValidator   LoginValidator
	Limiter          utils.Limiter
	NewAuthenticator func(*http.Request) authentication.EntityAuthenticator
	AgentTag         names.Tag
	AgentDataDir     string
	AgentLogDir      string
	FacadeRegistry   FacadeRegistry
}

func (config Config) Validate() error {
	// TODO(axw)
	return nil
}

// Newreturns an http.Handler that serves Juju API requests.
func New(config Config) (http.Handler, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Annotate(err, "validating config")
	}
	return &apiHTTPHandler{config: config}, nil
}

type apiHTTPHandler struct {
	lastConnectionID uint64
	config           Config
}

var adminAPIFactories = map[int]adminAPIFactory{
	3: newAdminAPIV3,
}

func (a *apiHTTPHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Get :modeluuid, if provided. If it is not
	// provided, the controller model is assumed.
	modelUUID := req.URL.Query().Get(":modeluuid")

	connectionID := atomic.AddUint64(&a.lastConnectionID, 1)
	apiObserver := a.config.ObserverFactory()
	apiObserver.Join(req, connectionID)
	defer apiObserver.Leave()

	authenticator := a.config.NewAuthenticator(req)
	wsServer := websocket.Server{
		Handler: func(conn *websocket.Conn) {
			a.config.Logger.Tracef("got a request for model %q", modelUUID)
			if err := a.serveConn(
				req.Context(),
				conn,
				modelUUID,
				apiObserver,
				authenticator,
			); err != nil {
				a.config.Logger.Errorf("error serving RPCs: %v", err)
			}
		},
	}
	wsServer.ServeHTTP(w, req)
}

func (a *apiHTTPHandler) serveConn(
	ctx context.Context,
	wsConn *websocket.Conn,
	modelUUID string,
	apiObserver observer.Observer,
	authenticator authentication.EntityAuthenticator,
) error {
	rpcConn := rpc.NewConn(
		jsoncodec.NewWebsocket(wsConn),
		apiObserver,
	)

	var h *apiHandler
	st, releaser, err := a.getState(modelUUID)
	if err == nil {
		defer releaser()
		h, err = newAPIHandler(
			st, rpcConn, modelUUID,
			a.config.AgentTag,
			a.config.AgentDataDir,
			a.config.AgentLogDir,
			a.config.FacadeRegistry,
		)
	}

	if err != nil {
		rpcConn.ServeRoot(&errRoot{errors.Trace(err)}, serverError)
	} else {
		adminAPIs := make(map[int]interface{})
		for apiVersion, factory := range adminAPIFactories {
			adminAPIs[apiVersion] = factory(adminAPIParams{
				state:            st,
				statePool:        a.config.StatePool,
				root:             h,
				apiObserver:      apiObserver,
				pingClock:        a.config.PingClock,
				allowModelAccess: a.config.AllowModelAccess,
				validator:        a.config.LoginValidator,
				limiter:          a.config.Limiter,
				authenticator:    authenticator,
				logger:           a.config.Logger,
				facades:          a.config.FacadeRegistry,
			})
		}
		rpcConn.ServeRoot(newAnonRoot(h, adminAPIs), serverError)
	}

	rpcConn.Start()
	select {
	case <-rpcConn.Dead():
	case <-ctx.Done():
	}
	return rpcConn.Close()
}

func (a *apiHTTPHandler) getState(modelUUID string) (*state.State, func(), error) {
	if modelUUID == "" {
		modelUUID = a.config.StatePool.SystemState().ModelUUID()
	}
	st, release, err := a.config.StatePool.Get(modelUUID)
	if err != nil {
		// TODO(axw) we should probably have StatePool.Get return
		// NotFound errors, and only return UnknownModelError when
		// that is returned?
		return nil, nil, errors.Wrap(err, common.UnknownModelError(modelUUID))
	}
	return st, release, nil
}
