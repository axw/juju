package raftdemo

import (
	"encoding/gob"
	"io"
	"sync"

	"github.com/hashicorp/raft"
)

type FSM struct {
	mu   sync.Mutex
	logs [][]byte
}

func (fsm *FSM) Apply(log *raft.Log) interface{} {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()
	logger.Debugf("applying log: %q", log.Data)
	fsm.logs = append(fsm.logs, log.Data)
	return len(fsm.logs)
}

func (fsm *FSM) Snapshot() (raft.FSMSnapshot, error) {
	fsm.mu.Lock()
	defer fsm.mu.Unlock()
	return &Snapshot{fsm.logs, len(fsm.logs)}, nil
}

func (fsm *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()
	var logs [][]byte
	if err := gob.NewDecoder(rc).Decode(&logs); err != nil {
		return err
	}
	fsm.mu.Lock()
	fsm.logs = logs
	fsm.mu.Unlock()
	return nil
}

type Snapshot struct {
	logs [][]byte
	n    int
}

func (snap *Snapshot) Persist(sink raft.SnapshotSink) error {
	if err := gob.NewEncoder(sink).Encode(snap.logs[:snap.n]); err != nil {
		sink.Cancel()
		return err
	}
	sink.Close()
	return nil
}

func (*Snapshot) Release() {}
