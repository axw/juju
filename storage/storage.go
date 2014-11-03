// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage

import (
	"gopkg.in/juju/charm.v4"
	"launchpad.net/loggo"
)

var logger = loggo.GetLogger("juju.storage")

// BlockDeviceState identifies the state of a block device.
type BlockDeviceState int

const (
	BlockDeviceStateUnknown BlockDeviceState = iota
	BlockDeviceStateAttaching
	BlockDeviceStateAttached
	BlockDeviceStateDetaching
	BlockDeviceStateDetached
)

func (s BlockDeviceState) String() string {
	switch s {
	case BlockDeviceStateAttaching:
		return "BlockDeviceStateAttaching"
	case BlockDeviceStateAttached:
		return "BlockDeviceStateAttached"
	case BlockDeviceStateDetaching:
		return "BlockDeviceStateDetaching"
	case BlockDeviceStateDetached:
		return "BlockDeviceStateDetached"
	default:
		return "BlockDeviceStateUnknown"
	}
}

// FilesystemState identifies the state of a filesystem.
type FilesystemState int

const (
	FilesystemStateUnknown FilesystemState = iota
	FilesystemStateCreating
	FilesystemStateMounting
	FilesystemStateMounted
	FilesystemStateUnmounting
	FilesystemStateUnmounted
)

func (s FilesystemState) String() string {
	switch s {
	case FilesystemStateCreating:
		return "FilesystemStateCreating"
	case FilesystemStateMounting:
		return "FilesystemStateMounting"
	case FilesystemStateMounted:
		return "FilesystemStateMounted"
	case FilesystemStateUnmounting:
		return "FilesystemStateUnmounting"
	case FilesystemStateUnmounted:
		return "FilesystemStateUnmounted"
	default:
		return "FilesystemStateUnknown"
	}
}

// Storage describes charm storage allocated to a unit.
type Storage struct {
	Type charm.StorageType `yaml:"type"`

	// Name is the charm storage name associated with the storage.
	// For charm storage with >1 count, this identifies the group.
	Name string `yaml:"name"`

	// Path is the unique filesystem path to the storage on the machine.
	// For block devices, this identifies the device; for filesystems,
	// this identifies the mount point.
	Path string `yaml:"path"`

	// BlockDevice is the associated block device, if any.
	//
	// If this is non-nil and Type is StorageFilesystem, then Juju manages
	// the creation of the filesystem on the block device. This will be nil
	// for remote filesystems.
	BlockDevice *BlockDevice `yaml:"blockdevice,omitempty"`

	// Filesystem contains filesystem information for StorageFilesystem
	// Type storage, and is nil otherwise.
	Filesystem *Filesystem `yaml:"filesystem,omitempty"`
}

// BlockDevice describes a block device (disk, logical volume, etc.)
type BlockDevice struct {
	// DeviceName is the block device's OS-specific name (e.g. "sdb").
	DeviceName string `yaml:"devicename,omitempty"`

	// State is the state of the block device.
	State BlockDeviceState `yaml:"state"`

	// DeviceUUID is a unique identifier for the block device. Not all block
	// devices have UUIDs, so this may be empty. Even for block devices that
	// do have UUIDs the UUID may not initially be known to Juju; the UUID will
	// eventually be populated by Juju.
	//
	// We must cater for LVM UUIDs here, which have a different format than
	// the standard v4 UUIDs for example.
	DeviceUUID string `yaml:"deviceuuid,omitempty"`

	// TODO(axw) remove this; we should just store Storage in state and do
	// away with the blockdevices collection. One collection for both fs
	// and block.
	//
	// StorageName is the storage group name associated with the device when
	// it was added to the system. For discovered devices that have not yet
	// been attached to a unit, this will be empty.
	StorageName string `yaml:"storagename"`
}

type Filesystem struct {
	Type         string
	MkfsOptions  []string
	MountOptions []string

	// State is the state of the filesystem.
	State FilesystemState `yaml:"state"`
}

// String implements fmt.Stringer.
func (d BlockDevice) String() string {
	s := d.DeviceName
	if d.DeviceUUID != "" {
		if s != "" {
			s += "/"
		}
		s += d.DeviceUUID
	}
	return s
}
