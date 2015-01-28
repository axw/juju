// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// Package diskformatter defines a worker that watches for block devices
// assigned to storage instances owned by the unit that runs this worker,
// and creates filesystems on them as necessary. Each unit agent runs this
// worker.
package diskformatter

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"

	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/storage"
	"github.com/juju/juju/worker"
)

var logger = loggo.GetLogger("juju.worker.diskformatter")

// defaultFilesystemType is the default filesystem type to
// create on a managed block device for a "filesystem" type
// storage instance.
const defaultFilesystemType = "ext4"

// BlockDeviceAccessor is an interface used to watch and retrieve details of
// the block devices assigned to storage instances owned by the unit.
type BlockDeviceAccessor interface {
	WatchBlockDevices() (watcher.StringsWatcher, error)
	BlockDevices([]names.DiskTag) (params.BlockDeviceResults, error)
	BlockDeviceStorageInstances([]names.DiskTag) (params.StorageInstanceResults, error)
	SetMountPoints(map[names.StorageTag]string) (params.ErrorResults, error)
}

// NewWorker returns a new worker that creates filesystems on block devices
// assigned to this unit's storage instances.
func NewWorker(
	storageDir string,
	accessor BlockDeviceAccessor,
) worker.Worker {
	return worker.NewStringsWorker(newDiskFormatter(storageDir, accessor))
}

func newDiskFormatter(storageDir string, accessor BlockDeviceAccessor) worker.StringsWatchHandler {
	return &diskFormatter{storageDir, accessor}
}

type diskFormatter struct {
	storageDir string
	accessor   BlockDeviceAccessor
}

func (f *diskFormatter) SetUp() (watcher.StringsWatcher, error) {
	return f.accessor.WatchBlockDevices()
}

func (f *diskFormatter) TearDown() error {
	return nil
}

func (f *diskFormatter) Handle(diskNames []string) error {
	tags := make([]names.DiskTag, len(diskNames))
	for i, name := range diskNames {
		tags[i] = names.NewDiskTag(name)
	}

	// attachedBlockDevices returns the block devices that are
	// assigned to the caller, and are known to be attached and
	// visible to their associated machines.
	blockDevices, err := f.attachedBlockDevices(tags)
	if err != nil {
		return err
	}

	blockDeviceTags := make([]names.DiskTag, len(blockDevices))
	for i, dev := range blockDevices {
		blockDeviceTags[i] = names.NewDiskTag(dev.Name)
	}

	// Map block devices to the storage instances they are assigned to.
	results, err := f.accessor.BlockDeviceStorageInstances(blockDeviceTags)
	if err != nil {
		return errors.Annotate(err, "cannot get assigned storage instances")
	}

	mountPoints := make(map[names.StorageTag]string)
	for i, result := range results.Results {
		if result.Error != nil {
			logger.Errorf(
				"could not determine storage instance for block device %q: %v",
				blockDevices[i].Name, result.Error,
			)
			continue
		}
		storageInstance := result.Result
		if storageInstance.Kind != storage.StorageKindFilesystem {
			logger.Debugf("storage instance %q does not need a filesystem", storageInstance.Id)
			continue
		}
		if storageInstance.Location != "" {
			// TODO(axw) we need to ensure the mount point
			// is still there, and remount if needed.
			logger.Debugf("storage instance %q is already mounted at %q", storageInstance.Location)
			continue
		}
		devicePath, err := storage.BlockDevicePath(blockDevices[i])
		if err != nil {
			logger.Errorf("cannot get path for block device %q: %v", blockDevices[i].Name, err)
			continue
		}
		if blockDevices[i].FilesystemType == "" {
			if err := createFilesystem(devicePath); err != nil {
				logger.Errorf("failed to create filesystem on block device %q: %v", blockDevices[i].Name, err)
				continue
			}
		}
		// Mount the block device and set the location.
		// TODO(axw) use the location in the charm if specified.
		location := filepath.Join(f.storageDir, filepath.FromSlash(storageInstance.Id))
		if err := mount(devicePath, location); err != nil {
			logger.Errorf("failed to mount block device %q: %v", blockDevices[i].Name, err)
			continue
		}
		mountPoints[names.NewStorageTag(storageInstance.Id)] = location
	}

	if len(mountPoints) > 0 {
		results, err := f.accessor.SetMountPoints(mountPoints)
		if err != nil {
			return err
		}
		return results.Combine()
	}
	return nil
}

func (f *diskFormatter) attachedBlockDevices(tags []names.DiskTag) ([]storage.BlockDevice, error) {
	results, err := f.accessor.BlockDevices(tags)
	if err != nil {
		return nil, errors.Annotate(err, "cannot get block devices")
	}
	blockDevices := make([]storage.BlockDevice, 0, len(tags))
	for i := range results.Results {
		result := results.Results[i]
		if result.Error != nil {
			if !errors.IsNotFound(result.Error) {
				logger.Errorf("could not get details for block device %q", tags[i])
			}
			continue
		}
		blockDevices = append(blockDevices, result.Result)
	}
	return blockDevices, nil
}

func createFilesystem(devicePath string) error {
	logger.Debugf("attempting to create filesystem on %q", devicePath)
	if err := maybeCreateFilesystem(devicePath); err != nil {
		return err
	}
	logger.Infof("created filesystem on %q", devicePath)
	return nil
}

func maybeCreateFilesystem(path string) error {
	mkfscmd := "mkfs." + defaultFilesystemType
	output, err := exec.Command(mkfscmd, path).CombinedOutput()
	if err != nil {
		return errors.Annotatef(err, "%s failed (%q)", mkfscmd, bytes.TrimSpace(output))
	}
	return nil
}

func mount(device, path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return errors.Annotate(err, "creating mount point")
	}
	output, err := exec.Command("mount", device, path).CombinedOutput()
	if err != nil {
		return errors.Annotatef(err, "mount failed (%q)", bytes.TrimSpace(output))
	}
	return nil
}
