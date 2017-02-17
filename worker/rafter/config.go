// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package rafter

import (
	"github.com/hashicorp/raft"
	"github.com/juju/errors"
	"github.com/juju/utils/voyeur"

	"github.com/juju/juju/agent"
)

// ManifoldConfig defines the static configuration, and names
// of the manifolds on which the rafter Manifold will depend.
type ManifoldConfig struct {
	AgentName          string
	AgentConfigChanged *voyeur.Value

	// RaftConfig is the raft.Config structure that controls
	// raft behaviour.
	RaftConfig *raft.Config

	// RaftFSM is the raft.FSM that defines the core business
	// logic as a Finite State Machine.
	RaftFSM raft.FSM

	// RaftTransport is the raft.Transport that will
	// be used to communicate between raft peers.
	RaftTransport raft.Transport

	// RaftPeerStore is the raft.PeerStore that will
	// be used to store raft's peer set.
	RaftPeerStore raft.PeerStore

	// RaftLogStore is the raft.LogStore that will
	// be used for raft's log storage.
	RaftLogStore raft.LogStore

	// RaftStableStore is the raft.StableStore that will
	// be used for raft's stable state storage.
	RaftStableStore raft.StableStore

	// RaftSnapshotStore is the raft.SnapshotStore that
	// will be used for snapshot storage and retrieval.
	RaftSnapshotStore raft.SnapshotStore
}

// Config is the worker config.
type Config struct {
	Agent              agent.Agent
	AgentConfigChanged *voyeur.Value
	RaftConfig         *raft.Config
	RaftFSM            raft.FSM
	RaftLogStore       raft.LogStore
	RaftTransport      raft.Transport
	RaftPeerStore      raft.PeerStore
	RaftStableStore    raft.StableStore
	RaftSnapshotStore  raft.SnapshotStore
}

// Validate validates the worker config.
func (config Config) Validate() error {
	if config.AgentConfigChanged == nil {
		return errors.NotValidf("nil AgentConfigChanged")
	}
	if config.RaftConfig == nil {
		return errors.NotValidf("nil RaftConfig")
	}
	if config.RaftFSM == nil {
		return errors.NotValidf("nil RaftFSM")
	}
	if config.RaftLogStore == nil {
		return errors.NotValidf("nil RaftLogStore")
	}
	if config.RaftTransport == nil {
		return errors.NotValidf("nil RaftTransport")
	}
	if config.RaftPeerStore == nil {
		return errors.NotValidf("nil RaftPeerStore")
	}
	if config.RaftStableStore == nil {
		return errors.NotValidf("nil RaftStableStore")
	}
	if config.RaftSnapshotStore == nil {
		return errors.NotValidf("nil RaftSnapshotStore")
	}
	if err := raft.ValidateConfig(config.RaftConfig); err != nil {
		return errors.Annotate(err, "validating raft config")
	}
	if config.RaftConfig.EnableSingleNode && !config.RaftConfig.DisableBootstrapAfterElect {
		// We must set EnableSingleNode for a single-node
		// Juju controller, but only for as long as there
		// is a single node. Thus, we require DisableBootstrapAfterElect
		// to be set if EnableSingleNode is set, to prevent
		// the initially lone node from creating a split brain
		// controller in the future.
		return errors.NotValidf("EnableSingleNode && !DisableBootstrapAfterElect")
	}
	return nil
}
