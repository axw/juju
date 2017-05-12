package storageprovisioner

import (
	"github.com/juju/errors"
	"github.com/juju/juju/state"
	"github.com/juju/juju/watcher"
)

func WatchMachineFilesystems(st *state.State, machineId string) (watcher.StringsWatcher, error) {
	w, err := st.Watch(state.WatchParams{IncludeOffers: false})
	if err != nil {
		return nil, errors.Trace(err)
	}
}
