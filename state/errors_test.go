// Copyright 2015 Canonical Ltd.
// Licensed under the LGPLv3, see LICENCE file for details.

package state_test

import (
	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/state"
)

type errorsSuite struct{}

var _ = gc.Suite(&errorsSuite{})

func (*errorsSuite) TestUnitNotAssigned(c *gc.C) {
	err := state.UnitNotAssigned(names.NewUnitTag("mysql/0"))
	c.Assert(err, gc.ErrorMatches, "unit mysql/0 not assigned to a machine")
	c.Assert(err, jc.Satisfies, state.IsUnitNotAssigned)
}

func (*errorsSuite) TestVolumeNotAssigned(c *gc.C) {
	err := state.VolumeNotAssigned(names.NewDiskTag("0"))
	c.Assert(err, gc.ErrorMatches, "disk 0 not assigned to a storage instance")
	c.Assert(err, jc.Satisfies, state.IsVolumeNotAssigned)
}
