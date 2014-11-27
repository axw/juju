// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskformatter

import (
	"github.com/juju/loggo"
	"github.com/juju/names"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/watcher"
	"github.com/juju/juju/storage"
)

func init() {
	common.RegisterStandardFacade("DiskFormatter", 1, NewDiskFormatterAPI)
}

var logger = loggo.GetLogger("juju.apiserver.diskformatter")

// DiskFormatterAPI provides access to the DiskFormatter API facade.
type DiskFormatterAPI struct {
	st          stateInterface
	resources   *common.Resources
	authorizer  common.Authorizer
	getAuthFunc common.GetAuthFunc
}

var getState = func(st *state.State) stateInterface {
	return stateShim{st}
}

// NewDiskFormatterAPI creates a new client-side DiskFormatter API facade.
func NewDiskFormatterAPI(
	st *state.State,
	resources *common.Resources,
	authorizer common.Authorizer,
) (*DiskFormatterAPI, error) {

	if !authorizer.AuthMachineAgent() {
		return nil, common.ErrPerm
	}

	getAuthFunc := func() (common.AuthFunc, error) {
		return authorizer.AuthOwner, nil
	}

	return &DiskFormatterAPI{
		st:          getState(st),
		resources:   resources,
		authorizer:  authorizer,
		getAuthFunc: getAuthFunc,
	}, nil
}

// WatchAttachedBlockDevices returns a NotifyWatcher for observing changes
// to each unit's attached block devices.
func (a *DiskFormatterAPI) WatchAttachedBlockDevices(args params.Entities) (params.StringsWatchResults, error) {
	result := params.StringsWatchResults{
		Results: make([]params.StringsWatchResult, len(args.Entities)),
	}
	canAccess, err := a.getAuthFunc()
	if err != nil {
		return params.StringsWatchResults{}, err
	}
	for i, entity := range args.Entities {
		unit, err := names.ParseUnitTag(entity.Tag)
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		err = common.ErrPerm
		var watcherId string
		var changes []string
		if canAccess(unit) {
			watcherId, changes, err = a.watchOneAttachedBlockDevices(unit)
		}
		result.Results[i].StringsWatcherId = watcherId
		result.Results[i].Changes = changes
		result.Results[i].Error = common.ServerError(err)
	}
	return result, nil
}

func (a *DiskFormatterAPI) watchOneAttachedBlockDevices(tag names.UnitTag) (string, []string, error) {
	w, err := a.st.WatchAttachedBlockDevices(tag.Id())
	if err != nil {
		return "", nil, err
	}
	// Consume the initial event. Technically, API
	// calls to Watch 'transmit' the initial event
	// in the Watch response.
	if changes, ok := <-w.Changes(); ok {
		return a.resources.Register(w), changes, nil
	}
	return "", nil, watcher.EnsureErr(w)
}

// BlockDevice returns details about each specified block device.
func (a *DiskFormatterAPI) BlockDevice(args params.Entities) (params.BlockDeviceResults, error) {
	result := params.BlockDeviceResults{
		Results: make([]params.BlockDeviceResult, len(args.Entities)),
	}
	canAccess, err := a.getAuthFunc()
	if err != nil {
		return params.BlockDeviceResults{}, err
	}
	for i, entity := range args.Entities {
		diskTag, err := names.ParseDiskTag(entity.Tag)
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		blockDevice, err := a.st.BlockDevice(diskTag.Id())
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		unit := blockDevice.Unit()
		if unit != "" && canAccess(names.NewUnitTag(unit)) {
			result.Results[i].Result = storageBlockDevice(blockDevice)
		} else {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
		}
	}
	return result, nil
}

func (a *DiskFormatterAPI) BlockDeviceDatastore(args params.Entities) (params.DatastoreResults, error) {
	result := params.DatastoreResults{
		Results: make([]params.DatastoreResult, len(args.Entities)),
	}
	canAccess, err := a.getAuthFunc()
	if err != nil {
		return params.DatastoreResults{}, err
	}
	for i, entity := range args.Entities {
		diskTag, err := names.ParseDiskTag(entity.Tag)
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		blockDevice, err := a.st.BlockDevice(diskTag.Id())
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		unit := blockDevice.Unit()
		if unit == "" || !canAccess(names.NewUnitTag(unit)) {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		datastore, err := a.st.Datastore(blockDevice.Datastore())
		if err != nil {
			logger.Errorf("cannot get datastore: %v", err)
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		result.Results[i].Result = *datastore
	}
	return result, nil
}

func (a *DiskFormatterAPI) SetBlockDeviceFilesystem(args params.SetBlockDeviceFilesystem) (params.ErrorResults, error) {
	result := params.ErrorResults{
		Results: make([]params.ErrorResult, len(args.BlockDeviceFilesystems)),
	}
	canAccess, err := a.getAuthFunc()
	if err != nil {
		return params.ErrorResults{}, err
	}
	setFilesystem := func(fs params.BlockDeviceFilesystem) error {
		diskTag, err := names.ParseDiskTag(fs.BlockDeviceTag)
		if err != nil {
			return common.ErrPerm
		}
		blockDevice, err := a.st.BlockDevice(diskTag.Id())
		if err != nil {
			return common.ErrPerm
		}
		unit := blockDevice.Unit()
		if unit == "" || !canAccess(names.NewUnitTag(unit)) {
			return common.ErrPerm
		}
		// A datastore's block-device may not change, but a block-device
		// can be reassigned to a new datastore. We must ensure that the
		// block-device is still assigned to the datastore that the client
		// thought it was assigned to.
		if fs.Datastore == "" || blockDevice.Datastore() != fs.Datastore {
			return common.ErrPerm
		}
		return a.st.SetDatastoreFilesystem(blockDevice.Datastore(), stateFilesystem(fs.Filesystem))
	}
	for i, fs := range args.BlockDeviceFilesystems {
		result.Results[i].Error = common.ServerError(setFilesystem(fs))
	}
	return result, nil
}

// NOTE: purposefully not using field keys below, so
// the code breaks if structures change.
// this breaks if changes.

func stateFilesystem(fs storage.Filesystem) state.Filesystem {
	return state.Filesystem{
		fs.Type,
		fs.MountOptions,
	}
}

func storageBlockDevice(dev state.BlockDevice) storage.BlockDevice {
	info := dev.Info()
	return storage.BlockDevice{
		dev.Name(),
		info.DeviceName,
		info.Label,
		info.UUID,
		info.Serial,
		info.Size,
		info.InUse,
	}
}
