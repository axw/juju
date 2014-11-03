// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storagemanager

import (
	"github.com/juju/loggo"
	"github.com/juju/names"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/watcher"
	"github.com/juju/juju/storage"
)

func init() {
	common.RegisterStandardFacade("StorageManager", 0, NewStorageManagerAPI)
}

var logger = loggo.GetLogger("juju.apiserver.storagemanager")

// StorageManagerAPI provides access to the StorageManager API facade.
type StorageManagerAPI struct {
	st          *state.State
	resources   *common.Resources
	authorizer  common.Authorizer
	getAuthFunc common.GetAuthFunc
}

// NewStorageManagerAPI creates a new client-side StorageManager API facade.
func NewStorageManagerAPI(
	st *state.State,
	resources *common.Resources,
	authorizer common.Authorizer,
) (*StorageManagerAPI, error) {

	if !authorizer.AuthUnitAgent() {
		return nil, common.ErrPerm
	}

	getAuthFunc := func() (common.AuthFunc, error) {
		authEntityTag := authorizer.GetAuthTag()
		return func(tag names.Tag) bool {
			if tag == authEntityTag {
				// A unit agent can always access its own storage.
				return true
			}
			return false
		}, nil
	}

	return &StorageManagerAPI{
		st:          st,
		resources:   resources,
		authorizer:  authorizer,
		getAuthFunc: getAuthFunc,
	}, nil
}

func (d *StorageManagerAPI) oneUnitStorage(id string) ([]storage.Storage, error) {
	unit, err := d.st.Unit(id)
	if err != nil {
		return nil, err
	}
	return unit.Storage()
}

// Storage returns the list of storage instances for each of a given set of unit.
func (d *StorageManagerAPI) Storage(args params.Entities) (params.StorageResults, error) {
	result := params.StorageResults{
		Results: make([]params.StorageResult, len(args.Entities)),
	}
	canAccess, err := d.getAuthFunc()
	if err != nil {
		return result, err
	}
	for i, entity := range args.Entities {
		tag, err := names.ParseUnitTag(entity.Tag)
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}

		if !canAccess(tag) {
			err = common.ErrPerm
		} else {
			id := tag.Id()
			result.Results[i].Storage, err = d.oneUnitStorage(id)
		}
		result.Results[i].Error = common.ServerError(err)
	}
	return result, nil
}

func (d *StorageManagerAPI) watchOneUnitStorage(id string) (string, error) {
	unit, err := d.st.Unit(id)
	if err != nil {
		return "", err
	}
	watch := unit.WatchStorage()
	// Consume the initial event.
	if _, ok := <-watch.Changes(); ok {
		return d.resources.Register(watch), nil
	}
	return "", watcher.EnsureErr(watch)
}

// WatchStorage returns a NotifyWatcher for observing changes
// to each unit's storage instances.
func (d *StorageManagerAPI) WatchStorage(args params.Entities) (params.NotifyWatchResults, error) {
	result := params.NotifyWatchResults{
		Results: make([]params.NotifyWatchResult, len(args.Entities)),
	}
	canAccess, err := d.getAuthFunc()
	if err != nil {
		return result, err
	}
	for i, entity := range args.Entities {
		tag, err := names.ParseUnitTag(entity.Tag)
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		if !canAccess(tag) {
			err = common.ErrPerm
		} else {
			logger.Infof("watching block devices for %q", tag)
			id := tag.Id()
			result.Results[i].NotifyWatcherId, err = d.watchOneUnitStorage(id)
		}
		result.Results[i].Error = common.ServerError(err)
	}
	return result, nil
}
