// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state_test

import (
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	"gopkg.in/mgo.v2/bson"
	"gopkg.in/mgo.v2/txn"

	"github.com/juju/juju/state"
	"github.com/juju/juju/state/testing"
)

type BlockDevicesSuite struct {
	ConnSuite
	machine *state.Machine
	charm   *state.Charm
	service *state.Service
	unit    *state.Unit
}

var _ = gc.Suite(&BlockDevicesSuite{})

func (s *BlockDevicesSuite) SetUpTest(c *gc.C) {
	s.ConnSuite.SetUpTest(c)
	var err error
	s.machine, err = s.State.AddMachine("quantal", state.JobHostUnits)
	c.Assert(err, jc.ErrorIsNil)

	s.charm = s.AddTestingCharm(c, "wordpress")
	s.service = s.AddTestingService(c, "wordpress", s.charm)
	c.Assert(err, jc.ErrorIsNil)
	s.unit, err = s.service.AddUnit()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(s.unit.Series(), gc.Equals, "quantal")
}

func (s *BlockDevicesSuite) assertBlockDevices(c *gc.C, expected map[string]state.BlockDeviceInfo) {
	devices, err := s.machine.BlockDevices()
	c.Assert(err, gc.IsNil)
	info := make(map[string]state.BlockDeviceInfo)
	for _, dev := range devices {
		info[dev.Name()] = dev.Info()
	}
	c.Assert(info, gc.DeepEquals, expected)
}

func (s *BlockDevicesSuite) TestSetMachineBlockDevices(c *gc.C) {
	sda := state.BlockDeviceInfo{DeviceName: "sda"}
	err := s.machine.SetMachineBlockDevices(sda)
	c.Assert(err, gc.IsNil)
	s.assertBlockDevices(c, map[string]state.BlockDeviceInfo{"0": sda})
}

func (s *BlockDevicesSuite) TestSetMachineBlockDevicesReplaces(c *gc.C) {
	sda := state.BlockDeviceInfo{DeviceName: "sda"}
	err := s.machine.SetMachineBlockDevices(sda)
	c.Assert(err, gc.IsNil)

	sdb := state.BlockDeviceInfo{DeviceName: "sdb"}
	err = s.machine.SetMachineBlockDevices(sdb)
	c.Assert(err, gc.IsNil)
	s.assertBlockDevices(c, map[string]state.BlockDeviceInfo{"1": sdb})
}

func (s *BlockDevicesSuite) TestSetMachineBlockDevicesUpdates(c *gc.C) {
	sda := state.BlockDeviceInfo{DeviceName: "sda"}
	sdb := state.BlockDeviceInfo{DeviceName: "sdb"}
	err := s.machine.SetMachineBlockDevices(sda, sdb)
	c.Assert(err, gc.IsNil)
	s.assertBlockDevices(c, map[string]state.BlockDeviceInfo{"0": sda, "1": sdb})

	sdb.Label = "root"
	err = s.machine.SetMachineBlockDevices(sdb)
	c.Assert(err, gc.IsNil)
	s.assertBlockDevices(c, map[string]state.BlockDeviceInfo{"1": sdb})

	// If a device is attached, unattached, then attached again,
	// then it gets a new name.
	sdb.Label = "" // Label should be reset.
	err = s.machine.SetMachineBlockDevices(sda, sdb)
	c.Assert(err, gc.IsNil)
	s.assertBlockDevices(c, map[string]state.BlockDeviceInfo{
		"2": sda,
		"1": sdb,
	})
}

func (s *BlockDevicesSuite) TestSetMachineBlockDevicesConcurrently(c *gc.C) {
	sdaInner := state.BlockDeviceInfo{DeviceName: "sda"}
	defer state.SetBeforeHooks(c, s.State, func() {
		err := s.machine.SetMachineBlockDevices(sdaInner)
		c.Assert(err, gc.IsNil)
	}).Check()

	sdaOuter := state.BlockDeviceInfo{
		DeviceName: "sda",
		Label:      "root",
	}
	err := s.machine.SetMachineBlockDevices(sdaOuter)
	c.Assert(err, gc.IsNil)

	// SetMachineBlockDevices will not remove concurrently added
	// block devices. This is fine in practice, because there is
	// a single worker responsible for populating machine block
	// devices.
	s.assertBlockDevices(c, map[string]state.BlockDeviceInfo{
		"1": sdaInner,
		// The outer call gets 0 because it's called first;
		// the before-hook call is called second but completes
		// first.
		"0": sdaOuter,
	})
}

func (s *BlockDevicesSuite) TestSetMachineBlockDevicesEmpty(c *gc.C) {
	sda := state.BlockDeviceInfo{DeviceName: "sda"}
	err := s.machine.SetMachineBlockDevices(sda)
	c.Assert(err, gc.IsNil)
	s.assertBlockDevices(c, map[string]state.BlockDeviceInfo{"0": sda})

	err = s.machine.SetMachineBlockDevices()
	c.Assert(err, gc.IsNil)
	s.assertBlockDevices(c, map[string]state.BlockDeviceInfo{})
}

func (s *BlockDevicesSuite) TestBlockDevicesMachineRemove(c *gc.C) {
	sda := state.BlockDeviceInfo{DeviceName: "sda"}
	err := s.machine.SetMachineBlockDevices(sda)
	c.Assert(err, gc.IsNil)

	err = s.machine.EnsureDead()
	c.Assert(err, jc.ErrorIsNil)
	err = s.machine.Remove()
	c.Assert(err, jc.ErrorIsNil)

	s.assertBlockDevices(c, map[string]state.BlockDeviceInfo{})
}

func (s *BlockDevicesSuite) TestMachineWatchAttachedBlockDevices(c *gc.C) {
	sda := state.BlockDeviceInfo{DeviceName: "sda"}
	sdb := state.BlockDeviceInfo{DeviceName: "sdb"}
	sdc := state.BlockDeviceInfo{DeviceName: "sdc"}
	err := s.machine.SetMachineBlockDevices(sda, sdb, sdc)
	c.Assert(err, gc.IsNil)

	// Start attached block device watcher.
	w := s.machine.WatchAttachedBlockDevices()
	defer testing.AssertStop(c, w)
	wc := testing.NewStringsWatcherC(c, s.State, w)
	assertOneChange := func(names ...string) {
		wc.AssertChangeInSingleEvent(names...)
		wc.AssertNoChange()
	}
	assertOneChange("0", "1", "2")

	// Setting the same should not trigger the watcher.
	err = s.machine.SetMachineBlockDevices(sdc, sdb, sda)
	c.Assert(err, gc.IsNil)
	wc.AssertNoChange()

	// change sdb's label.
	sdb.Label = "fatty"
	err = s.machine.SetMachineBlockDevices(sda, sdb, sdc)
	c.Assert(err, gc.IsNil)
	assertOneChange("1")

	// change sda's label and sdb's UUID at once.
	sda.Label = "giggly"
	sdb.UUID = "4c062658-6225-4f4b-96f3-debf00b964b4"
	err = s.machine.SetMachineBlockDevices(sda, sdb, sdc)
	c.Assert(err, gc.IsNil)
	assertOneChange("0", "1")

	// drop sdc.
	err = s.machine.SetMachineBlockDevices(sda, sdb)
	c.Assert(err, gc.IsNil)
	assertOneChange("2")

	// add sdc again: should get a new name.
	err = s.machine.SetMachineBlockDevices(sda, sdb, sdc)
	c.Assert(err, gc.IsNil)
	assertOneChange("3")
}

func (s *BlockDevicesSuite) TestUnitWatchAttachedBlockDevices(c *gc.C) {
	sda := state.BlockDeviceInfo{DeviceName: "sda"}
	sdb := state.BlockDeviceInfo{DeviceName: "sdb"}
	err := s.machine.SetMachineBlockDevices(sda, sdb)
	c.Assert(err, gc.IsNil)

	err = s.unit.AssignToMachine(s.machine)
	c.Assert(err, jc.ErrorIsNil)
	s.setBlockDeviceUnit(c, "0", s.unit.Name())

	// Start watching block devices attached to
	// the machine, and assigned to the unit.
	w, err := s.unit.WatchAttachedBlockDevices()
	c.Assert(err, jc.ErrorIsNil)
	defer testing.AssertStop(c, w)
	wc := testing.NewStringsWatcherC(c, s.State, w)
	assertOneChange := func(names ...string) {
		wc.AssertChangeInSingleEvent(names...)
		wc.AssertNoChange()
	}
	// Only one disk is assigned to the unit initially.
	assertOneChange("0")

	// Setting the same unit should not trigger.
	s.setBlockDeviceUnit(c, "0", s.unit.Name())
	wc.AssertNoChange()

	// Assigning another unit should trigger.
	s.setBlockDeviceUnit(c, "1", s.unit.Name())
	assertOneChange("1")

	// Unassigning from a unit should trigger.
	s.setBlockDeviceUnit(c, "1", "")
	assertOneChange("1")

	// Unattaching disk should trigger for any that
	// were still assigned to the unit.
	err = s.machine.SetMachineBlockDevices()
	c.Assert(err, gc.IsNil)
	assertOneChange("0")
}

func (s *BlockDevicesSuite) setBlockDeviceUnit(c *gc.C, deviceName, unitName string) {
	ops := []txn.Op{{
		C:      state.BlockDevicesC,
		Id:     state.DocID(s.State, deviceName),
		Assert: txn.DocExists,
		Update: bson.D{{"$set", bson.D{{"unit", unitName}}}},
	}}
	err := state.RunTransaction(s.State, ops)
	c.Assert(err, jc.ErrorIsNil)
}
