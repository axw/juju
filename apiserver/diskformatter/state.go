// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskformatter

import (
	"github.com/juju/juju/api/watcher"
	"github.com/juju/juju/state"
)

type stateInterface interface {
	WatchAttachedBlockDevices(unit string) (watcher.StringsWatcher, error)
	BlockDevice(name string) (state.BlockDevice, error)
}

type stateShim struct {
	*state.State
}

func (s stateShim) WatchAttachedBlockDevices(unit string) (watcher.StringsWatcher, error) {
	u, err := s.State.Unit(unit)
	if err != nil {
		return nil, err
	}
	return u.WatchAttachedBlockDevices()
}
