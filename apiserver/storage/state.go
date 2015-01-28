// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage

import "github.com/juju/juju/state"

type storageAccess interface {
	StorageInstance(id string) (state.StorageInstance, error)
	AllStorageInstances() ([]state.StorageInstance, error)
}

type stateShim struct {
	*state.State
}
