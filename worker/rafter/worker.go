// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package rafter

import (
	"github.com/hashicorp/raft"
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/utils/voyeur"
	tomb "gopkg.in/tomb.v1"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/catacomb"
)

// NewWorker returns a new worker.Worker that runs a Raft, and keeps its
// peer list up-to-date. Neither the logic (FSM) nor various stores are
// the responsibility the worker; they must be supplied and available
// for the lifetime of this worker.
func NewWorker(config Config) (worker.Worker, *raft.Raft, error) {
	if err := config.Validate(); err != nil {
		return nil, nil, errors.Annotate(err, "validating config")
	}

	// Set a default logger if none has been configured.
	if config.RaftConfig.Logger == nil && config.RaftConfig.LogOutput == nil {
		raftConfig := *config.RaftConfig
		raftConfig.LogOutput = loggoWriter{loggo.GetLogger("raft")}
		config.RaftConfig = &raftConfig
	}

	rw, err := newRaftWorker(config)
	if err != nil {
		return nil, nil, errors.Annotate(err, "creating raft worker")
	}

	u := &raftPeerUpdater{raft: rw.raft}
	go func() {
		defer u.tomb.Done()
		u.tomb.Kill(u.loop(config.Agent, config.AgentConfigChanged))
	}()

	if err := catacomb.Invoke(catacomb.Plan{
		Site: &rw.catacomb,
		Init: []worker.Worker{u},
		Work: rw.loop,
	}); err != nil {
		return nil, nil, errors.Trace(err)
	}
	return rw, rw.raft, nil
}

type raftWorker struct {
	catacomb         catacomb.Catacomb
	raft             *raft.Raft
	shutdownCh       chan raft.Observation
	shutdownObserver *raft.Observer
}

func newRaftWorker(config Config) (*raftWorker, error) {
	var err error
	rw := &raftWorker{}
	rw.raft, err = raft.NewRaft(
		config.RaftConfig,
		config.RaftFSM,
		config.RaftLogStore,
		config.RaftStableStore,
		config.RaftSnapshotStore,
		config.RaftPeerStore,
		config.RaftTransport,
	)
	if err != nil {
		return nil, errors.Annotate(err, "creating raft")
	}

	// Register an observer that waits for raft to transition to Shutdown.
	rw.shutdownCh = make(chan raft.Observation, 1)
	rw.shutdownObserver = raft.NewObserver(rw.shutdownCh, false, func(o *raft.Observation) bool {
		return o.Data == raft.Shutdown
	})
	rw.raft.RegisterObserver(rw.shutdownObserver)
	return rw, nil
}

// Kill is part of the worker.Worker interface.
func (w *raftWorker) Kill() {
	w.catacomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (w *raftWorker) Wait() error {
	return w.catacomb.Wait()
}

func (w *raftWorker) loop() error {
	defer close(w.shutdownCh)
	defer w.raft.DeregisterObserver(w.shutdownObserver)

	select {
	case <-w.catacomb.Dying():
		if err := w.raft.Shutdown().Error(); err != nil {
			return err
		}
		return w.catacomb.ErrDying()

	case <-w.shutdownCh:
		// Raft has transitioned to the Shutdown state.
		//
		// BUG(axw) Raft.Shutdown itself does not wait
		// for the Raft goroutines to exit; you must wait
		// on the Future returned. Unfortunately the
		// Raft.Shutdown method returns a no-op Future
		// if Raft.Shutdown has already been called,
		// regardless of whether its Future is waited on.
		//
		// Even so, we call the Shutdown method here and
		// wait for its Future in the hope that that is
		// fixed upstream.
		return w.raft.Shutdown().Error()
	}
}

type raftPeerUpdater struct {
	tomb tomb.Tomb
	raft *raft.Raft
}

func (u *raftPeerUpdater) Kill() {
	u.tomb.Kill(nil)
}

func (u *raftPeerUpdater) Wait() error {
	return u.tomb.Wait()
}

func (u *raftPeerUpdater) loop(a agent.Agent, agentConfigChanged *voyeur.Value) error {
	watcher := agentConfigChanged.Watch()
	defer watcher.Close()

	watchCh := make(chan struct{})
	go func() {
		defer close(watchCh)
		for watcher.Next() {
			watchCh <- struct{}{}
		}
	}()

	for {
		select {
		case <-u.tomb.Dying():
			return tomb.ErrDying
		case <-watchCh:
			// TODO(axw) update u.raft's peers.
		}
	}
}
