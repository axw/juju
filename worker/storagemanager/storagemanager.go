// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storagemanager

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"
	"gopkg.in/juju/charm.v4"

	"github.com/juju/juju/api/storagemanager"
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/storage"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/uniter"
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
	// TODO(axw) should wait until unit is installed.
	w, err := m.st.WatchStorage(m.tag)
	if err != nil {
		return nil, errors.LoggedErrorf(logger, "starting storage manager worker: %v", err)
	}
	logger.Infof("%q storage manager worker started", m.tag)
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

	// Wait for block device to be attached.
	switch store.BlockDevice.State {
	default:
		return errors.Errorf("unhandled state %v", store.BlockDevice.State)
	case storage.BlockDeviceStateAttaching:
		logger.Debugf("waiting for block device attachment")
		return nil
	case storage.BlockDeviceStateAttached:
		break
	}

	// Create or mount the filesystem.
	switch store.Filesystem.State {
	default:
		return errors.Errorf("unhandled state %v", store.Filesystem.State)
	case storage.FilesystemStateCreating:
		return m.createFilesystem(store)
	case storage.FilesystemStateMounted:
		/*
			// TODO(axw) check if already mounted
			mounted := false
			if mounted {
				return nil
			}
			fallthrough
		*/
		return nil
	case storage.FilesystemStateMounting:
		return m.mountFilesystem(store)
	}
}

func (m *storageManager) createFilesystem(store storage.Storage) error {
	path, err := blockDevicePath(store.BlockDevice)
	if err != nil {
		return err
	}

	prefs := store.Filesystem.Preferences
	prefs = append(prefs, storage.FilesystemPreference{Type: "ext4"})
	for _, pref := range prefs {
		if err := m.maybeCreateFilesystem(path, pref); err == nil {
			logger.Infof("created %q filesystem on %q", pref.Type, path)
			if err := m.st.SetFilesystem(store.Id, pref.Type, pref.MountOptions); err != nil {
				return errors.Annotate(err, "cannot record filesystem")
			}
			return nil
		}
	}
	return errors.Errorf("failed to create filesystem on storage %q", store.Name)
}

func (m *storageManager) maybeCreateFilesystem(path string, fs storage.FilesystemPreference) error {
	args := []string{"-t", fs.Type}
	args = append(args, fs.MkfsOptions...)
	args = append(args, path)
	output, err := exec.Command("mkfs", args...).CombinedOutput()
	if err != nil {
		return errors.Annotatef(err, "mkfs failed (%q)", bytes.TrimSpace(output))
	}
	return nil
}

func blockDevicePath(dev *storage.BlockDevice) (string, error) {
	if dev.DeviceName != "" {
		path := dev.DeviceName
		if !strings.HasPrefix(path, "/dev") {
			path = filepath.Join("/dev", path)
		}
		return path, nil
	}
	if dev.DeviceUUID != "" {
		return filepath.Join("/dev/disk/by-uuid", dev.DeviceUUID), nil
	}
	return "", errors.New("block device path cannot be identified")
}

func (m *storageManager) mountFilesystem(store storage.Storage) error {
	devicePath, err := blockDevicePath(store.BlockDevice)
	if err != nil {
		return err
	}
	mountpoint := store.Path
	if mountpoint == "" {
		mountpoint = store.Id
	}
	if !filepath.IsAbs(mountpoint) {
		// Paths are relative to the unit agent's storage directory.
		storageDir := uniter.NewPaths(m.dataDir, m.tag).State.StorageDir
		mountpoint = filepath.Join(storageDir, mountpoint)
	}
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return err
	}

	args := []string{"-t", store.Filesystem.Type}
	if len(store.Filesystem.MountOptions) > 0 {
		args = append(args, "-o")
		args = append(args, strings.Join(store.Filesystem.MountOptions, ","))
	}
	args = append(args, devicePath, mountpoint)
	output, err := exec.Command("mount", args...).CombinedOutput()
	if err != nil {
		return errors.Annotatef(err, "mount failed (%q)", bytes.TrimSpace(output))
	}
	logger.Infof("mounted %q filesystem on %q at %q", store.Filesystem.Type, devicePath, mountpoint)
	return m.st.SetMountPoint(store.Id, mountpoint)
}
