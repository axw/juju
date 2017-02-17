// Copyright 2015-2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package raftflag

import (
	"github.com/hashicorp/raft"
	"github.com/juju/errors"
	"github.com/juju/juju/worker/catacomb"
	"github.com/juju/juju/worker/dependency"
	"github.com/juju/loggo"
)

var logger = loggo.GetLogger("juju.worker.singular")

// FlagConfig holds a FlagWorker's dependencies and resources.
type FlagConfig struct {
	Raft *raft.Raft
}

// Validate returns an error if the config cannot be expected to run a
// FlagWorker.
func (config FlagConfig) Validate() error {
	if config.Raft == nil {
		return errors.NotValidf("nil Raft")
	}
	return nil
}

// ErrRefresh indicates that the flag's Check result is no longer valid,
// and a new FlagWorker must be started to get a valid result.
var ErrRefresh = errors.New("model responsibility unclear, please retry")

// FlagWorker implements worker.Worker and util.Flag, representing
// controller ownership of a model, such that the Flag's validity is tied
// to the Worker's lifetime.
type FlagWorker struct {
	catacomb       catacomb.Catacomb
	config         FlagConfig
	leaderObserver *raft.Observer
	leaderCh       chan raft.Observation
	valid          bool
}

func NewFlagWorker(config FlagConfig) (*FlagWorker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}

	// We cannot rely on config.Raft.LeaderCh(), as that channel
	// is unbuffered and the sender does not block.
	leaderCh := make(chan raft.Observation, 1)
	o := raft.NewObserver(leaderCh, false, func(o *raft.Observation) bool {
		_, ok := o.Data.(raft.LeaderObservation)
		return ok
	})
	config.Raft.RegisterObserver(o)

	// Check the initial state *after* registering the observer,
	// to ensure we don't miss any changes.
	isLeader := config.Raft.State() == raft.Leader

	flag := &FlagWorker{
		config:         config,
		leaderObserver: o,
		leaderCh:       leaderCh,
		valid:          isLeader,
	}
	err := catacomb.Invoke(catacomb.Plan{
		Site: &flag.catacomb,
		Work: flag.run,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return flag, nil
}

// Kill is part of the worker.Worker interface.
func (flag *FlagWorker) Kill() {
	flag.catacomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (flag *FlagWorker) Wait() error {
	return flag.catacomb.Wait()
}

// Check is part of the util.Flag interface.
//
// Check returns true if the flag indicates that the configured Identity
// (i.e. this controller) has taken control of the configured Scope (i.e.
// the model we want to manage exclusively).
//
// The validity of this result is tied to the lifetime of the FlagWorker;
// once the worker has stopped, no inferences may be drawn from any Check
// result.
func (flag *FlagWorker) Check() bool {
	return flag.valid
}

// run invokes a suitable runFunc, depending on the value of .valid.
func (flag *FlagWorker) run() error {
	defer close(flag.leaderCh)
	defer flag.config.Raft.DeregisterObserver(flag.leaderObserver)
	logger.Debugf("waiting for change in flag (currently: %v)", flag.valid)
	for {
		select {
		case <-flag.catacomb.Dying():
			return flag.catacomb.ErrDying()
		case <-flag.leaderCh:
			// It is possible to see an event for the initial
			// state if the leadership changes between registering
			// the observer and obtaining the initial state.
			if flag.valid != (flag.config.Raft.State() == raft.Leader) {
				return dependency.ErrBounce
			}
		}
	}
}
