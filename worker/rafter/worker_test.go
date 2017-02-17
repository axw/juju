// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package rafter_test

import (
	"time"

	"github.com/hashicorp/raft"
	"github.com/juju/names"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils/voyeur"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/rafter"
	"github.com/juju/juju/worker/workertest"
)

type RafterSuite struct {
	testing.IsolationSuite

	agent     *mockAgent
	addr      string
	transport *raft.InmemTransport
	peers     *raft.StaticPeers
	store     *raft.InmemStore
	fsm       *mockFSM
}

var _ = gc.Suite(&RafterSuite{})

func (s *RafterSuite) SetUpTest(c *gc.C) {
	s.IsolationSuite.SetUpTest(c)
	s.agent = &mockAgent{
		config: mockAgentConfig{
			tag: names.NewMachineTag("0"),
		},
	}
	s.addr, s.transport = raft.NewInmemTransport("")
	s.AddCleanup(func(c *gc.C) {
		c.Assert(s.transport.Close(), jc.ErrorIsNil)
	})
	s.peers = &raft.StaticPeers{}
	s.store = raft.NewInmemStore()
	s.fsm = &mockFSM{}
}

func testRaftConfig() *raft.Config {
	cfg := raft.DefaultConfig()
	cfg.EnableSingleNode = true
	cfg.StartAsLeader = true
	cfg.ElectionTimeout = time.Millisecond * 100
	cfg.HeartbeatTimeout = cfg.ElectionTimeout
	cfg.LeaderLeaseTimeout = cfg.HeartbeatTimeout / 2
	return cfg
}

func (s *RafterSuite) newRafter(c *gc.C) (worker.Worker, *raft.Raft) {
	w, raft, err := rafter.NewWorker(rafter.Config{
		Agent:              s.agent,
		AgentConfigChanged: voyeur.NewValue(nil),
		RaftConfig:         testRaftConfig(),
		RaftFSM:            s.fsm,
		RaftLogStore:       s.store,
		RaftTransport:      s.transport,
		RaftPeerStore:      s.peers,
		RaftStableStore:    s.store,
		RaftSnapshotStore:  &raft.DiscardSnapshotStore{},
	})
	c.Assert(err, jc.ErrorIsNil)
	return w, raft
}

func (s *RafterSuite) TestRafterCleanKill(c *gc.C) {
	w, r := s.newRafter(c)
	workertest.CleanKill(c, w)
	c.Assert(r.State(), gc.Equals, raft.Shutdown)
}

func (s *RafterSuite) TestRafterShutdownKillsWorker(c *gc.C) {
	w, r := s.newRafter(c)
	c.Assert(r.Shutdown().Error(), jc.ErrorIsNil)
	workertest.CheckKilled(c, w)
}

func (s *RafterSuite) TestRafterRaftApply(c *gc.C) {
	w, r := s.newRafter(c)
	defer workertest.CleanKill(c, w)

	s.fsm.applyResult = "test.output"
	future := r.Apply([]byte("test.input"), time.Minute)
	c.Assert(future.Error(), jc.ErrorIsNil)
	c.Assert(future.Response(), gc.Equals, "test.output")

	s.fsm.CheckCallNames(c, "Apply")
	log := s.fsm.Calls()[0].Args[0].(*raft.Log)
	c.Assert(log.Type, gc.Equals, raft.LogCommand)
	c.Assert(string(log.Data), gc.Equals, "test.input")
}
