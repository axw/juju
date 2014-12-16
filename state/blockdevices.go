// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"fmt"

	"github.com/juju/errors"
	"github.com/juju/juju/storage"
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

	// Machine returns the ID of the machine the block device
	// is associated with, or ("", false) if the block device
	// is not associated with any machine.
	Machine() (string, bool)

	// Info returns the block device's BlockDeviceInfo, or a
	// NotProvisionedError if the disk has not yet been provisioned.
	Info() (*BlockDeviceInfo, error)

	// Constraints returns the constraints used to provision the block device.
	Constraints() (storage.Constraints, error)
}

type blockDevice struct {
	doc blockDeviceDoc
}

// blockDeviceDoc records information about a disk attached to a machine.
type blockDeviceDoc struct {
	DocID   string           `bson:"_id"`
	Name    string           `bson:"name"`
	EnvUUID string           `bson:"env-uuid"`
	Machine string           `bson:"machine"`
	Info    *BlockDeviceInfo `bson:"info,omitempty"`
}

// BlockDeviceInfo describes information about a block device.
type BlockDeviceInfo struct {
	ProviderId string `bson:"providerid"`
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

func (b *blockDevice) Machine() (string, bool) {
	return b.doc.Machine, b.doc.Machine != ""
}

func (b *blockDevice) Info() (*BlockDeviceInfo, error) {
	if b.doc.Info == nil {
		return nil, NotProvisionedf("block device %q", b.Name)
	}
	return b.doc.Info, nil
}

func (b *blockDevice) Constraints() (storage.Constraints, error) {
	// TODO(axw) implement this properly.
	return storage.Constraints{}, nil
}

// BlockDevice returns the BlockDevice with the specified name.
func (st *State) BlockDevice(diskName string) (BlockDevice, error) {
	blockDevices, cleanup := st.getCollection(blockDevicesC)
	defer cleanup()

	var d blockDevice
	err := blockDevices.FindId(diskName).One(&d.doc)
	if err == mgo.ErrNotFound {
		return nil, errors.NotFoundf("block device %q", diskName)
	} else if err != nil {
		return nil, errors.Annotate(err, "cannot get block device details")
	}
	return &d, nil
}

// newDiskName returns a unique disk name.
func newDiskName(st *State) (string, error) {
	seq, err := st.sequence("disk")
	if err != nil {
		return "", errors.Trace(err)
	}
	return fmt.Sprint(seq), nil
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
				if oldDev.doc.Info == nil {
					// This should never happen.
					logger.Warningf("machine block device %q has no Info", oldDev.Name())
					continue
				}
				if blockDevicesSame(oldDev.doc.Info, &newInfo) {
					// Merge the two structures by replacing the old document's
					// BlockDeviceInfo with the new one.
					if *oldDev.doc.Info != newInfo {
						ops = append(ops, txn.Op{
							C:      blockDevicesC,
							Id:     oldDev.doc.DocID,
							Assert: txn.DocExists,
							Update: bson.D{{"$set", bson.D{
								{"info", &newInfo},
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
			name, err := newDiskName(st)
			if err != nil {
				return nil, errors.Annotate(err, "cannot generate disk name")
			}
			info := info // new address
			newDoc := blockDeviceDoc{
				Name:    name,
				Machine: machineId,
				EnvUUID: st.EnvironUUID(),
				DocID:   st.docID(name),
				Info:    &info,
			}
			ops = append(ops, txn.Op{
				C:      blockDevicesC,
				Id:     newDoc.DocID,
				Assert: txn.DocMissing,
				Insert: &newDoc,
			})
		}

		return ops, nil
	}
	return st.run(buildTxn)
}

func getMachineBlockDevices(st *State, machineId string) ([]*blockDevice, error) {
	sel := bson.D{{"machine", machineId}}
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

func createRequestedMachineBlockDeviceOps(st *State, machineId string, constraints ...storage.Constraints) ([]txn.Op, error) {
	for _, cons := range constraints {
		// machine-level storage constraints are always
		// singular (Count=1) and required.
		if cons.Minimum.Count != 1 {
			return nil, errors.Errorf("expected minimum disk count of 1, got %d", cons.Minimum.Count)
		} else if cons.Preferred.Count != 1 {
			return nil, errors.Errorf("expected preferred disk count of 1, got %d", cons.Minimum.Count)
		}
	}
	ops := make([]txn.Op, len(constraints))
	for i := range constraints {
		name, err := newDiskName(st)
		if err != nil {
			return nil, errors.Annotate(err, "cannot generate disk name")
		}
		newDoc := blockDeviceDoc{
			DocID:   st.docID(name),
			Name:    name,
			EnvUUID: st.EnvironUUID(),
			Machine: machineId,
		}
		ops[i] = txn.Op{
			C:      blockDevicesC,
			Id:     newDoc.DocID,
			Assert: txn.DocMissing,
			Insert: &newDoc,
		}
		// TODO(axw) record storage constraints.
	}
	return ops, nil
}

func removeMachineBlockDevicesOps(st *State, machineId string) ([]txn.Op, error) {
	sel := bson.D{{"machine", machineId}}
	blockDevices, closer := st.getCollection(blockDevicesC)
	defer closer()

	iter := blockDevices.Find(sel).Select(bson.D{{"_id", 1}}).Iter()
	defer iter.Close()
	var ops []txn.Op
	var doc blockDeviceDoc
	for iter.Next(&doc) {
		ops = append(ops, txn.Op{
			C:      blockDevicesC,
			Id:     doc.DocID,
			Remove: true,
		})
	}
	return ops, errors.Trace(iter.Close())
}

// blockDevicesSame reports whether or not two BlockDevices identify the
// same block device.
//
// In descending order of preference, we use: serial number, filesystem
// UUID, device name.
func blockDevicesSame(a, b *BlockDeviceInfo) bool {
	if a.Serial != "" && b.Serial != "" {
		return a.Serial == b.Serial
	}
	if a.UUID != "" && b.UUID != "" {
		return a.UUID == b.UUID
	}
	return a.DeviceName != "" && a.DeviceName == b.DeviceName
}

func blockDevicesEqual(a, b []blockDevice) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
