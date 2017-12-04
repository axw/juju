// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package caasunitprovisioner

import (
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/state"
)

// CAASUnitProvisionerState provides the subset of global state
// required by the CAAS operator facade.
type CAASUnitProvisionerState interface {
	Application(string) (Application, error)
	Model() (Model, error)
	Unit(string) (Unit, error)
	WatchApplications() state.StringsWatcher
}

// Model provides the subset of CAAS model state required
// by the CAAS operator facade.
type Model interface {
	ContainerSpec(names.Tag) (string, error)
	WatchContainerSpec(names.Tag) (state.NotifyWatcher, error)
}

// Application provides the subset of application state
// required by the CAAS operator facade.
type Application interface {
	Life() state.Life
	WatchUnits() state.StringsWatcher
}

// Unit provides the subset of unit state required by the
// CAAS operator facade.
type Unit interface {
	Life() state.Life
}

type stateShim struct {
	*state.State
}

func (s stateShim) Application(id string) (Application, error) {
	app, err := s.State.Application(id)
	if err != nil {
		return nil, err
	}
	return app, nil
}

func (s stateShim) Unit(id string) (Unit, error) {
	unit, err := s.State.Unit(id)
	if err != nil {
		return nil, err
	}
	return unit, nil
}

func (s stateShim) Model() (Model, error) {
	model, err := s.State.Model()
	if err != nil {
		return nil, err
	}
	return model.CAASModel()
}
