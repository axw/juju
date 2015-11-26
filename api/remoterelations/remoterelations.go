// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package remoterelations

import (
	"github.com/juju/errors"
	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/names"
)

const remoteRelationsFacade = "RemoteRelations"

// State provides access to a remoterelations's view of the state.
type State struct {
	facade base.FacadeCaller
}

// NewState creates a new client-side RemoteRelations facade.
func NewState(caller base.APICaller) *State {
	facadeCaller := base.NewFacadeCaller(caller, remoteRelationsFacade)
	return &State{facadeCaller}
}

// WatchRemoteServices returns a strings watcher that notifies of the addition,
// removal, and lifecycle changes of remote services in the environment.
func (st *State) WatchRemoteServices() (watcher.StringsWatcher, error) {
	var result params.StringsWatchResult
	err := st.facade.FacadeCall("WatchRemoteServices", nil, &result)
	if err != nil {
		return nil, err
	}
	if result.Error != nil {
		return nil, result.Error
	}
	w := watcher.NewStringsWatcher(st.facade.RawAPICaller(), result)
	return w, nil
}

// WatchRemoteService returns a remote service watcher that delivers
// changes made to the remote service in the offering environment.
// This includes status, lifecycle and relation changes.
func (st *State) WatchRemoteService(service string) (watcher.ServiceWatcher, error) {
	if !names.IsValidService(service) {
		return nil, errors.NotValidf("service name %q", service)
	}
	serviceTag := names.NewServiceTag(service)
	args := params.Entities{
		Entities: []params.Entity{{Tag: serviceTag.String()}},
	}

	var results params.ServiceWatchResults
	err := st.facade.FacadeCall("WatchRemoteService", args, &results)
	if err != nil {
		return nil, err
	}
	if len(results.Results) != 1 {
		return nil, errors.Errorf("expected 1 result, got %d", len(results.Results))
	}
	result := results.Results[0]
	if result.Error != nil {
		return nil, result.Error
	}
	w := watcher.NewServiceWatcher(st.facade.RawAPICaller(), result)
	return w, nil
}

// WatchServiceRelations returns a service relations watcher that delivers
// changes made to the relations of the identified services in the local
// environment.
func (st *State) WatchServiceRelations(service string) (watcher.ServiceRelationsWatcher, error) {
	if !names.IsValidService(service) {
		return nil, errors.NotValidf("service name %q", service)
	}
	serviceTag := names.NewServiceTag(service)
	args := params.Entities{
		Entities: []params.Entity{{Tag: serviceTag.String()}},
	}

	var results params.ServiceRelationsWatchResults
	err := st.facade.FacadeCall("WatchServiceRelations", args, &results)
	if err != nil {
		return nil, err
	}
	if len(results.Results) != 1 {
		return nil, errors.Errorf("expected 1 result, got %d", len(results.Results))
	}
	result := results.Results[0]
	if result.Error != nil {
		return nil, result.Error
	}
	w := watcher.NewServiceRelationsWatcher(st.facade.RawAPICaller(), result)
	return w, nil
}

func (st *State) ConsumeRemoteServiceChange(change params.ServiceChange) error {
	args := params.ServiceChanges{
		Changes: []params.ServiceChange{change},
	}
	var results params.ErrorResults
	err := st.facade.FacadeCall("ConsumeRemoteServiceChange", args, &results)
	if err != nil {
		return err
	}
	if len(results.Results) != 1 {
		return errors.Errorf("expected 1 result, got %d", len(results.Results))
	}
	result := results.Results[0]
	if result.Error != nil {
		return result.Error
	}
	return nil
}

func (st *State) PublishLocalRelationsChange(change params.ServiceRelationsChange) error {
	args := params.ServiceRelationsChanges{
		Changes: []params.ServiceRelationsChange{change},
	}
	var results params.ErrorResults
	err := st.facade.FacadeCall("PublishLocalRelationsChange", args, &results)
	if err != nil {
		return err
	}
	if len(results.Results) != 1 {
		return errors.Errorf("expected 1 result, got %d", len(results.Results))
	}
	result := results.Results[0]
	if result.Error != nil {
		return result.Error
	}
	return nil
}
