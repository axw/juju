// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package caasunitprovisioner

import (
	"github.com/juju/errors"
	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/facade"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state/watcher"
	"github.com/juju/names"
)

type Facade struct {
	auth      facade.Authorizer
	resources facade.Resources

	state CAASUnitProvisionerState
}

// NewStateCAASUnitProvisionerFacade provides the signature required for facade registration.
func NewStateCAASUnitProvisionerFacade(ctx facade.Context) (*Facade, error) {
	authorizer := ctx.Auth()
	resources := ctx.Resources()
	return NewCAASUnitProvisionerFacade(
		resources,
		authorizer,
		stateShim{ctx.State()},
	)
}

// NewCAASUnitProvisionerFacade returns a new CAAS unit provisioner Facade facade.
func NewCAASUnitProvisionerFacade(
	resources facade.Resources,
	authorizer facade.Authorizer,
	st CAASUnitProvisionerState,
) (*Facade, error) {
	if !authorizer.AuthController() {
		return nil, common.ErrPerm
	}
	return &Facade{
		auth:      authorizer,
		resources: resources,
		state:     st,
	}, nil
}

// WatchApplications starts a StringsWatcher to watch CAAS applications
// deployed to this model.
func (f *Facade) WatchApplications() (params.StringsWatchResult, error) {
	watch := f.state.WatchApplications()
	if changes, ok := <-watch.Changes(); ok {
		return params.StringsWatchResult{
			StringsWatcherId: f.resources.Register(watch),
			Changes:          changes,
		}, nil
	}
	return params.StringsWatchResult{}, watcher.EnsureErr(watch)
}

// WatchContainerSpec starts a NotifyWatcher to watch changes to the
// container spec for specified units in this model.
func (f *Facade) WatchContainerSpec(args params.Entities) (params.NotifyWatchResults, error) {
	model, err := f.state.Model()
	if err != nil {
		return params.NotifyWatchResults{}, errors.Trace(err)
	}
	results := params.NotifyWatchResults{
		Results: make([]params.NotifyWatchResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		id, err := f.watchContainerSpec(model, arg.Tag)
		if err != nil {
			results.Results[i].Error = common.ServerError(err)
			continue
		}
		results.Results[i].NotifyWatcherId = id
	}
	return results, nil
}

func (f *Facade) watchContainerSpec(model Model, tagString string) (string, error) {
	tag, err := names.ParseUnitTag(tagString)
	if err != nil {
		return "", errors.Trace(err)
	}
	w, err := model.WatchContainerSpec(tag)
	if err != nil {
		return "", errors.Trace(err)
	}
	if _, ok := <-w.Changes(); ok {
		return f.resources.Register(w), nil
	}
	return "", watcher.EnsureErr(w)
}
