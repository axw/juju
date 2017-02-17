// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package rafter

import (
	"github.com/hashicorp/raft"
	"github.com/juju/errors"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/dependency"
)

// Manifold returns a dependency manifold that runs a rafter worker, using
// the resource names defined in the supplied config.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{
			config.AgentName,
		},
		Start: func(context dependency.Context) (worker.Worker, error) {
			var agent agent.Agent
			if err := context.Get(config.AgentName, &agent); err != nil {
				return nil, err
			}
			w, _, err := NewWorker(Config{
				Agent:              agent,
				AgentConfigChanged: config.AgentConfigChanged,
				RaftConfig:         config.RaftConfig,
				RaftFSM:            config.RaftFSM,
				RaftLogStore:       config.RaftLogStore,
				RaftTransport:      config.RaftTransport,
				RaftPeerStore:      config.RaftPeerStore,
				RaftStableStore:    config.RaftStableStore,
				RaftSnapshotStore:  config.RaftSnapshotStore,
			})
			return w, err
		},
		Output: func(in worker.Worker, out interface{}) error {
			w, ok := in.(*raftWorker)
			if !ok {
				return errors.Errorf("in should be a %T; got %T", w, in)
			}
			if raftp, ok := out.(**raft.Raft); ok {
				*raftp = w.raft
				return nil
			}
			return errors.Errorf("out should be **raft.Raft; got %T", out)
		},
	}
}
