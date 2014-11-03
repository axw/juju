// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"gopkg.in/mgo.v2/bson"

	"github.com/juju/juju/storage"
)

// unitStorageDoc represents storage attached to a unit.
type unitStorageDoc struct {
	Id     bson.ObjectId `bson:"_id"`
	UnitId string        `bson:"unitid"`
	Name   string        `bson:"name"`
	Path   string        `bson:"path"`

	// BlockDeviceId is the document ID for the associated
	// block device, if any.
	BlockDeviceId string `bson:"blockdeviceid"`

	BlockDevice *blockDevice `bson:"blockdevice,omitempty"`
	Filesystem  *filesystem  `bson:"filesystem,omitempty"`
}

type blockDevice struct {
	DeviceName string `bson:"devicename"`
	//DiskID     string `bson:"diskid"`
	DiskUUID string `bson:"diskuuid"`
	State    int    `bson:"state"`
}

type filesystem struct {
	Type         string   `bson:"type"`
	MkfsOptions  []string `bson:"mkfsoptions,omitempty"`
	MountOptions []string `bson:"mountoptions,omitempty"`
	State        int      `bson:"state"`
}

func newUnitStorageDoc(info storage.Storage) *unitStorageDoc {
	var fs *filesystem
	var dev *blockDevice
	if info.Filesystem != nil {
		fs = &filesystem{
			Type:         info.Filesystem.Type,
			MkfsOptions:  info.Filesystem.MkfsOptions,
			MountOptions: info.Filesystem.MountOptions,
			State:        int(info.Filesystem.State),
		}
	}
	if info.BlockDevice != nil {
		dev = &blockDevice{
			DeviceName: info.BlockDevice.DeviceName,
			DiskUUID:   info.BlockDevice.DeviceUUID, // FIXME
			State:      int(info.BlockDevice.State),
		}
	}
	return &unitStorageDoc{
		Name:        info.Name,
		Path:        info.Path,
		Filesystem:  fs,
		BlockDevice: dev,
	}
}

func newUnitStorage(doc *unitStorageDoc) storage.Storage {
	var fs *storage.Filesystem
	var dev *storage.BlockDevice
	if doc.Filesystem != nil {
		fs = &storage.Filesystem{
			Type:         doc.Filesystem.Type,
			MkfsOptions:  doc.Filesystem.MkfsOptions,
			MountOptions: doc.Filesystem.MountOptions,
			State:        storage.FilesystemState(doc.Filesystem.State),
		}
	}
	if doc.BlockDevice != nil {
		dev = &storage.BlockDevice{
			DeviceName: doc.BlockDevice.DeviceName,
			DeviceUUID: doc.BlockDevice.DiskUUID, // FIXME
			State:      storage.BlockDeviceState(doc.BlockDevice.State),
		}
	}
	return storage.Storage{
		Name:        doc.Name,
		Path:        doc.Path,
		Filesystem:  fs,
		BlockDevice: dev,
	}
}

/*
func createUnitStorageOps(machineId, unitId string, arg storage.Storage) []txn.Op {
	var ops []txn.Op
	doc := newUnitStorageDoc(arg)
	doc.MachineId = machineId
	doc.Id = bson.NewObjectId()
	if arg.BlockDevice != nil {
		doc.BlockDeviceId, ops = createBlockDeviceOps(machineId, arg.BlockDevice)
	}
	ops = append(ops, txn.Op{
		C:      unitStoragesC,
		Id:     doc.Id,
		Assert: txn.DocMissing,
		Insert: doc,
	})
	return ops
}
*/
