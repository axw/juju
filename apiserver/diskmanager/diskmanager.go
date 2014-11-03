// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package diskmanager

import (
	"github.com/juju/loggo"
	"github.com/juju/names"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state"
	"github.com/juju/juju/storage"
)

func init() {
	common.RegisterStandardFacade("DiskManager", 0, NewDiskManagerAPI)
}

var logger = loggo.GetLogger("juju.apiserver.diskmanager")

// DiskManagerAPI provides access to the DiskManager API facade.
type DiskManagerAPI struct {
	st          *state.State
	resources   *common.Resources
	authorizer  common.Authorizer
	getAuthFunc common.GetAuthFunc
}

// NewDiskManagerAPI creates a new client-side DiskManager API facade.
func NewDiskManagerAPI(
	st *state.State,
	resources *common.Resources,
	authorizer common.Authorizer,
) (*DiskManagerAPI, error) {

	if !authorizer.AuthMachineAgent() {
		return nil, common.ErrPerm
	}

	getAuthFunc := func() (common.AuthFunc, error) {
		authEntityTag := authorizer.GetAuthTag()
		return func(tag names.Tag) bool {
			if tag == authEntityTag {
				// A machine agent can always access its own machine.
				return true
			}
			return false
		}, nil
	}

	return &DiskManagerAPI{
		st:          st,
		resources:   resources,
		authorizer:  authorizer,
		getAuthFunc: getAuthFunc,
	}, nil
}

func (d *DiskManagerAPI) oneMachineStorage(id string) ([]storage.Storage, error) {
	/*
		machine, err := d.st.Machine(id)
		if err != nil {
			return nil, err
		}
		return machine.Storage()
	*/
	panic("unimplemented")
}

// Storage returns the list of block devices for each of a given set of machines.
func (d *DiskManagerAPI) Storage(args params.Entities) (params.StorageResults, error) {
	result := params.StorageResults{
		Results: make([]params.StorageResult, len(args.Entities)),
	}
	canAccess, err := d.getAuthFunc()
	if err != nil {
		return result, err
	}
	for i, entity := range args.Entities {
		tag, err := names.ParseMachineTag(entity.Tag)
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}

		if !canAccess(tag) {
			err = common.ErrPerm
		} else {
			id := tag.Id()
			result.Results[i].Result, err = d.oneMachineStorage(id)
		}
		result.Results[i].Error = common.ServerError(err)
	}
	return result, nil
}

func (d *DiskManagerAPI) watchOneMachineStorage(id string) (string, error) {
	/*
		machine, err := d.st.Machine(id)
		if err != nil {
			return "", err
		}
		watch := machine.WatchStorage()
		// Consume the initial event.
		if _, ok := <-watch.Changes(); ok {
			return d.resources.Register(watch), nil
		}
		return "", watcher.EnsureErr(watch)
	*/
	panic("unimeplemented")
}

// WatchStorage returns a NotifyWatcher for observing changes
// to each machine's block devices.
func (d *DiskManagerAPI) WatchStorage(args params.Entities) (params.NotifyWatchResults, error) {
	result := params.NotifyWatchResults{
		Results: make([]params.NotifyWatchResult, len(args.Entities)),
	}
	canAccess, err := d.getAuthFunc()
	if err != nil {
		return result, err
	}
	for i, entity := range args.Entities {
		tag, err := names.ParseMachineTag(entity.Tag)
		if err != nil {
			result.Results[i].Error = common.ServerError(common.ErrPerm)
			continue
		}
		if !canAccess(tag) {
			err = common.ErrPerm
		} else {
			logger.Infof("watching block devices for %q", tag)
			id := tag.Id()
			result.Results[i].NotifyWatcherId, err = d.watchOneMachineStorage(id)
		}
		result.Results[i].Error = common.ServerError(err)
	}
	return result, nil
}
