// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"

	"github.com/juju/juju/storage"
)

// blockDeviceDoc represents a block device attached to a machine.
type blockDeviceDoc struct {
	Id          bson.ObjectId `bson:"_id"`
	MachineId   string        `bson:"machineid"`
	DeviceName  string        `bson:"devicename,omitempty"`
	DeviceUUID  string        `bson:"deviceuuid,omitempty"`
	StorageName string        `bson:"storagename"`
	// TODO(axw) source: provider or charm? e.g. may be RBD-based.
}

func newBlockDeviceDoc(info storage.BlockDevice) *blockDeviceDoc {
	return &blockDeviceDoc{
		DeviceName: info.DeviceName,
		DeviceUUID: info.DeviceUUID,
		//StorageName: info.StorageName,
	}
}

func newBlockDevice(doc *blockDeviceDoc) storage.BlockDevice {
	return storage.BlockDevice{
		DeviceName: doc.DeviceName,
		DeviceUUID: doc.DeviceUUID,
		//StorageName: doc.StorageName,
	}
}

func createBlockDeviceOps(machineId string, arg storage.BlockDevice) (bson.ObjectId, []txn.Op) {
	doc := newBlockDeviceDoc(arg)
	doc.MachineId = machineId
	doc.Id = bson.NewObjectId()
	ops := []txn.Op{{
		C:      blockDevicesC,
		Id:     doc.Id,
		Assert: txn.DocMissing,
		Insert: doc,
	}}
	return doc.Id, ops
}

func createBlockDevicesOps(machineId string, args []storage.BlockDevice) []txn.Op {
	ops := make([]txn.Op, 0, len(args))
	for _, arg := range args {
		_, deviceOps := createBlockDeviceOps(machineId, arg)
		ops = append(ops, deviceOps...)
	}
	return ops
}
