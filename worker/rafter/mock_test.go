// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package rafter_test

import (
	"io"

	"github.com/hashicorp/raft"
	"github.com/juju/testing"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/agent"
)

type mockAgent struct {
	config mockAgentConfig
}

func (a *mockAgent) CurrentConfig() agent.Config {
	return &a.config
}

func (a *mockAgent) ChangeConfig(agent.ConfigMutator) error {
	return nil
}

type mockAgentConfig struct {
	agent.Config
	tag names.Tag
}

func (cfg *mockAgentConfig) Tag() names.Tag {
	return cfg.tag
}

type mockFSM struct {
	testing.Stub
	applyResult    interface{}
	snapshotResult raft.FSMSnapshot
}

func (fsm *mockFSM) Apply(log *raft.Log) interface{} {
	fsm.MethodCall(fsm, "Apply", log)
	return fsm.applyResult
}

func (fsm *mockFSM) Snapshot() (raft.FSMSnapshot, error) {
	fsm.MethodCall(fsm, "Snapshot")
	if err := fsm.NextErr(); err != nil {
		return nil, err
	}
	return &mockFSMSnapshot{&fsm.Stub}, nil
}

func (fsm *mockFSM) Restore(rc io.ReadCloser) error {
	fsm.MethodCall(fsm, "Restore", rc)
	return fsm.NextErr()
}

type mockFSMSnapshot struct {
	*testing.Stub
}

func (snap *mockFSMSnapshot) Persist(sink raft.SnapshotSink) error {
	snap.MethodCall(snap, "Persist", sink)
	return snap.NextErr()
}

func (snap *mockFSMSnapshot) Release() {
	snap.MethodCall(snap, "Release")
	snap.PopNoErr()
}
