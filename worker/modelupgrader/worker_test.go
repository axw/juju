// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelupgrader_test

import (
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/version"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/environs"
	coretesting "github.com/juju/juju/testing"
	"github.com/juju/juju/worker/modelupgrader"
	"github.com/juju/juju/worker/workertest"
)

type WorkerSuite struct {
	testing.IsolationSuite
}

var _ = gc.Suite(&WorkerSuite{})

func (*WorkerSuite) TestNewWorkerValidatesConfig(c *gc.C) {
	_, err := modelupgrader.NewWorker(modelupgrader.Config{})
	c.Assert(err, gc.ErrorMatches, "nil Facade not valid")
}

func (*WorkerSuite) TestNewWorker(c *gc.C) {
	mockFacade := mockFacade{v: version.MustParse("1.2.3")}
	mockEnviron := mockEnviron{}
	mockGateUnlocker := mockGateUnlocker{}
	w, err := modelupgrader.NewWorker(modelupgrader.Config{
		Facade:        &mockFacade,
		Environ:       &mockEnviron,
		GateUnlocker:  &mockGateUnlocker,
		ControllerTag: coretesting.ControllerTag,
		ModelTag:      coretesting.ModelTag,
	})
	c.Assert(err, jc.ErrorIsNil)
	workertest.CheckKill(c, w)
	mockFacade.CheckCalls(c, []testing.StubCall{
		{"ModelEnvironVersion", []interface{}{coretesting.ModelTag}},
	})
	mockEnviron.CheckCallNames(c, "UpgradeOperations")
	mockGateUnlocker.CheckCallNames(c, "Unlock")
}

func (*WorkerSuite) TestNonUpgradeable(c *gc.C) {
	mockFacade := mockFacade{v: version.MustParse("1.2.3")}
	mockEnviron := struct{ environs.Environ }{} // not an Upgrader
	mockGateUnlocker := mockGateUnlocker{}
	w, err := modelupgrader.NewWorker(modelupgrader.Config{
		Facade:        &mockFacade,
		Environ:       &mockEnviron,
		GateUnlocker:  &mockGateUnlocker,
		ControllerTag: coretesting.ControllerTag,
		ModelTag:      coretesting.ModelTag,
	})
	c.Assert(err, jc.ErrorIsNil)
	workertest.CheckKill(c, w)
	mockFacade.CheckCalls(c, []testing.StubCall{
		{"ModelEnvironVersion", []interface{}{coretesting.ModelTag}},
	})
	mockGateUnlocker.CheckCallNames(c, "Unlock")
}

func (*WorkerSuite) TestRunUpgradeOperations(c *gc.C) {
	var stepsStub testing.Stub
	mockFacade := mockFacade{v: version.MustParse("1.2.3")}
	mockEnviron := mockEnviron{
		ops: []environs.UpgradeOperation{{
			TargetVersion: version.MustParse("1.2.2"),
			Steps: []environs.UpgradeStep{
				newStep(&stepsStub, "step122"),
			},
		}, {
			TargetVersion: version.MustParse("1.2.3"),
			Steps: []environs.UpgradeStep{
				newStep(&stepsStub, "step123"),
			},
		}, {
			TargetVersion: version.MustParse("1.2.4"),
			Steps: []environs.UpgradeStep{
				newStep(&stepsStub, "step124_0"),
				newStep(&stepsStub, "step124_1"),
			},
		}, {
			TargetVersion: version.MustParse("1.2.5"),
			Steps: []environs.UpgradeStep{
				newStep(&stepsStub, "step125"),
			},
		}},
	}
	mockGateUnlocker := mockGateUnlocker{}
	w, err := modelupgrader.NewWorker(modelupgrader.Config{
		Facade:        &mockFacade,
		Environ:       &mockEnviron,
		GateUnlocker:  &mockGateUnlocker,
		ControllerTag: coretesting.ControllerTag,
		ModelTag:      coretesting.ModelTag,
	})
	c.Assert(err, jc.ErrorIsNil)
	workertest.CheckKill(c, w)
	mockFacade.CheckCalls(c, []testing.StubCall{
		{"ModelEnvironVersion", []interface{}{coretesting.ModelTag}},
		{"SetModelEnvironVersion", []interface{}{
			coretesting.ModelTag, version.MustParse("1.2.4"),
		}},
		{"SetModelEnvironVersion", []interface{}{
			coretesting.ModelTag, version.MustParse("1.2.5"),
		}},
	})
	mockEnviron.CheckCallNames(c, "UpgradeOperations")
	mockGateUnlocker.CheckCallNames(c, "Unlock")

	stepArgs := environs.UpgradeStepParams{
		ControllerUUID: coretesting.ControllerTag.Id(),
	}
	stepsStub.CheckCalls(c, []testing.StubCall{
		{"step124_0", []interface{}{stepArgs}},
		{"step124_1", []interface{}{stepArgs}},
		{"step125", []interface{}{stepArgs}},
	})
}

func newStep(stub *testing.Stub, name string) environs.UpgradeStep {
	run := func(args environs.UpgradeStepParams) error {
		stub.AddCall(name, args)
		return stub.NextErr()
	}
	return mockUpgradeStep{name, run}
}

type mockUpgradeStep struct {
	description string
	run         func(environs.UpgradeStepParams) error
}

func (s mockUpgradeStep) Description() string {
	return s.description
}

func (s mockUpgradeStep) Run(args environs.UpgradeStepParams) error {
	return s.run(args)
}

type mockFacade struct {
	testing.Stub
	v version.Number
}

func (f *mockFacade) ModelEnvironVersion(tag names.ModelTag) (version.Number, error) {
	f.MethodCall(f, "ModelEnvironVersion", tag)
	return f.v, f.NextErr()
}

func (f *mockFacade) SetModelEnvironVersion(tag names.ModelTag, v version.Number) error {
	f.MethodCall(f, "SetModelEnvironVersion", tag, v)
	return f.NextErr()
}

type mockEnviron struct {
	environs.Environ
	testing.Stub
	ops []environs.UpgradeOperation
}

func (e *mockEnviron) UpgradeOperations() []environs.UpgradeOperation {
	e.MethodCall(e, "UpgradeOperations")
	e.PopNoErr()
	return e.ops
}

type mockGateUnlocker struct {
	testing.Stub
}

func (g *mockGateUnlocker) Unlock() {
	g.MethodCall(g, "Unlock")
	g.PopNoErr()
}
