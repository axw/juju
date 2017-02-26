package apiserver

import (
	"net/http"
	"sync/atomic"

	"github.com/juju/errors"
	"github.com/juju/utils"
	"github.com/juju/utils/clock"
	"golang.org/x/net/websocket"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/apiserver/observer"
	"github.com/juju/juju/rpc"
	"github.com/juju/juju/rpc/jsoncodec"
	"github.com/juju/juju/state"
)

type apiHTTPHandler struct {
	lastConnectionID uint64

	statePool        *state.StatePool
	newObserver      observer.ObserverFactory
	pingClock        clock.Clock
	allowModelAccess bool
	validator        LoginValidator
	limiter          utils.Limiter
	authCtxt         *authContext

	agentTag     names.Tag
	agentDataDir string
	agentLogDir  string
}

var adminAPIFactories = map[int]adminAPIFactory{
	3: newAdminAPIV3,
}

func (a *apiHTTPHandler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	connectionID := atomic.AddUint64(&a.lastConnectionID, 1)

	apiObserver := a.newObserver()
	apiObserver.Join(req, connectionID)
	defer apiObserver.Leave()

	wsServer := websocket.Server{
		Handler: func(conn *websocket.Conn) {
			modelUUID := req.URL.Query().Get(":modeluuid")
			logger.Tracef("got a request for model %q", modelUUID)
			if err := a.serveConn(conn, modelUUID, apiObserver, req.Host); err != nil {
				logger.Errorf("error serving RPCs: %v", err)
			}
		},
	}
	wsServer.ServeHTTP(w, req)
}

func (a *apiHTTPHandler) serveConn(
	wsConn *websocket.Conn,
	modelUUID string,
	apiObserver observer.Observer,
	host string,
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
			st, rpcConn, modelUUID, host,
			a.agentTag, a.agentDataDir, a.agentLogDir,
		)
	}

	if err != nil {
		rpcConn.ServeRoot(&errRoot{errors.Trace(err)}, serverError)
	} else {
		adminAPIs := make(map[int]interface{})
		for apiVersion, factory := range adminAPIFactories {
			adminAPIs[apiVersion] = factory(adminAPIParams{
				state:            st,
				statePool:        a.statePool,
				root:             h,
				apiObserver:      apiObserver,
				pingClock:        a.pingClock,
				allowModelAccess: a.allowModelAccess,
				validator:        a.validator,
				limiter:          a.limiter,
				authCtxt:         a.authCtxt,
			})
		}
		rpcConn.ServeRoot(newAnonRoot(h, adminAPIs), serverError)
	}

	rpcConn.Start()
	select {
	case <-rpcConn.Dead():
	case <-a.catacomb.Dying():
	}
	return rpcConn.Close()
}

func (a *apiHTTPHandler) getState(modelUUID string) (*state.State, func(), error) {
	// Note that we don't overwrite modelUUID in the caller because
	// newAPIHandler treats an empty modelUUID as signifying the API
	// version used.
	resolvedModelUUID, err := validateModelUUID(validateArgs{
		statePool: a.statePool,
		modelUUID: modelUUID,
	})
	if err != nil {
		return nil, nil, err
	}
	return a.statePool.Get(resolvedModelUUID)
}
