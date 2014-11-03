// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskformatter

import (
	"github.com/juju/errors"
	"github.com/juju/names"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/storage"
)

const diskFormatterFacade = "DiskFormatter"

// State provides access to a diskformatter worker's view of the state.
type State struct {
	facade base.FacadeCaller
}

// NewState creates a new client-side DiskFormatter facade.
func NewState(caller base.APICaller) *State {
	return &State{base.NewFacadeCaller(caller, diskFormatterFacade)}
}

// Storage returns the storage associated with the specified unit.
func (st *State) Storage(tag names.UnitTag) ([]storage.Storage, error) {
	args := params.Entities{
		Entities: []params.Entity{{Tag: tag.String()}},
	}
	var results params.StorageResults
	err := st.facade.FacadeCall("Storage", args, &results)
	if err != nil {
		// TODO: Not directly tested
		return nil, err
	}
	if len(results.Results) != 1 {
		// TODO: Not directly tested
		err = errors.Errorf("expected one result, got %d", len(results.Results))
		return nil, err
	}
	result := results.Results[0]
	if result.Error != nil {
		return nil, result.Error
	}
	return results.Results[0].Storage, nil
}

// WatchStorage returns a NotifyWatcher that notifies of changes to storage
// instances on the unit.
func (st *State) WatchStorage(tag names.UnitTag) (watcher.NotifyWatcher, error) {
	args := params.Entities{
		Entities: []params.Entity{{Tag: tag.String()}},
	}
	var results params.NotifyWatchResults
	err := st.facade.FacadeCall("WatchStorage", args, &results)
	if err != nil {
		// TODO: Not directly tested
		return nil, err
	}
	if len(results.Results) != 1 {
		// TODO: Not directly tested
		err = errors.Errorf("expected one result, got %d", len(results.Results))
		return nil, err
	}
	result := results.Results[0]
	if result.Error != nil {
		return nil, result.Error
	}
	w := watcher.NewNotifyWatcher(st.facade.RawAPICaller(), result)
	return w, nil
}
