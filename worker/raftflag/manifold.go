// Copyright 2015-2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package raftflag

import (
	"github.com/hashicorp/raft"
	"github.com/juju/errors"
	"github.com/juju/juju/cmd/jujud/agent/engine"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/dependency"
)

// ManifoldConfig holds the information necessary to run a FlagWorker in
// a dependency.Engine.
type ManifoldConfig struct {
	RaftName  string
	NewWorker func(FlagConfig) (worker.Worker, error)
}

// start is a method on ManifoldConfig because it's more readable than a closure.
func (config ManifoldConfig) start(context dependency.Context) (worker.Worker, error) {
	var raft *raft.Raft
	if err := context.Get(config.RaftName, &raft); err != nil {
		return nil, errors.Trace(err)
	}

	flag, err := config.NewWorker(FlagConfig{
		Raft: raft,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return flag, nil
}

// Manifold returns a dependency.Manifold that will run a FlagWorker and
// expose it to clients as a engine.Flag resource.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{config.RaftName},
		Start:  config.start,
		Output: engine.FlagOutput,
	}
}
