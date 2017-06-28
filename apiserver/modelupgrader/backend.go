// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelupgrader

import (
	"github.com/juju/errors"
	"github.com/juju/juju/state"
	"github.com/juju/version"
	"gopkg.in/juju/names.v2"
)

type Backend interface {
	GetModel(names.ModelTag) (Model, error)
}

type Model interface {
	EnvironVersion() version.Number
	SetEnvironVersion(version.Number) error
}

func NewStateBackend(st *state.State) Backend {
	return stateBackend{st}
}

type stateBackend struct {
	st *state.State
}

func (s stateBackend) GetModel(tag names.ModelTag) (Model, error) {
	m, err := s.st.GetModel(tag)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return stateModel{m}, nil
}

type stateModel struct {
	*state.Model
}
