// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storageworker

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"
	"launchpad.net/tomb"

	apiwatcher "github.com/juju/juju/api/watcher"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/state/watcher"
	"github.com/juju/juju/worker"
)

var logger = loggo.GetLogger("juju.worker.storageworker")

type VolumeAccessor interface {
	WatchVolumes() (apiwatcher.StringsWatcher, error)
	Volumes([]names.DiskTag) ([]params.VolumeResult, error)
	VolumeParams([]names.DiskTag) ([]params.VolumeParamsResult, error)
	SetVolumeInfo([]params.Volume) (params.ErrorResults, error)
}

type LifecycleManager interface {
	Life([]names.Tag) ([]params.LifeResult, error)
	EnsureDead([]names.Tag) ([]params.ErrorResult, error)
	Remove([]names.Tag) ([]params.ErrorResult, error)
}

// NewStorageWorker returns a Worker which manages
// provisioning (deprovisioning), and attachment (detachment)
// of first-class volumes and filesystems.
func NewStorageWorker(v VolumeAccessor, l LifecycleManager) worker.Worker {
	w := &storageWorker{
		volumes: v,
		life:    l,
	}
	go func() {
		defer w.tomb.Done()
		w.tomb.Kill(w.loop())
	}()
	return w
}

type storageWorker struct {
	tomb    tomb.Tomb
	volumes VolumeAccessor
	life    LifecycleManager
}

// Kill implements Worker.Kill().
func (w *storageWorker) Kill() {
	w.tomb.Kill(nil)
}

// Wait implements Worker.Wait().
func (w *storageWorker) Wait() error {
	return w.tomb.Wait()
}

func (w *storageWorker) loop() error {
	// TODO(axw) wait for and watch environ config.
	var environConfig *config.Config
	/*
		var environConfigChanges <-chan struct{}
		environWatcher, err := p.st.WatchForEnvironConfigChanges()
		if err != nil {
			return err
		}
		environConfigChanges = environWatcher.Changes()
		defer watcher.Stop(environWatcher, &p.tomb)
		p.environ, err = worker.WaitForEnviron(environWatcher, p.st, p.tomb.Dying())
		if err != nil {
			return err
		}
	*/

	volumesWatcher, err := w.volumes.WatchVolumes()
	if err != nil {
		return errors.Annotate(err, "watching volumes")
	}
	defer watcher.Stop(volumesWatcher, &w.tomb)
	volumesChanges := volumesWatcher.Changes()

	ctx := context{
		environConfig: environConfig,
		volumes:       w.volumes,
		life:          w.life,
	}

	for {
		select {
		case <-w.tomb.Dying():
			return tomb.ErrDying
		case changes, ok := <-volumesChanges:
			if !ok {
				return watcher.EnsureErr(volumesWatcher)
			}
			if err := volumesChanged(&ctx, changes); err != nil {
				return errors.Trace(err)
			}
		}
	}
}

type context struct {
	environConfig *config.Config
	volumes       VolumeAccessor
	life          LifecycleManager
}
