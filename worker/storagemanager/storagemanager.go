// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storagemanager

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"
	"gopkg.in/juju/charm.v4"

	"github.com/juju/juju/api/storagemanager"
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/storage"
	"github.com/juju/juju/worker"
)

var logger = loggo.GetLogger("juju.worker.storagemanager")

// NewStorageManager creates a new worker that manages
// a unit's charm storage on the machine.
func NewStorageManager(st *storagemanager.State, tag names.UnitTag, dataDir string) (worker.Worker, error) {
	sm := &storageManager{
		st:      st,
		tag:     tag,
		dataDir: dataDir,
	}
	return worker.NewNotifyWorker(sm), nil
}

type storageManager struct {
	st      *storagemanager.State
	tag     names.UnitTag
	dataDir string
}

func (m *storageManager) SetUp() (watcher.NotifyWatcher, error) {
	w, err := m.st.WatchStorage(m.tag)
	if err != nil {
		return nil, errors.LoggedErrorf(logger, "starting storage manager worker: %v", err)
	}
	logger.Infof("%q disk formatter worker started", m.tag)
	return w, nil
}

func (m *storageManager) TearDown() error {
	return nil
}

func (m *storageManager) Handle() error {
	stores, err := m.st.Storage(m.tag)
	if err != nil {
		return errors.Annotate(err, "cannot get block devices")
	}
	logger.Infof("storage: %+v", stores)
	for _, store := range stores {
		switch store.Type {
		default:
			logger.Errorf("unknown storage type: %v", store.Type)
		case charm.StorageBlock:
			// TODO(axw) record device attachment; partition
			// and record UUID in state if not already done.
			if err := m.handleBlockDevice(store); err != nil {
				return err
			}
		case charm.StorageFilesystem:
			if err := m.handleFilesystem(store); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *storageManager) handleBlockDevice(store storage.Storage) error {
	switch store.BlockDevice.State {
	default:
		return errors.Errorf("unhandled state %v", store.Filesystem.State)
	case storage.BlockDeviceStateAttaching:
		logger.Debugf("attaching block device... %v", store)
		// TODO(axw) report back that device is attached now.
		return nil
	case storage.BlockDeviceStateAttached:
		// Nothing to do
		return nil
	}
}

func (m *storageManager) handleFilesystem(store storage.Storage) error {
	if store.BlockDevice == nil {
		// TODO(axw)
		return errors.NotSupportedf("unmanaged filesystems")
	}

	switch store.Filesystem.State {
	default:
		return errors.Errorf("unhandled state %v", store.Filesystem.State)
	case storage.FilesystemStateMounted:
		// TODO(axw) check if already mounted
		mounted := false
		if mounted {
			return nil
		}
		fallthrough
	case storage.FilesystemStateMounting:
		switch store.BlockDevice.State {
		default:
			return errors.Errorf("unhandled state %v", store.BlockDevice.State)
		case storage.BlockDeviceStateAttaching:
			logger.Debugf("waiting for block device attachment")
			return nil
		case storage.BlockDeviceStateAttached:
			break
		}
		logger.Debugf("mounting filesystem... %v", store)
		// TODO(axw) report back that filesystem is mounted now.
		return nil
	}
}
