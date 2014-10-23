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
}

func newBlockDeviceDoc(info storage.BlockDevice) *blockDeviceDoc {
	// This does not set the machine id.
	return &blockDeviceDoc{
		DeviceName:  info.DeviceName,
		DeviceUUID:  info.DeviceUUID,
		StorageName: info.StorageName,
	}
}

func newBlockDevice(doc *blockDeviceDoc) storage.BlockDevice {
	return storage.BlockDevice{
		DeviceName:  doc.DeviceName,
		DeviceUUID:  doc.DeviceUUID,
		StorageName: doc.StorageName,
	}
}

func createBlockDevicesOps(machineId string, args []storage.BlockDevice) []txn.Op {
	ops := make([]txn.Op, len(args))
	for i, arg := range args {
		doc := newBlockDeviceDoc(arg)
		doc.MachineId = machineId
		doc.Id = bson.NewObjectId()
		ops[i] = txn.Op{
			C:      blockDevicesC,
			Id:     doc.Id,
			Assert: txn.DocMissing,
			Insert: doc,
		}
	}
	return ops
}
