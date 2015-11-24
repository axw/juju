// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package remoterelationsworker provides a worker that manages the exchange
// of relation settings between environments.
package remoterelationsworker

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"launchpad.net/tomb"

	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
	statewatcher "github.com/juju/juju/state/watcher"
	"github.com/juju/juju/worker"
)

var logger = loggo.GetLogger("juju.worker.remoterelationsworker")

// Config encapsulates the configuration for the worker.
type Config struct {
	RemoteServicesAccessor RemoteServicesAccessor
}

func (cfg Config) Validate() error {
	if cfg.RemoteServicesAccessor == nil {
		return errors.NotValidf("nil RemoteServicesAccessor")
	}
	return nil
}

// RemoteServicesAccessor is an interface that provides a means of watching
// the lifecycle states of remote services known to the local environment.
type RemoteServicesAccessor interface {
	// ConsumeRemoteServiceChange consumes remote changes to a service
	// into the local environment.
	ConsumeRemoteServiceChange(params.ServiceChange) error

	// PublishLocalRelationsChange publishes local relations changes
	// to the remote side offering those relations.
	PublishLocalRelationsChange(params.ServiceRelationsChange) error

	// WatchRemoteServices watches for addition, removal and lifecycle
	// changes to remote services known to the local environment.
	WatchRemoteServices() (watcher.StringsWatcher, error)

	// WatchRemoteService watches for remote changes to the service
	// with the given name.
	WatchRemoteService(name string) (watcher.ServiceWatcher, error)

	// WatchServiceRelations watches for local changes to the relations
	// involving the service with the given name.
	WatchServiceRelations(service string) (watcher.ServiceRelationsWatcher, error)
}

type consumeFunc func(params.ServiceChange) error
type publishFunc func(params.ServiceRelationsChange) error

func NewWorker(config Config) (worker.Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Annotate(err, "validating config")
	}
	worker := worker.NewStringsWorker(&remoteServicesHandler{
		config:  config,
		workers: make(map[string]worker.Worker),
	})
	return worker, nil
}

type remoteServicesHandler struct {
	config  Config
	workers map[string]worker.Worker
}

func (h *remoteServicesHandler) SetUp() (watcher.StringsWatcher, error) {
	return h.config.RemoteServicesAccessor.WatchRemoteServices()
}

func (h *remoteServicesHandler) TearDown() error {
	for _, w := range h.workers {
		w.Kill()
	}
	var firstErr error
	var firstErrServiceId string
	var errCount int
	for serviceId, w := range h.workers {
		err := w.Wait()
		if err != nil {
			if firstErr == nil {
				firstErr = err
				firstErrServiceId = serviceId
			}
			errCount++
		}
	}
	if firstErr != nil {
		return errors.Annotatef(
			firstErr, "stopping relations watcher for remote service %q (%d more error(s))",
			firstErrServiceId, errCount-1,
		)
	}
	return nil
}

func (h *remoteServicesHandler) Handle(serviceIds []string) error {
	startWatchers := func(serviceId string) (watcher.ServiceRelationsWatcher, watcher.ServiceWatcher, error) {
		localWatcher, err := h.config.RemoteServicesAccessor.WatchServiceRelations(serviceId)
		if err != nil {
			return nil, nil, errors.Trace(err)
		}
		remoteWatcher, err := h.config.RemoteServicesAccessor.WatchRemoteService(serviceId)
		if err != nil {
			if err := localWatcher.Stop(); err != nil {
				logger.Errorf("stopping local watcher while starting remote watcher: %v", err)
			}
			return nil, nil, errors.Trace(err)
		}
		return localWatcher, remoteWatcher, nil
	}

	// TODO(axw) bulk methods for starting watchers?
	for _, id := range serviceIds {
		localWatcher, remoteWatcher, err := startWatchers(id)
		if errors.IsNotFound(err) {
			w, ok := h.workers[id]
			if ok {
				w.Kill()
				if err := w.Wait(); err != nil {
					return errors.Annotate(err, "stopping worker")
				}
			}
			continue
		} else if err != nil {
			return errors.Annotatef(err, "watching relations for remote service %q", id)
		}
		h.workers[id] = newRemoteServiceWorker(
			localWatcher, remoteWatcher,
			h.config.RemoteServicesAccessor.ConsumeRemoteServiceChange,
			h.config.RemoteServicesAccessor.PublishLocalRelationsChange,
		)
	}
	return nil
}

type remoteServiceWorker struct {
	tomb          tomb.Tomb
	localWatcher  watcher.ServiceRelationsWatcher
	remoteWatcher watcher.ServiceWatcher
	consume       consumeFunc
	publish       publishFunc
}

func newRemoteServiceWorker(
	localWatcher watcher.ServiceRelationsWatcher,
	remoteWatcher watcher.ServiceWatcher,
	consume consumeFunc,
	publish publishFunc,
) worker.Worker {
	worker := &remoteServiceWorker{
		localWatcher:  localWatcher,
		remoteWatcher: remoteWatcher,
		consume:       consume,
		publish:       publish,
	}
	go func() {
		defer worker.tomb.Done()
		defer statewatcher.Stop(worker.localWatcher, &worker.tomb)
		worker.tomb.Kill(worker.loop())
	}()
	return worker
}

func (w *remoteServiceWorker) Kill() {
	w.tomb.Kill(nil)
}

func (w *remoteServiceWorker) Wait() error {
	return w.tomb.Wait()
}

func (w *remoteServiceWorker) loop() error {
	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying
		case change, ok := <-w.localWatcher.Changes():
			if !ok {
				return statewatcher.EnsureErr(w.localWatcher)
			}
			if err := w.publish(change); err != nil {
				return errors.Annotate(err, "publishing change to offering environment")
			}
		case change, ok := <-w.remoteWatcher.Changes():
			if !ok {
				return statewatcher.EnsureErr(w.remoteWatcher)
			}
			if err := w.consume(change); err != nil {
				return errors.Annotate(err, "consuming change into local environment")
			}
		}
	}
}
