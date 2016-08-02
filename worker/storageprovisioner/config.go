// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storageprovisioner

import (
	"github.com/juju/errors"
	"github.com/juju/utils/clock"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/environs/config"
)

// Config holds configuration and dependencies for a storageprovisioner worker.
type Config struct {
	Scope       names.Tag
	StorageDir  string
	Volumes     VolumeAccessor
	Filesystems FilesystemAccessor
	Life        LifecycleManager
	Machines    MachineAccessor
	Status      StatusSetter
	Clock       clock.Clock
	ModelConfig *config.Config
}

// Validate returns an error if the config cannot be relied upon to start a worker.
func (config Config) Validate() error {
	switch config.Scope.(type) {
	case nil:
		return errors.NotValidf("nil Scope")
	case names.ModelTag:
		if config.StorageDir != "" {
			return errors.NotValidf("environ Scope with non-empty StorageDir")
		}
		if config.ModelConfig == nil {
			return errors.NotValidf("environ Scope with nil ModelConfig")
		}
	case names.MachineTag:
		if config.StorageDir == "" {
			return errors.NotValidf("machine Scope with empty StorageDir")
		}
		if config.ModelConfig != nil {
			return errors.NotValidf("machine Scope with non-nil ModelConfig")
		}
	default:
		return errors.NotValidf("%T Scope", config.Scope)
	}
	if config.Volumes == nil {
		return errors.NotValidf("nil Volumes")
	}
	if config.Filesystems == nil {
		return errors.NotValidf("nil Filesystems")
	}
	if config.Life == nil {
		return errors.NotValidf("nil Life")
	}
	if config.Machines == nil {
		return errors.NotValidf("nil Machines")
	}
	if config.Status == nil {
		return errors.NotValidf("nil Status")
	}
	if config.Clock == nil {
		return errors.NotValidf("nil Clock")
	}
	return nil
}
