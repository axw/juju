// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package caasunitprovisioner_test

import (
	"github.com/juju/testing"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/apiserver/facades/controller/caasunitprovisioner"
	"github.com/juju/juju/state"
	statetesting "github.com/juju/juju/state/testing"
)

type mockState struct {
	testing.Stub
	applicationsWatcher *statetesting.MockStringsWatcher
	model               mockModel
}

func (st *mockState) WatchApplications() state.StringsWatcher {
	st.MethodCall(st, "WatchApplications")
	return st.applicationsWatcher
}

func (st *mockState) Application(name string) (caasunitprovisioner.Application, error) {
	panic("TODO")
}

func (st *mockState) Model() (caasunitprovisioner.Model, error) {
	st.MethodCall(st, "Model")
	if err := st.NextErr(); err != nil {
		return nil, err
	}
	return &st.model, nil
}

func (st *mockState) Unit(name string) (caasunitprovisioner.Unit, error) {
	panic("TODO")
}

type mockModel struct {
	testing.Stub
	containerSpec        string
	containerSpecWatcher *statetesting.MockNotifyWatcher
}

func (m *mockModel) ContainerSpec(tag names.Tag) (string, error) {
	m.MethodCall(m, "ContainerSpec", tag)
	if err := m.NextErr(); err != nil {
		return "", err
	}
	return m.containerSpec, nil
}

func (m *mockModel) WatchContainerSpec(tag names.Tag) (state.NotifyWatcher, error) {
	m.MethodCall(m, "WatchContainerSpec", tag)
	if err := m.NextErr(); err != nil {
		return nil, err
	}
	return m.containerSpecWatcher, nil
}

/*
type mockApplication struct {
	state.Authenticator
	tag      names.Tag
	password string
}

func (m *mockApplication) Tag() names.Tag {
	return m.tag
}

func (m *mockApplication) SetPassword(password string) error {
	m.password = password
	return nil
}
*/
