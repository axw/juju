// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskformatter

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"

	"github.com/juju/juju/api/diskformatter"
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/worker"
)

var logger = loggo.GetLogger("juju.worker.diskformatter")

// NewDiskFormatter creates a new worker that creates
// filesystems on block devices for a unit's charm storage.
func NewDiskFormatter(st *diskformatter.State, tag names.UnitTag) (worker.Worker, error) {
	df := &diskFormatter{
		st:  st,
		tag: tag,
	}
	return worker.NewNotifyWorker(df), nil
}

type diskFormatter struct {
	st  *diskformatter.State
	tag names.MachineTag
}

func (d *diskFormatter) SetUp() (watcher.NotifyWatcher, error) {
	w, err := m.st.WatchBlockDevices(d.tag)
	if err != nil {
		return nil, errors.LoggedErrorf(logger, "starting storage manager worker: %v", err)
	}
	logger.Infof("%q disk formatter worker started", d.tag)
	return w, nil
}

func (d *diskFormatter) TearDown() error {
	return nil
}

func (d *diskFormatter) Handle() error {
	devices, err := d.st.BlockDevices(d.tag)
	if err != nil {
		return errors.Annotate(err, "cannot get block devices")
	}
	logger.Infof("block devices: %+v", devices)
	return nil
}
