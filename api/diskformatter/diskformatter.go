// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskformatter

import (
	"fmt"

	"github.com/juju/errors"
	"github.com/juju/names"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
)

const diskFormatterFacade = "DiskFormatter"

// State provides access to a diskformatter worker's view of the state.
type State struct {
	facade base.FacadeCaller
	tag    names.UnitTag
}

// NewState creates a new client-side DiskFormatter facade.
func NewState(caller base.APICaller, authTag names.UnitTag) *State {
	return &State{
		base.NewFacadeCaller(caller, diskFormatterFacade),
		authTag,
	}
}

// WatchAttachedBlockDevices sets the block devices attached to the machine
// identified by the authenticated machine tag.
func (st *State) WatchAttachedBlockDevices() (watcher.StringsWatcher, error) {
	var results params.StringsWatchResults
	args := params.Entities{
		Entities: []params.Entity{{Tag: st.tag.String()}},
	}
	err := st.facade.FacadeCall("WatchAttachedBlockDevices", args, &results)
	if err != nil {
		return nil, err
	}
	if len(results.Results) != 1 {
		return nil, fmt.Errorf("expected 1 result, got %d", len(results.Results))
	}
	result := results.Results[0]
	if result.Error != nil {
		return nil, result.Error
	}
	w := watcher.NewStringsWatcher(st.facade.RawAPICaller(), result)
	return w, nil
}

func (st *State) BlockDevice(tags []names.DiskTag) (params.BlockDeviceResults, error) {
	var results params.BlockDeviceResults
	args := params.Entities{
		Entities: make([]params.Entity, len(tags)),
	}
	for i, tag := range tags {
		args.Entities[i].Tag = tag.String()
	}
	err := st.facade.FacadeCall("BlockDevice", args, &results)
	if err != nil {
		return params.BlockDeviceResults{}, err
	}
	if len(results.Results) != len(tags) {
		return params.BlockDeviceResults{}, errors.Errorf("expected %d result, got %d", len(tags), len(results.Results))
	}
	return results, nil
}

func (st *State) BlockDeviceDatastore(tags []names.DiskTag) (params.DatastoreResults, error) {
	var results params.DatastoreResults
	args := params.Entities{
		Entities: make([]params.Entity, len(tags)),
	}
	for i, tag := range tags {
		args.Entities[i].Tag = tag.String()
	}
	err := st.facade.FacadeCall("BlockDeviceDatastore", args, &results)
	if err != nil {
		return params.DatastoreResults{}, err
	}
	if len(results.Results) != len(tags) {
		return params.DatastoreResults{}, errors.Errorf("expected %d result, got %d", len(tags), len(results.Results))
	}
	return results, nil
}

func (st *State) SetBlockDeviceFilesystem(filesystems []params.BlockDeviceFilesystem) error {
	var results params.ErrorResults
	args := params.SetBlockDeviceFilesystem{
		BlockDeviceFilesystems: filesystems,
	}
	err := st.facade.FacadeCall("SetBlockDeviceFilesystem", args, &results)
	if err != nil {
		return err
	}
	return results.Combine()
}
