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

func (st *State) ExportEntities(tags []names.Tag) ([]params.RemoteEntityIdResult, error) {
	args := params.Entities{Entities: make([]params.Entity, len(tags))}
	for i, tag := range tags {
		args.Entities[i].Tag = tag.String()
	}
	var results params.RemoteEntityIdResults
	err := st.facade.FacadeCall("ExportEntities", args, &results)
	if err != nil {
		return nil, err
	}
	if len(results.Results) != len(tags) {
		return nil, errors.Errorf("expected %d result(s), got %d", len(tags), len(results.Results))
	}
	return results.Results, nil
}

func (st *State) PublishLocalRelationChange(change params.RemoteRelationChange) error {
	args := params.RemoteRelationChanges{
		Changes: []params.RemoteRelationChange{change},
	}
	var results params.ErrorResults
	err := st.facade.FacadeCall("PublishLocalRelationChange", args, &results)
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

func (st *State) RelationUnitSettings(relationUnits []params.RelationUnit) ([]params.SettingsResult, error) {
	args := params.RelationUnits{relationUnits}
	var results params.SettingsResults
	err := st.facade.FacadeCall("RelationUnitSettings", args, &results)
	if err != nil {
		return nil, err
	}
	if len(results.Results) != len(relationUnits) {
		return nil, errors.Errorf("expected %d result(s), got %d", len(relationUnits), len(results.Results))
	}
	return results.Results, nil
}

func (st *State) RemoteRelations(keys []string) ([]params.RemoteRelationResult, error) {
	args := params.Entities{Entities: make([]params.Entity, len(keys))}
	for i, key := range keys {
		args.Entities[i].Tag = names.NewRelationTag(key).String()
	}
	var results params.RemoteRelationResults
	err := st.facade.FacadeCall("RemoteRelations", args, &results)
	if err != nil {
		return nil, err
	}
	if len(results.Results) != len(keys) {
		return nil, errors.Errorf("expected %d result(s), got %d", len(keys), len(results.Results))
	}
	return results.Results, nil
}

func (st *State) RemoteServices(services []string) ([]params.RemoteServiceResult, error) {
	args := params.Entities{Entities: make([]params.Entity, len(services))}
	for i, service := range services {
		args.Entities[i].Tag = names.NewServiceTag(service).String()
	}
	var results params.RemoteServiceResults
	err := st.facade.FacadeCall("RemoteServices", args, &results)
	if err != nil {
		return nil, err
	}
	if len(results.Results) != len(services) {
		return nil, errors.Errorf("expected %d result(s), got %d", len(services), len(results.Results))
	}
	return results.Results, nil
}

func (st *State) WatchLocalRelationUnits(relationKey string) (watcher.RelationUnitsWatcher, error) {
	if !names.IsValidRelation(relationKey) {
		return nil, errors.NotValidf("relation key %q", relationKey)
	}
	relationTag := names.NewRelationTag(relationKey)
	args := params.Entities{
		Entities: []params.Entity{{Tag: relationTag.String()}},
	}
	var results params.RelationUnitsWatchResults
	err := st.facade.FacadeCall("WatchLocalRelationUnits", args, &results)
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
	w := watcher.NewRelationUnitsWatcher(st.facade.RawAPICaller(), result)
	return w, nil
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

func (st *State) WatchServiceRelations(service string) (watcher.StringsWatcher, error) {
	if !names.IsValidService(service) {
		return nil, errors.NotValidf("service name %q", service)
	}
	serviceTag := names.NewServiceTag(service)
	args := params.Entities{
		Entities: []params.Entity{{Tag: serviceTag.String()}},
	}

	var results params.StringsWatchResults
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
	w := watcher.NewStringsWatcher(st.facade.RawAPICaller(), result)
	return w, nil
}
