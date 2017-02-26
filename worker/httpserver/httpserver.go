// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package httpserver

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"

	"github.com/juju/errors"
	"github.com/juju/loggo"

	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/catacomb"
)

var logger = loggo.GetLogger("juju.worker.httpserver")

type Config struct {
	Listener  net.Listener
	Handler   http.Handler
	TLSConfig *tls.Config
	Closer    io.Closer
}

func (config Config) Validate() error {
	if config.Listener == nil {
		return errors.NotValidf("nil Listener")
	}
	if config.Handler == nil {
		return errors.NotValidf("nil Handler")
	}
	return nil
}

// NewHTTPServer returns a worker.Worker that serves HTTP
// requests for the Juju controller. This function takes
// ownership of any workers provided to it, whether or not
// an error is returned.
func NewHTTPServer(config Config, subs ...worker.Worker) (worker.Worker, error) {
	w := &httpServerWorker{
		config: config,
	}
	if err := catacomb.Invoke(catacomb.Plan{
		Site: &w.catacomb,
		Work: w.loop,
		Init: subs,
	}); err != nil {
		return nil, errors.Trace(err)
	}
	return w, nil
}

// httpServerWorker is responsible for running the Juju controller
// HTTP server. This includes the apiserver, introspection endpoints,
// etc.
type httpServerWorker struct {
	catacomb catacomb.Catacomb
	config   Config
}

// Kill is part of the worker.Worker interface.
func (w *httpServerWorker) Kill() {
	w.catacomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (w *httpServerWorker) Wait() error {
	return w.catacomb.Wait()
}

func (w *httpServerWorker) loop() error {
	tlsConfig := w.config.TLSConfig
	listener := w.config.Listener
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}

	s := &http.Server{
		Handler:   w.config.Handler,
		TLSConfig: tlsConfig,
		ErrorLog: log.New(
			&loggoWrapper{
				level:  loggo.WARNING,
				logger: logger,
			},
			// no prefix and no flags so log.Logger doesn't add extra prefixes
			"", 0,
		),
	}
	logger.Debugf("Listening for HTTP requests on %q", listener.Addr())

	done := make(chan error, 1)
	go func() {
		done <- s.Serve(listener)
	}()

	select {
	case <-w.catacomb.Dying():
		// TODO(axw) graceful shutdown: requires Go 1.8+.
		listener.Close()
		if w.config.Closer != nil {
			if err := w.config.Closer.Close(); err != nil {
				return errors.Trace(err)
			}
		}
		return w.catacomb.ErrDying()
	case err := <-done:
		return err
	}
}
