// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskmanager

import (
	"github.com/juju/errors"
	"github.com/juju/names"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/storage"
)

const diskManagerFacade = "DiskManager"

// State provides access to a diskmanager worker's view of the state.
type State struct {
	facade base.FacadeCaller
}

// NewState creates a new client-side DiskManager facade.
func NewState(caller base.APICaller) *State {
	return &State{base.NewFacadeCaller(caller, diskManagerFacade)}
}

// BlockDevices returns the block devices attached to the specified machine.
func (st *State) BlockDevices(tag names.MachineTag) ([]storage.BlockDevice, error) {
	args := params.Entities{
		Entities: []params.Entity{{Tag: tag.String()}},
	}
	var results params.BlockDevicesResults
	err := st.facade.FacadeCall("BlockDevices", args, &results)
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
	return results.Results[0].BlockDevices, nil
}

// WatchBlockDevices returns a NotifyWatcher that notifies of changes to block
// devices on the machine.
func (st *State) WatchBlockDevices(tag names.MachineTag) (watcher.NotifyWatcher, error) {
	args := params.Entities{
		Entities: []params.Entity{{Tag: tag.String()}},
	}
	var results params.NotifyWatchResults
	err := st.facade.FacadeCall("WatchBlockDevices", args, &results)
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
