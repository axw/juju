// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskmanager

import (
	"time"

	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"

	"github.com/juju/juju/api/diskmanager"
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/version"
	"github.com/juju/juju/worker"
)

var logger = loggo.GetLogger("juju.worker.diskmanager")

const (
	// listDevicesPeriod is the period for repeating the block device
	// management listing operation.
	listDevicesPeriod = 30 * time.Second
)

// NewDiskManger creates a new worker that manages the disks on the machine.
func NewDiskManager(st *diskmanager.State, tag names.MachineTag) (worker.Worker, error) {
	if version.Current.OS == version.Windows {
		return nil, errors.New("disk management is not yet available for Windows")
	}
	// TODO(axw) also periodically look for block devices on the system and
	// report them back to the API server. This should be done in a single
	// goroutine with a select, to avoid races.
	dm := &diskManager{
		st:  st,
		tag: tag,
	}
	return worker.NewNotifyWorker(dm), nil
}

type diskManager struct {
	st  *diskmanager.State
	tag names.MachineTag
}

func (m *diskManager) SetUp() (watcher.NotifyWatcher, error) {
	w, err := m.st.WatchBlockDevices(m.tag)
	if err != nil {
		return nil, errors.LoggedErrorf(logger, "starting disk manager worker: %v", err)
	}
	logger.Infof("%q disk manager worker started", m.tag)
	return w, nil
}

func (m *diskManager) TearDown() error {
	return nil
}

func (m *diskManager) Handle() error {
	devices, err := m.st.BlockDevices(m.tag)
	if err != nil {
		return errors.Annotate(err, "cannot get block devices")
	}
	logger.Infof("block devices: %+v", devices)
	return nil
}
