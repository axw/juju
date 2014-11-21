// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage

// BlockDeviceId is a source-specific identifier for a block device
// (e.g. EBS volume ID).
//
// BlockDeviceId values are required to be globally unique, so if the
// IDs allocated by a source are only unique to the source, the ID must
// incorporate a unique identifier for the source itself.
type BlockDeviceId string

// BlockDevice describes a block device (disk, logical volume, etc.)
type BlockDevice struct {
	// Id is an identifier assigned by the block device source.
	Id BlockDeviceId `yaml:"id"`

	// DeviceName is the block device's OS-specific name (e.g. "sdb").
	DeviceName string `yaml:"devicename,omitempty"`

	// Label is the label for the filesystem on the block device.
	//
	// This will be empty if the block device does not have a filesystem,
	// or if the filesystem is not yet known to Juju.
	Label string `yaml:"label,omitempty"`

	// UUID is a unique identifier for the filesystem on the block device.
	//
	// This will be empty if the block device does not have a filesystem,
	// or if the filesystem is not yet known to Juju.
	//
	// The UUID format is not necessarily uniform; for example, LVM UUIDs
	// differ in format to the standard v4 UUIDs.
	UUID string `yaml:"uuid,omitempty"`

	// Serial is the block device's serial number. Not all block devices
	// have a serial number. This is used to identify a block device if
	// it is available, in preference to UUID or device name, as the serial
	// is immutable.
	Serial string `yaml:"serial,omitempty"`

	// Size is the size of the block device, in MiB.
	Size uint64 `yaml:"size"`

	// InUse indicates that the block device is in use (e.g. mounted).
	InUse bool `yaml:"inuse"`
}

// BlockDevicesSame reports whether or not two block devices are the same.
//
// In descending order of preference, we use: serial number, filesystem UUID,
// device name.
func BlockDevicesSame(a, b BlockDevice) bool {
	if a.Serial != "" && b.Serial != "" {
		return a.Serial == b.Serial
	}
	if a.UUID != "" && b.UUID != "" {
		return a.UUID == b.UUID
	}
	return a.DeviceName != "" && a.DeviceName == b.DeviceName
}
