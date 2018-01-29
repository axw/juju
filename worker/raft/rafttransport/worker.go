// Copyright 2018 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package rafttransport

import (
	"context"
	"crypto/tls"
	"log"
	"net"
	"strconv"
	"time"

	"github.com/hashicorp/raft"
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/api"
	"github.com/juju/juju/apiserver/apiserverhttp"
	"github.com/juju/juju/worker/catacomb"
	"github.com/juju/juju/worker/raft/raftutil"
)

var (
	logger = loggo.GetLogger("juju.worker.raft.rafttransport")
)

// Config is the configuration required for running an apiserver-based
// raft transport worker.
type Config struct {
	// APIInfo contains the information, excluding addresses,
	// required to connect to an API server.
	APIInfo *api.Info

	// Mux is the API server HTTP mux into which the handler will
	// be installed.
	Mux *apiserverhttp.Mux

	// Path is the path of the raft HTTP endpoint.
	Path string

	// Tag is the tag of the agent running this worker.
	Tag names.Tag

	// Timeout, if non-zero, is the timeout to apply to transport
	// operations. See raft.NetworkTransportConfig.Timeout for more
	// details.
	Timeout time.Duration
}

// Validate validates the raft worker configuration.
func (config Config) Validate() error {
	if config.APIInfo == nil {
		return errors.NotValidf("nil APIInfo")
	}
	if config.Mux == nil {
		return errors.NotValidf("nil Mux")
	}
	if config.Path == "" {
		return errors.NotValidf("empty Path")
	}
	if config.Tag == nil {
		return errors.NotValidf("nil Tag")
	}
	return nil
}

// NewWorker returns a new apiserver-based raft transport worker,
// with the given configuration. The worker itself implements
// raft.Transport.
func NewWorker(config Config) (*Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}

	certPool, err := api.CreateCertPool(config.APIInfo.CACert)
	if err != nil {
		return nil, errors.Trace(err)
	}
	apiPorts := config.APIInfo.Ports()
	if n := len(apiPorts); n != 1 {
		return nil, errors.Errorf("api.Info has %d unique ports, expected 1", n)
	}

	w := &Worker{
		config:       config,
		connections:  make(chan net.Conn),
		dialRequests: make(chan dialRequest),
		tlsConfig:    api.NewTLSConfig(certPool),
		apiPort:      apiPorts[0],
	}

	const logPrefix = "[transport] "
	logWriter := &raftutil.LoggoWriter{logger, loggo.DEBUG}
	logLogger := log.New(logWriter, logPrefix, 0)
	transport := raft.NewNetworkTransportWithConfig(&raft.NetworkTransportConfig{
		Logger:  logLogger,
		MaxPool: 7, // 7 is the maximum juju cluster size
		Stream: newStreamLayer(config.Tag, w.connections, &Dialer{
			APIInfo: config.APIInfo,
			DialRaw: w.dialRaw,
			Path:    config.Path,
		}),
		Timeout: config.Timeout,
	})
	w.Transport = transport

	if err := catacomb.Invoke(catacomb.Plan{
		Site: &w.catacomb,
		Work: func() error {
			defer transport.Close()
			return w.loop()
		},
	}); err != nil {
		transport.Close()
		return nil, errors.Trace(err)
	}
	return w, nil
}

// Worker is a worker that manages a raft.Transport.
type Worker struct {
	raft.Transport

	catacomb     catacomb.Catacomb
	config       Config
	connections  chan net.Conn
	dialRequests chan dialRequest
	tlsConfig    *tls.Config
	apiPort      int
}

type dialRequest struct {
	ctx     context.Context
	address string
	result  chan<- dialResult
}

type dialResult struct {
	conn net.Conn
	err  error
}

// Kill is part of the worker.Worker interface.
func (w *Worker) Kill() {
	w.catacomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (w *Worker) Wait() error {
	return w.catacomb.Wait()
}

// dialRaw dials a new TLS connection to the controller identified
// by the given address. The address is expected to be the stringified
// tag of a controller machine agent. The resulting connection is
// appropriate for use as Dialer.DialRaw.
func (w *Worker) dialRaw(address raft.ServerAddress, timeout time.Duration) (net.Conn, error) {
	// Give precedence to the worker dying.
	select {
	case <-w.catacomb.Dying():
		return nil, w.errDialWorkerStopped()
	default:
	}

	ctx := context.Background()
	if timeout != 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	resultCh := make(chan dialResult)
	req := dialRequest{
		ctx:     ctx,
		address: string(address),
		result:  resultCh,
	}
	select {
	case w.dialRequests <- req:
	case <-ctx.Done():
		return nil, dialRequestTimeoutError{}
	case <-w.catacomb.Dying():
		return nil, w.errDialWorkerStopped()
	}

	select {
	case res := <-resultCh:
		return res.conn, res.err
	case <-ctx.Done():
		return nil, dialRequestTimeoutError{}
	case <-w.catacomb.Dying():
		return nil, w.errDialWorkerStopped()
	}
}

func (w *Worker) errDialWorkerStopped() error {
	err := w.catacomb.Err()
	if err != nil && err != w.catacomb.ErrDying() {
		return dialWorkerStoppedError{err}
	}
	return dialWorkerStoppedError{
		errors.New("worker stopped"),
	}
}

func (w *Worker) loop() error {
	h := NewHandler(w.connections, w.catacomb.Dying())
	w.config.Mux.AddHandler("GET", w.config.Path, &ControllerHandler{
		Mux:     w.config.Mux,
		Handler: h,
	})
	defer w.config.Mux.RemoveHandler("GET", w.config.Path)

	for {
		select {
		case <-w.catacomb.Dying():
			return w.catacomb.ErrDying()
		case req := <-w.dialRequests:
			go w.handleDial(req)
		}
	}
}

func (w *Worker) handleDial(req dialRequest) {
	addr := net.JoinHostPort(
		req.address,
		strconv.Itoa(w.apiPort),
	)
	conn, err := dialConn(req.ctx, addr, w.tlsConfig)
	select {
	case req.result <- dialResult{conn, err}:
		return
	case <-req.ctx.Done():
	case <-w.catacomb.Dying():
	}
	if err == nil {
		// result wasn't delivered, close connection
		conn.Close()
	}
}

// dialConn dials a TLS connection to the API server with the
// given address, using the given TLS configuration. This will
// be used for requesting the raft endpoint, upgrading to a
// raw connection for inter-node raft communications.
//
// TODO: this function needs to be made proxy-aware.
func dialConn(ctx context.Context, addr string, tlsConfig *tls.Config) (net.Conn, error) {
	dialer := &net.Dialer{}
	if deadline, ok := ctx.Deadline(); ok {
		dialer.Deadline = deadline
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	canceled := make(chan struct{})
	go func() {
		<-ctx.Done()
		if ctx.Err() == context.Canceled {
			close(canceled)
		}
	}()
	dialer.Cancel = canceled

	return tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
}
