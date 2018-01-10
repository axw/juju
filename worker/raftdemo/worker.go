// Copyright 2018 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package raftdemo

import (
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/raft"
	"github.com/juju/errors"
	"github.com/juju/loggo"
	worker "gopkg.in/juju/worker.v1"

	"github.com/juju/juju/worker/catacomb"
)

var (
	logger = loggo.GetLogger("juju.worker.raftdemo")
)

type Config struct {
	Raft *raft.Raft
}

func (config Config) Validate() error {
	if config.Raft == nil {
		return errors.NotValidf("nil Raft")
	}
	return nil
}

func NewWorker(config Config) (worker.Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}
	w := &Worker{
		config: config,
	}
	if err := catacomb.Invoke(catacomb.Plan{
		Site: &w.catacomb,
		Work: w.loop,
	}); err != nil {
		return nil, errors.Trace(err)
	}
	return w, nil
}

type Worker struct {
	catacomb catacomb.Catacomb
	config   Config
}

// Kill is part of the worker.Worker interface.
func (w *Worker) Kill() {
	w.catacomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (w *Worker) Wait() error {
	return w.catacomb.Wait()
}

func (w *Worker) loop() error {
	hostname, _ := os.Hostname()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.catacomb.Dying():
			return w.catacomb.ErrDying()
		case <-ticker.C:
			f := w.config.Raft.Apply([]byte(fmt.Sprintf(
				"hello from %s at %s",
				hostname, time.Now(),
			)), 0)
			if err := f.Error(); err != nil {
				return err
			}
		}
	}
}
