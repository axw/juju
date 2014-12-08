// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"fmt"

	"github.com/juju/errors"
	"github.com/juju/names"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"
)

// BlockDevice represents the state of a block device attached to a machine.
type BlockDevice interface {
	// Tag returns the tag for the block device.
	Tag() names.Tag

	// Name returns the unique name of the block device.
	Name() string

	// Machine returns the ID of the machine the block device is attached to.
	Machine() string

	// Datastore returns the name of the block device's assigned unit.
	Unit() string

	// Datastore returns the name of the block device's assigned datastore.
	Datastore() string

	// Info returns the block device's BlockDeviceInfo.
	Info() BlockDeviceInfo
}

type blockDevice struct {
	doc blockDeviceDoc
}

// blockDeviceDoc records information about a disk attached to a machine.
type blockDeviceDoc struct {
	DocID   string `bson:"_id"`
	Name    string `bson:"name"`
	EnvUUID string `bson:"env-uuid"`
	Machine string `bson:"machine"`

	// Unit and Datastore are set together. Unit is redundant, but simplifies
	// watching and querying for block devices that are assigned to datastores
	// owned by a specific unit.
	Unit      string `bson:"unit,omitempty"`
	Datastore string `bson:"datastore,omitempty"`

	Info BlockDeviceInfo `bson:"info"`
}

// BlockDeviceInfo describes information about a block device.
type BlockDeviceInfo struct {
	DeviceName string `bson:"devicename,omitempty"`
	Label      string `bson:"label,omitempty"`
	UUID       string `bson:"uuid,omitempty"`
	Serial     string `bson:"serial,omitempty"`
	Size       uint64 `bson:"size"`
	InUse      bool   `bson:"inuse"`
}

func (b *blockDevice) Tag() names.Tag {
	return names.NewDiskTag(b.doc.Name)
}

func (b *blockDevice) Name() string {
	return b.doc.Name
}

func (b *blockDevice) Machine() string {
	return b.doc.Machine
}

func (b *blockDevice) Unit() string {
	return b.doc.Unit
}

func (b *blockDevice) Datastore() string {
	return b.doc.Datastore
}

func (b *blockDevice) Info() BlockDeviceInfo {
	return b.doc.Info
}

// BlockDevice returns the BlockDevice with the specified name.
func (st *State) BlockDevice(diskName string) (BlockDevice, error) {
	blockDevices, cleanup := st.getCollection(blockDevicesC)
	defer cleanup()

	var d blockDevice
	err := blockDevices.FindId(st.docID(diskName)).One(&d.doc)
	if err == mgo.ErrNotFound {
		return nil, errors.NotFoundf("block device %q", diskName)
	} else if err != nil {
		return nil, errors.Annotate(err, "cannot get block device details")
	}
	return &d, nil
}

// newDiskName returns the next disk name for a machine.
func newDiskName(st *State, machineId string) (string, error) {
	machines, closer := st.getCollection(machinesC)
	defer closer()

	change := mgo.Change{Update: bson.D{{"$inc", bson.D{{"diskseq", 1}}}}}
	var doc machineDoc
	_, err := machines.FindId(st.docID(machineId)).Apply(change, &doc)
	if err == mgo.ErrNotFound {
		return "", errors.NotFoundf("machine %q", machineId)
	} else if err != nil {
		return "", errors.Annotate(err, "cannot increment disk sequence")
	}
	return fmt.Sprintf("%s#%d", machineId, doc.DiskSeq), nil
}

// setMachineBlockDevices updates the blockdevices collection with the
// currently attached block devices. Previously recorded block devices not in
// the list will be removed.
//
// The Name field of each BlockDevice is ignored, if specified. Block devices
// are matched according to their identifying attributes (device name, UUID,
// etc.), and the existing Name will be retained.
func setMachineBlockDevices(st *State, machineId string, newInfo []BlockDeviceInfo) error {
	buildTxn := func(attempt int) ([]txn.Op, error) {
		oldDevices, err := getMachineBlockDevices(st, machineId)
		if err != nil && err != mgo.ErrNotFound {
			return nil, errors.Trace(err)
		}

		ops := []txn.Op{{
			C:      machinesC,
			Id:     st.docID(machineId),
			Assert: isAliveDoc,
		}}

		// Create ops to update and remove existing block devices.
		found := make([]bool, len(newInfo))
		for _, oldDev := range oldDevices {
			var updated bool
			for j, newInfo := range newInfo {
				if found[j] {
					continue
				}
				if blockDevicesSame(oldDev.Info(), newInfo) {
					// Merge the two structures by replacing the old document's
					// BlockDeviceInfo with the new one.
					if oldDev.doc.Info != newInfo {
						ops = append(ops, txn.Op{
							C:      blockDevicesC,
							Id:     oldDev.doc.DocID,
							Assert: txn.DocExists,
							Update: bson.D{{"$set", bson.D{
								{"info", newInfo},
							}}},
						})
					}
					found[j] = true
					updated = true
					break
				}
			}
			if !updated {
				ops = append(ops, txn.Op{
					C:      blockDevicesC,
					Id:     oldDev.doc.DocID,
					Assert: txn.DocExists,
					Remove: true,
				})
			}
		}

		// Create ops to insert new block devices.
		for i, info := range newInfo {
			if found[i] {
				continue
			}
			name, err := newDiskName(st, machineId)
			if err != nil {
				return nil, errors.Annotate(err, "cannot generate disk name")
			}
			var newDoc blockDeviceDoc
			newDoc.Name = name
			newDoc.Machine = machineId
			newDoc.EnvUUID = st.EnvironUUID()
			newDoc.DocID = st.docID(name)
			newDoc.Info = info
			ops = append(ops, txn.Op{
				C:      blockDevicesC,
				Id:     newDoc.DocID,
				Assert: txn.DocMissing,
				Insert: newDoc,
			})
		}

		return ops, nil
	}
	return st.run(buildTxn)
}

func getMachineBlockDevices(st *State, machineId string) ([]*blockDevice, error) {
	sel := bson.D{
		{"env-uuid", st.EnvironUUID()},
		{"machine", machineId},
	}
	blockDevices, closer := st.getCollection(blockDevicesC)
	defer closer()

	var docs []blockDeviceDoc
	err := blockDevices.Find(sel).All(&docs)
	if err != nil {
		return nil, errors.Trace(err)
	}
	devices := make([]*blockDevice, len(docs))
	for i, doc := range docs {
		devices[i] = &blockDevice{doc}
	}
	return devices, nil
}

func removeMachineBlockDevicesOps(st *State, machineId string) ([]txn.Op, error) {
	sel := bson.D{
		{"env-uuid", st.EnvironUUID()},
		{"machine", machineId},
	}
	blockDevices, closer := st.getCollection(blockDevicesC)
	defer closer()

	iter := blockDevices.Find(sel).Select(bson.D{
		{"_id", 1},
		{"datastore", 1},
	}).Iter()
	defer iter.Close()
	var ops []txn.Op
	var doc blockDeviceDoc
	for iter.Next(&doc) {
		ops = append(ops, txn.Op{
			C:      blockDevicesC,
			Id:     doc.DocID,
			Remove: true,
		})
		if doc.Datastore != "" {
			ops = append(ops, txn.Op{
				C:      datastoresC,
				Id:     st.docID(doc.Datastore),
				Remove: true,
			})
		}
	}
	return ops, errors.Trace(iter.Close())
}

// blockDevicesSame reports whether or not two BlockDevices identify the
// same block device.
//
// In descending order of preference, we use: serial number, filesystem
// UUID, device name.
func blockDevicesSame(a, b BlockDeviceInfo) bool {
	if a.Serial != "" && b.Serial != "" {
		return a.Serial == b.Serial
	}
	if a.UUID != "" && b.UUID != "" {
		return a.UUID == b.UUID
	}
	return a.DeviceName != "" && a.DeviceName == b.DeviceName
}
