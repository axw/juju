// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package httpserver

import (
	"io"
	"net"
	"net/http"
	"sync"

	tomb "gopkg.in/tomb.v1"

	"github.com/bmizerany/pat"
	"github.com/juju/errors"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/dependency"
	"github.com/juju/juju/worker/state"
)

// ManifoldConfig defines the static configuration, and names
// of the manifolds on which the httpserver Manifold will depend.
type ManifoldConfig struct {
	AgentName       string
	StateName       string
	CertChangedName string
}

// Manifold returns a dependency manifold that runs an httpserver worker, using
// the resource names defined in the supplied config.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{
			config.AgentName,
			config.StateName,
			config.CertChangedName,
		},
		Start: func(context dependency.Context) (worker.Worker, error) {
			var a agent.Agent
			if err := context.Get(config.AgentName, &a); err != nil {
				return nil, errors.Trace(err)
			}
			agentConfig := a.CurrentConfig()
			servingInfo, ok := agentConfig.StateServingInfo()
			if !ok {
				logger.Debugf("no state serving info set")
				return nil, dependency.ErrUninstall
			}
			// TODO(axw) ensure a cert/key pair is available

			var stateTracker state.StateTracker
			if err := context.Get(config.StateName, &stateTracker); err != nil {
				return nil, errors.Trace(err)
			}
			st, err := stateTracker.Use()
			if err != nil {
				return nil, errors.Trace(err)
			}
			controllerConfig, err := st.ControllerConfig()
			if err != nil {
				return nil, errors.Trace(err)
			}

			var certChanged <-chan params.StateServingInfo
			if err := context.Get(config.CertChangedName, &certChanged); err != nil {
				return nil, errors.Trace(err)
			}

			// XXX
			_ = servingInfo
			addr := ":18080"
			//addr := net.JoinHostPort("", strconv.Itoa(servingInfo.APIPort))
			listener, err := net.Listen("tcp", addr)
			if err != nil {
				return nil, errors.Annotate(err, "creating HTTP server listener")
			}

			// The HTTP server needs the state object to cache
			// autocert certificates.
			stateTrackerCloser := worker.NewSimpleWorker(func(abort <-chan struct{}) error {
				<-abort
				if err := stateTracker.Done(); err != nil {
					return errors.Trace(err)
				}
				return tomb.ErrDying
			})

			var closers closers
			certManager := newCertManager(certChanged)
			tlsConfig := certManager.newTLSConfig(
				controllerConfig.AutocertDNSName(),
				controllerConfig.AutocertURL(),
				st.AutocertCache(),
			)
			mux := pat.New()
			w, err := NewHTTPServer(Config{
				Listener:  listener,
				TLSConfig: tlsConfig,
				Handler:   mux,
				Closer:    &closers,
			}, certManager, stateTrackerCloser)
			if err != nil {
				return nil, errors.Trace(err)
			}
			return &httpserverManifoldWorker{w, mux, &closers}, nil
		},
		Output: func(in worker.Worker, out interface{}) error {
			w, ok := in.(*httpserverManifoldWorker)
			if !ok {
				return errors.Errorf("in should be a %T; got %T", w, in)
			}
			fptr, ok := out.(*RegisterHandlerFunc)
			if ok {
				*fptr = w.registerHandler
				return nil
			}
			return errors.Errorf("out should be %T; got %T", fptr, out)
		},
	}
}

type httpserverManifoldWorker struct {
	worker.Worker

	mux     *pat.PatternServeMux
	closers *closers
}

func (w *httpserverManifoldWorker) registerHandler(method, pattern string, hc HandleCloser) func() {
	w.mux.Add(method, pattern, hc)
	w.closers.add(hc)
	return func() {
		// TODO(axw) deregister the handler
		w.closers.remove(hc)
	}
}

type closers struct {
	mu      sync.Mutex
	closers []io.Closer
}

func (cs *closers) add(c io.Closer) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.closers = append(cs.closers, c)
}

func (cs *closers) remove(c io.Closer) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for i := range cs.closers {
		if c != cs.closers[i] {
			continue
		}
		head := cs.closers[:i]
		tail := cs.closers[i+1:]
		cs.closers = append(head, tail...)
		break
	}
}

// Close is part of the io.Closer interface.
func (cs *closers) Close() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	for _, c := range cs.closers {
		if err := c.Close(); err != nil {
			return errors.Trace(err)
		}
	}
	return nil
}

type RegisterHandlerFunc func(string, string, HandleCloser) func()

type HandleCloser interface {
	http.Handler
	io.Closer
}

func NopCloser(h http.Handler) HandleCloser {
	return nopCloser{h}
}

type nopCloser struct {
	http.Handler
}

func (nopCloser) Close() error {
	return nil
}
