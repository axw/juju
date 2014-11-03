// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage

import (
	"gopkg.in/juju/charm.v4"
	"launchpad.net/loggo"
)

var logger = loggo.GetLogger("juju.storage")

type StorageState int

const (
	StorageStateUnknown StorageState = iota
	StorageStateAttaching
	StorageStateAttached
	StorageStateDetaching
	StorageStateDetached
)

// Storage describes charm storage allocated to a unit.
type Storage struct {
	Type charm.StorageType `yaml:"type"`

	// BlockDevice is the associated block device, if any. If this
	// is non-nil and Type is StorageFilesystem, then Juju manages
	// the creation of the filesystem on the block device. This will
	// be nil for remote filesystems.
	BlockDevice *BlockDevice `yaml:"blockdevice,omitempty"`

	// Name is the charm storage name associated with the storage.
	// For charm storage with >1 count, this identifies the group.
	Name string `yaml:"name"`

	// Path is the unique filesystem path to the storage on the machine.
	// For block devices, this identifies the device; for filesystems,
	// this identifies the mount point.
	Path string `yaml:"path"`

	// State is the state of the Storage.
	State StorageState `yaml:"state"`
}

// BlockDevice describes a block device (disk, logical volume, etc.)
type BlockDevice struct {
	// DeviceName is the block device's OS-specific name (e.g. "sdb").
	DeviceName string `yaml:"devicename,omitempty"`

	// DeviceUUID is a unique identifier for the block device. Not all block
	// devices have UUIDs, so this may be empty. Even for block devices that
	// do have UUIDs the UUID may not initially be known to Juju; the UUID will
	// eventually be populated by Juju.
	//
	// We must cater for LVM UUIDs here, which have a different format than
	// the standard v4 UUIDs for example.
	DeviceUUID string `yaml:"deviceuuid,omitempty"`

	// StorageName is the storage group name associated with the device when
	// it was added to the system. For discovered devices that have not yet
	// been attached to a unit, this will be empty.
	StorageName string `yaml:"storagename"`
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
