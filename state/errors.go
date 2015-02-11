// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state

import (
	"github.com/juju/errors"
	"github.com/juju/names"
)

// unitNotAssigned represents an error when a unit is not yet assigned to
// a machine.
type unitNotAssigned struct {
	error
}

// UnitNotAssigned returns an error which satisfies IsUnitNotAssigned().
func UnitNotAssigned(unit names.UnitTag) error {
	err := errors.Errorf("%s not assigned to a machine", names.ReadableString(unit))
	return &unitNotAssigned{err}
}

// IsUnitNotAssigned reports whether err was created with UnitNotAssigned.
func IsUnitNotAssigned(err error) bool {
	_, ok := errors.Cause(err).(*unitNotAssigned)
	return ok
}

// volumeNotAssigned represents an error when a volume is not yet assigned to
// a storage instance.
type volumeNotAssigned struct {
	error
}

// VolumeNotAssigned returns an error which satisfies IsVolumeNotAssigned().
func VolumeNotAssigned(volume names.DiskTag) error {
	err := errors.Errorf("%s not assigned to a storage instance", names.ReadableString(volume))
	return &volumeNotAssigned{err}
}

// IsVolumeNotAssigned reports whether err was created with VolumeNotAssigned.
func IsVolumeNotAssigned(err error) bool {
	_, ok := errors.Cause(err).(*volumeNotAssigned)
	return ok
}
