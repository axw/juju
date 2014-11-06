// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"gopkg.in/juju/charm.v4"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"

	"github.com/juju/juju/storage"
)

// unitStorageDoc represents storage attached to a unit.
type unitStorageDoc struct {
	Id        bson.ObjectId     `bson:"_id"`
	UnitId    string            `bson:"unitid"`
	Name      string            `bson:"name"`
	Type      string            `bson:"type"`
	Path      string            `bson:"path"`
	Directive *storageDirective `bson:"directive,omitempty"`

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
	Type         string                 `bson:"type"`
	MountOptions []string               `bson:"mountoptions,omitempty"`
	Preferences  []filesystemPreference `bson:"preferences,omitempty"`
	State        int                    `bson:"state"`
}

type filesystemPreference struct {
	Type         string   `bson:"type"`
	MkfsOptions  []string `bson:"mkfsoptions"`
	MountOptions []string `bson:"mountoptions"`
}

type storageDirective struct {
	Source     string `bson:"source"`
	Count      int    `bson:"count"`
	Size       uint64 `bson:"size"`
	Persistent bool   `bson:"persistent"`
	Options    string `bson:"options"`
}

func newUnitStorageDoc(info storage.Storage) *unitStorageDoc {
	var fs *filesystem
	var directive *storageDirective
	if info.Filesystem != nil {
		var preferences []filesystemPreference
		for _, pref := range info.Filesystem.Preferences {
			preferences = append(preferences, filesystemPreference{
				Type:         pref.Type,
				MkfsOptions:  pref.MkfsOptions,
				MountOptions: pref.MountOptions,
			})
		}
		fs = &filesystem{
			Type:         info.Filesystem.Type,
			MountOptions: info.Filesystem.MountOptions,
			Preferences:  preferences,
			State:        int(info.Filesystem.State),
		}
	}
	if info.Directive != nil {
		directive = &storageDirective{
			Source:     info.Directive.Source,
			Count:      info.Directive.Count,
			Size:       info.Directive.Size,
			Persistent: info.Directive.Persistent,
			Options:    info.Directive.Options,
		}
	}
	return &unitStorageDoc{
		Name:        info.Name,
		Type:        string(info.Type),
		Path:        info.Path,
		Directive:   directive,
		Filesystem:  fs,
		BlockDevice: newBlockDeviceDoc(info.BlockDevice),
	}
}

func newBlockDeviceDoc(dev *storage.BlockDevice) *blockDevice {
	if dev == nil {
		return nil
	}
	return &blockDevice{
		DeviceName: dev.DeviceName,
		DiskUUID:   dev.DeviceUUID, // FIXME
		State:      int(dev.State),
	}
}

func newUnitStorage(doc *unitStorageDoc) storage.Storage {
	var fs *storage.Filesystem
	var dev *storage.BlockDevice
	var directive *storage.Directive
	if doc.Filesystem != nil {
		var preferences []storage.FilesystemPreference
		for _, pref := range doc.Filesystem.Preferences {
			preferences = append(preferences, storage.FilesystemPreference{
				Type:         pref.Type,
				MkfsOptions:  pref.MkfsOptions,
				MountOptions: pref.MountOptions,
			})
		}
		fs = &storage.Filesystem{
			Type:         doc.Filesystem.Type,
			MountOptions: doc.Filesystem.MountOptions,
			Preferences:  preferences,
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
	if doc.Directive != nil {
		directive = &storage.Directive{
			Name:       doc.Name,
			Source:     doc.Directive.Source,
			Count:      doc.Directive.Count,
			Size:       doc.Directive.Size,
			Persistent: doc.Directive.Persistent,
			Options:    doc.Directive.Options,
		}
	}
	return storage.Storage{
		Type:        charm.StorageType(doc.Type),
		Id:          doc.Id.Hex(),
		Name:        doc.Name,
		Path:        doc.Path,
		Directive:   directive,
		Filesystem:  fs,
		BlockDevice: dev,
	}
}

func setUnitStorageBlockDeviceOp(doc *unitStorageDoc, dev *blockDevice) txn.Op {
	return txn.Op{
		C:      unitstoragesC,
		Id:     doc.Id,
		Assert: bson.D{{"blockdevice", nil}},
		Update: bson.D{{"$set", bson.D{{"blockdevice", dev}}}},
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

func (st *State) SetStorageFilesystem(storageId, fsType string, fsMountOptions []string) error {
	op := txn.Op{
		C:  unitstoragesC,
		Id: bson.ObjectIdHex(storageId),
		Assert: bson.D{
			{"filesystem.state", int(storage.FilesystemStateCreating)},
		},
		Update: bson.D{{
			"$set", bson.D{
				{"filesystem.state", int(storage.FilesystemStateMounting)},
				{"filesystem.type", fsType},
				{"filesystem.mountoptions", fsMountOptions},
			},
		}, {
			"$unset", bson.D{{"filesystem.preferences", nil}},
		}},
	}
	return st.runTransaction([]txn.Op{op})
}

func (st *State) SetFilesystemMountPoint(storageId, mountPoint string) error {
	op := txn.Op{
		C:  unitstoragesC,
		Id: bson.ObjectIdHex(storageId),
		Assert: bson.D{
			{"filesystem.state", int(storage.FilesystemStateMounting)},
		},
		Update: bson.D{{
			"$set", bson.D{
				{"filesystem.state", int(storage.FilesystemStateMounted)},
				{"path", mountPoint},
			},
		}},
	}
	return st.runTransaction([]txn.Op{op})
}
