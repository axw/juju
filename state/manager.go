// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"sync"
	"time"

	"github.com/juju/errors"
	"github.com/juju/juju/worker/catacomb"
	"github.com/juju/utils/clock"
	names "gopkg.in/juju/names.v2"
	mgo "gopkg.in/mgo.v2"
)

type ManagerConfig struct {
	Clock        clock.Clock
	MongoSession *mgo.Session

	ControllerTag          names.ControllerTag // remove this
	ControllerModelTag     names.ModelTag      // remove this
	NewPolicy              NewPolicyFunc
	RunTransactionObserver RunTransactionObserverFunc
	InitDatabaseFunc       InitDatabaseFunc
}

// Validate validates the Manager configuration.
func (config ManagerConfig) Validate() error {
	// TODO(axw)
	return nil
}

// Manager manages the workers required for accessing and watching changes to
// state. All State objects are obtained through a Manager, and are bound to
// its lifetime.
type Manager struct {
	catacomb catacomb.Catacomb
	config   ManagerConfig

	mu     sync.Mutex
	states map[string]*stateWorker
}

// NewManager returns a new Manager with the given configuration.
func NewManager(config ManagerConfig) (*Manager, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}
	m := &Manager{
		config: config,
		states: make(map[string]*State),
	}
	if err := catacomb.Invoke(catacomb.Plan{
		Site: &m.catacomb,
		Work: m.loop,
	}); err != nil {
		return nil, errors.Trace(err)
	}
	return m, nil
}

// Kill is part of the worker.Worker interface.
func (m *Manager) Kill() {
	m.catacomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (m *Manager) Wait() error {
	return m.catacomb.Wait()
}

// State returns a State object for the model with the specified UUID.
//
// If no model with that UUID is found, then an error satisfying
// errors.IsNotFound will be returned.
func (m *Manager) State(modelUUID string) (*State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	sw, ok := m.states[modelUUID]
	if ok {
		return sw.st, nil
	}

	// TODO(axw) create new state object.

	sw, err := newStateWorker(st, m.config.Clock)
	if err != nil {
		return nil, errors.Trace(err)
	}
	m.states[modelUUID] = sw
	return sw.st, nil
}

func (m *Manager) removeState(modelUUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
}

func (m *Manager) loop() error {
	// TODO(axw) run a model watcher on the "system state",
	// kill state workers and remove from the map when they
	// are removed.
	for {
		select {
		case <-w.catacomb.Dying():
			return w.catacomb.ErrDying()
		}
	}
	return nil
}

// stateWorker is a worker that encapsulates a State object, and pings
// its Mongo connection periodically.
type stateWorker struct {
	catacomb catacomb.Catacomb
	st       *State
	clock    clock.Clock
}

func newStateWorker(st *State, clock clock.Clock) (*stateWorker, error) {
	sw := &stateWorker{
		st:    st,
		clock: clock,
	}
	if err := catacomb.Invoke(catacomb.Plan{
		Site: &m.catacomb,
		Work: m.loop,
	}); err != nil {
		return nil, errors.Trace(err)
	}
	return sw, nil
}

// Kill is part of the worker.Worker interface.
func (w *stateWorker) Kill() {
	w.catacomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (w *stateWorker) Wait() error {
	return w.catacomb.Wait()
}

func (w *stateWorker) loop() error {
	const pingInterval = 30 * time.Second // TODO(axw) config
	pingTimer := w.clock.NewTimer(pingInterval)
	defer pingTimer.Stop()
	for {
		select {
		case <-w.catacomb.Dying():
			return w.catacomb.ErrDying()
		case <-pingTimer.Chan():
			// TODO(axw) unexpose State.Ping, nothing
			// should be using except for this worker.
			if err := w.st.Ping(); err != nil {
				return errors.Annotate(err, "pinging state")
			}
			pingTimer.Reset(pingInterval)
		}
	}
}
