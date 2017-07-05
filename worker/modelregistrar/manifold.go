// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelregistrar

import (
	"github.com/juju/errors"
	"gopkg.in/juju/names.v2"
	"gopkg.in/juju/worker.v1"

	jujuworker "github.com/juju/juju/worker"
	"github.com/juju/juju/worker/dependency"
)

// ManifoldConfig describes how to configure and construct a Worker,
// and what registered resources it may depend upon.
type ManifoldConfig struct {
	ModelTag names.ModelTag
	Registry ModelRegistry
}

// ModelRegistry provides an interface for registering (unregistering) models
// as they become available (unavailable) for use.
type ModelRegistry interface {
	RegisterModel(names.ModelTag)
	UnregisterModel(names.ModelTag)
}

func (config ManifoldConfig) start(context dependency.Context) (worker.Worker, error) {
	if config.ModelTag == (names.ModelTag{}) {
		return nil, errors.NotValidf("empty ModelTag")
	}
	if config.Registry == nil {
		return nil, errors.NotValidf("nil Registry")
	}
	return jujuworker.NewSimpleWorker(func(abort <-chan struct{}) error {
		defer config.Registry.UnregisterModel(config.ModelTag)
		config.Registry.RegisterModel(config.ModelTag)
		<-abort
		return nil
	}), nil
}

// Manifold returns a dependency.Manifold that will run a Worker as
// configured.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Start: config.start,
	}
}
