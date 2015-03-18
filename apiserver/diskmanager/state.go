// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskmanager

import (
	"github.com/juju/juju/state"
	"github.com/juju/names"
)

type stateInterface interface {
	Filesystem(names.FilesystemTag) (state.Filesystem, error)
	FilesystemAttachment(names.MachineTag, names.FilesystemTag) (state.FilesystemAttachment, error)
	MachineFilesystemAttachments(names.MachineTag) ([]state.FilesystemAttachment, error)
	Volume(names.VolumeTag) (state.Volume, error)
	VolumeAttachment(names.MachineTag, names.VolumeTag) (state.VolumeAttachment, error)
	SetMachineBlockDevices(machineId string, devices []state.BlockDeviceInfo) error
	SetFilesystemAttachmentInfo(names.MachineTag, names.FilesystemTag, state.FilesystemAttachmentInfo) error
}

type stateShim struct {
	*state.State
}

func (s stateShim) SetMachineBlockDevices(machineId string, devices []state.BlockDeviceInfo) error {
	m, err := s.State.Machine(machineId)
	if err != nil {
		return err
	}
	return m.SetMachineBlockDevices(devices...)
}
