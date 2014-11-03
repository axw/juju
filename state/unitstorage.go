// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"

	"github.com/juju/juju/storage"
)

// unitStorageDoc represents storage attached to a unit.
type unitStorageDoc struct {
	Id     bson.ObjectId `bson:"_id"`
	UnitId string        `bson:"unitid"`

	// BlockDeviceId is the document ID for the associated
	// block device, if any.
	BlockDeviceId string `bson:"blockdeviceid"`

	Name  string `bson:"name"`
	Path  string `bson:"path"`
	State int    `bson:"state"`
}

func newUnitStorageDoc(info storage.UnitStorage) *unitStorageDoc {
	return &unitStorageDoc{
		DeviceName:  info.DeviceName,
		DeviceUUID:  info.DeviceUUID,
		StorageName: info.StorageName,
	}
}

func newUnitStorage(doc *unitStorageDoc) storage.Storage {
	return storage.Storage{
		DeviceName:  doc.DeviceName,
		DeviceUUID:  doc.DeviceUUID,
		StorageName: doc.StorageName,
	}
}

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
