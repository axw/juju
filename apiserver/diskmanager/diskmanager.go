// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskmanager

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state"
	"github.com/juju/juju/storage"
)

func init() {
	common.RegisterStandardFacade("DiskManager", 1, NewDiskManagerAPI)
}

var logger = loggo.GetLogger("juju.apiserver.diskmanager")

// DiskManagerAPI provides access to the DiskManager API facade.
type DiskManagerAPI struct {
	st          stateInterface
	authorizer  common.Authorizer
	getAuthFunc common.GetAuthFunc
}

var getState = func(st *state.State) stateInterface {
	return stateShim{st}
}

// NewDiskManagerAPI creates a new server-side DiskManager API facade.
func NewDiskManagerAPI(
	st *state.State,
	resources *common.Resources,
	authorizer common.Authorizer,
) (*DiskManagerAPI, error) {

	if !authorizer.AuthMachineAgent() {
		return nil, common.ErrPerm
	}

	authEntityTag := authorizer.GetAuthTag()
	getAuthFunc := func() (common.AuthFunc, error) {
		return func(tag names.Tag) bool {
			// A machine agent can always access its own machine.
			return tag == authEntityTag
		}, nil
	}

	return &DiskManagerAPI{
		st:          getState(st),
		authorizer:  authorizer,
		getAuthFunc: getAuthFunc,
	}, nil
}

func (d *DiskManagerAPI) SetMachineBlockDevices(args params.SetMachineBlockDevices) (params.ErrorResults, error) {
	result := params.ErrorResults{
		Results: make([]params.ErrorResult, len(args.MachineBlockDevices)),
	}
	canAccess, err := d.getAuthFunc()
	if err != nil {
		return result, err
	}
	for i, arg := range args.MachineBlockDevices {
		tag, err := names.ParseMachineTag(arg.Machine)
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		if !canAccess(tag) {
			err = common.ErrPerm
		} else {
			// TODO(axw) create volumes for block devices without matching
			// volumes, if and only if the block device has a serial. Under
			// the assumption of unique (to a machine) serial IDs, this
			// gives us a guaranteed *persistently* unique way of identifying
			// the volume.
			//
			// NOTE: we must predicate the above on there being no unprovisioned
			// volume attachments for the machine, otherwise we would have
			// a race between the volume attachment info being recorded and
			// the diskmanager publishing block devices and erroneously creating
			// volumes.
			blockDevices := stateBlockDeviceInfo(arg.BlockDevices)
			err = d.setMachineBlockDevices(tag, blockDevices)
		}
		result.Results[i].Error = common.ServerError(err)
	}
	return result, nil
}

func (d *DiskManagerAPI) setMachineBlockDevices(
	machineTag names.MachineTag, blockDevices []state.BlockDeviceInfo,
) error {
	err := d.st.SetMachineBlockDevices(machineTag.Id(), blockDevices)
	if err != nil {
		return errors.Trace(err)
	}
	// TODO(axw) when we set volume info, we need
	// to also set filesystem info if there is one
	// backed by the volume. This can be done in
	// the state package.
	return d.setMachineFilesystemAttachmentInfo(machineTag, blockDevices)
}

// setMachineFilesystemAttachmentInfo sets the filesystem attachment info
// for filesystems backed by volumes, by matching block device info with
// said volumes.
func (d *DiskManagerAPI) setMachineFilesystemAttachmentInfo(
	machineTag names.MachineTag, blockDevices []state.BlockDeviceInfo,
) error {
	attachments, err := d.st.MachineFilesystemAttachments(machineTag)
	if err != nil {
		return errors.Annotate(err, "getting machine filesystem attachments")
	}
	for _, attachment := range attachments {
		if _, err := attachment.Info(); !errors.IsNotProvisioned(err) {
			// filesystem attachment info already set
			continue
		}
		filesystem, err := d.st.Filesystem(attachment.Filesystem())
		if err != nil {
			return errors.Annotate(err, "getting filesystem")
		}
		volumeTag, err := filesystem.Volume()
		if errors.Cause(err) == state.ErrNoBackingVolume {
			// filesystem is not backed by a volume
			continue
		}
		volume, err := d.st.Volume(volumeTag)
		if err != nil {
			return errors.Annotate(err, "getting volume")
		}
		volumeInfo, err := volume.Info()
		if errors.IsNotProvisioned(err) {
			// volume has not been provisioned
			continue
		} else if err != nil {
			return errors.Annotate(err, "getting volume info")
		}
		volumeAttachment, err := d.st.VolumeAttachment(machineTag, volumeTag)
		if err != nil {
			return errors.Annotate(err, "getting volume")
		}
		volumeAttachmentInfo, err := volumeAttachment.Info()
		if errors.IsNotProvisioned(err) {
			// volume attachment has not been provisioned
			continue
		} else if err != nil {
			return errors.Annotate(err, "getting volume attachment info")
		}
		blockDevice, ok := common.MatchingBlockDevice(
			blockDevices, volumeInfo, volumeAttachmentInfo,
		)
		if !ok || blockDevice.MountPoint == "" {
			// none of the block devices match the volume/attachment,
			// or the block device doesn't yet have a filesystem.
			continue
		}
		err = d.st.SetFilesystemAttachmentInfo(
			machineTag, attachment.Filesystem(),
			state.FilesystemAttachmentInfo{
				blockDevice.MountPoint,
			},
		)
		if err != nil {
			return errors.Annotate(err, "setting filesystem attachment info")
		}
		logger.Debugf(
			"set info for filesystem %s on machine %s",
			filesystem.Tag().Id(),
			machineTag.Id(),
		)
	}
	return nil
}

func stateBlockDeviceInfo(devices []storage.BlockDevice) []state.BlockDeviceInfo {
	result := make([]state.BlockDeviceInfo, len(devices))
	for i, dev := range devices {
		result[i] = state.BlockDeviceInfo{
			dev.DeviceName,
			dev.Label,
			dev.UUID,
			dev.Serial,
			dev.Size,
			dev.FilesystemType,
			dev.InUse,
			dev.MountPoint,
		}
	}
	return result
}
