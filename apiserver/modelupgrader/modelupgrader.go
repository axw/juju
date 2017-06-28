// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelupgrader

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/version"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/facade"
	"github.com/juju/juju/apiserver/params"
)

var logger = loggo.GetLogger("juju.apiserver.modelupgrader")

type Facade struct {
	backend Backend
}

// NewStateFacade provides the signature required for facade registration.
func NewStateFacade(ctx facade.Context) (*Facade, error) {
	backend := NewStateBackend(ctx.State())
	return NewFacade(backend, ctx.Auth())
}

// NewFacade returns a new Facade using the given Backend and Authorizer.
func NewFacade(backend Backend, auth facade.Authorizer) (*Facade, error) {
	if !auth.AuthController() {
		return nil, common.ErrPerm
	}
	return &Facade{backend: backend}, nil
}

// ModelEnvironVersion returns the current version of the environ corresponding
// to each specified model.
func (f *Facade) ModelEnvironVersion(args params.Entities) (params.VersionResults, error) {
	result := params.VersionResults{
		Results: make([]params.VersionResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		v, err := f.modelEnvironVersion(arg)
		if err != nil {
			result.Results[i].Error = common.ServerError(err)
			continue
		}
		result.Results[i].Version = v
	}
	return result, nil
}

func (f *Facade) modelEnvironVersion(arg params.Entity) (*version.Number, error) {
	tag, err := names.ParseModelTag(arg.Tag)
	if err != nil {
		return nil, errors.Trace(err)
	}
	model, err := f.backend.GetModel(tag)
	if err != nil {
		return nil, errors.Trace(err)
	}
	v := model.EnvironVersion()
	return &v, nil
}

// SetModelEnvironVersion sets the current version of the environ corresponding
// to each specified model.
func (f *Facade) SetModelEnvironVersion(args params.EntityVersionNumbers) (params.ErrorResults, error) {
	result := params.ErrorResults{
		Results: make([]params.ErrorResult, len(args.Entities)),
	}
	for i, arg := range args.Entities {
		err := f.setModelEnvironVersion(arg)
		if err != nil {
			result.Results[i].Error = common.ServerError(err)
		}
	}
	return result, nil
}

func (f *Facade) setModelEnvironVersion(arg params.EntityVersionNumber) error {
	tag, err := names.ParseModelTag(arg.Tag)
	if err != nil {
		return errors.Trace(err)
	}
	v, err := version.Parse(arg.Version)
	if err != nil {
		return errors.Trace(err)
	}
	model, err := f.backend.GetModel(tag)
	if err != nil {
		return errors.Trace(err)
	}
	return errors.Trace(model.SetEnvironVersion(v))
}
