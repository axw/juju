// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelupgrader

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/version"
	"gopkg.in/juju/names.v2"
	"gopkg.in/juju/worker.v1"

	"github.com/juju/juju/environs"
	jujuworker "github.com/juju/juju/worker"
	"github.com/juju/juju/worker/gate"
)

var logger = loggo.GetLogger("juju.worker.modelupgrader")

// Facade exposes capabilities required by the worker.
type Facade interface {
	ModelEnvironVersion(tag names.ModelTag) (version.Number, error)
	SetModelEnvironVersion(tag names.ModelTag, v version.Number) error
}

// Config holds the configuration and dependencies for a worker.
type Config struct {
	Facade        Facade
	Environ       environs.Environ
	GateUnlocker  gate.Unlocker
	ControllerTag names.ControllerTag
	ModelTag      names.ModelTag
}

// Validate returns an error if the config cannot be expected
// to drive a functional worker.
func (config Config) Validate() error {
	if config.Facade == nil {
		return errors.NotValidf("nil Facade")
	}
	if config.Environ == nil {
		return errors.NotValidf("nil Environ")
	}
	if config.GateUnlocker == nil {
		return errors.NotValidf("nil GateUnlocker")
	}
	if config.ControllerTag == (names.ControllerTag{}) {
		return errors.NotValidf("empty ControllerTag")
	}
	if config.ModelTag == (names.ModelTag{}) {
		return errors.NotValidf("empty ModelTag")
	}
	return nil
}

// NewWorker returns a worker that runs environ/provider schema upgrades
// when the model is first loaded by a controller of a new version.
func NewWorker(config Config) (worker.Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}
	return jujuworker.NewSimpleWorker(func(<-chan struct{}) error {
		currentVersion, err := config.Facade.ModelEnvironVersion(config.ModelTag)
		if err != nil {
			return errors.Trace(err)
		}
		setVersion := func(v version.Number) error {
			return config.Facade.SetModelEnvironVersion(config.ModelTag, v)
		}
		if err := runEnvironUpgradeSteps(
			config.Environ,
			config.ControllerTag,
			currentVersion,
			setVersion,
		); err != nil {
			return errors.Annotate(err, "upgrading environ")
		}
		config.GateUnlocker.Unlock()
		return nil
	}), nil
}

func runEnvironUpgradeSteps(
	env environs.Environ,
	controllerTag names.ControllerTag,
	currentVersion version.Number,
	setVersion func(version.Number) error,
) error {
	upgrader, ok := env.(environs.Upgrader)
	if !ok {
		logger.Debugf("%T does not support environs.Upgrader", env)
		return nil
	}
	args := environs.UpgradeStepParams{
		ControllerUUID: controllerTag.Id(),
	}
	for _, op := range upgrader.UpgradeOperations() {
		if op.TargetVersion.Compare(currentVersion) <= 0 {
			// The operation is for the same as or older
			// than the current environ version.
			continue
		}
		logger.Debugf(
			"running upgrade operations for version %s",
			op.TargetVersion,
		)
		for _, step := range op.Steps {
			logger.Debugf("running step %q", step.Description())
			if err := step.Run(args); err != nil {
				return errors.Trace(err)
			}
		}
		// Record the new version as we go, so we minimise the number
		// of operations we'll re-run in the case of failure.
		if err := setVersion(op.TargetVersion); err != nil {
			return errors.Trace(err)
		}
	}
	return nil
}
