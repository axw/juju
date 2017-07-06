// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelregistrar_test

import (
	"github.com/juju/errors"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	worker "gopkg.in/juju/worker.v1"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/worker/dependency"
	dt "github.com/juju/juju/worker/dependency/testing"
	"github.com/juju/juju/worker/gate"
	"github.com/juju/juju/worker/modelregistrar"
)

type ManifoldSuite struct {
	testing.IsolationSuite
}

var _ = gc.Suite(&ManifoldSuite{})

func (*ManifoldSuite) TestInputs(c *gc.C) {
	manifold := modelregistrar.Manifold(modelregistrar.ManifoldConfig{})
	c.Check(manifold.Inputs, jc.DeepEquals, []string{})
}

func (*ManifoldSuite) TestMissingModelTag(c *gc.C) {
	context := dt.StubContext(nil, map[string]interface{}{})
	manifold := modelregistrar.Manifold(modelregistrar.ManifoldConfig{
		Registry: s.registry,
	})

	worker, err := manifold.Start(context)
	c.Check(worker, gc.IsNil)
	c.Check(errors.Cause(err), gc.Equals, dependency.ErrMissing)
}

func (*ManifoldSuite) TestMissingEnvironName(c *gc.C) {
	context := dt.StubContext(nil, map[string]interface{}{})
	manifold := modelregistrar.Manifold(modelregistrar.ManifoldConfig{
		ModelTag: coretesting.ModelTag,
	})

	worker, err := manifold.Start(context)
	c.Check(worker, gc.IsNil)
	c.Check(errors.Cause(err), gc.Equals, dependency.ErrMissing)
}

func (*ManifoldSuite) TestNewFacadeError(c *gc.C) {
	expectAPICaller := struct{ base.APICaller }{}
	expectEnviron := struct{ environs.Environ }{}
	expectGate := struct{ gate.Unlocker }{}
	context := dt.StubContext(nil, map[string]interface{}{
		"api-caller": expectAPICaller,
		"environ":    expectEnviron,
		"gate":       expectGate,
	})
	manifold := modelregistrar.Manifold(modelregistrar.ManifoldConfig{
		APICallerName: "api-caller",
		EnvironName:   "environ",
		GateName:      "gate",
		NewFacade: func(actual base.APICaller) (modelregistrar.Facade, error) {
			c.Check(actual, gc.Equals, expectAPICaller)
			return nil, errors.New("splort")
		},
	})

	worker, err := manifold.Start(context)
	c.Check(worker, gc.IsNil)
	c.Check(err, gc.ErrorMatches, "splort")
}

func (*ManifoldSuite) TestNewWorkerError(c *gc.C) {
	expectFacade := struct{ modelregistrar.Facade }{}
	context := dt.StubContext(nil, map[string]interface{}{
		"api-caller": struct{ base.APICaller }{},
		"environ":    struct{ environs.Environ }{},
		"gate":       struct{ gate.Unlocker }{},
	})
	manifold := modelregistrar.Manifold(modelregistrar.ManifoldConfig{
		APICallerName: "api-caller",
		EnvironName:   "environ",
		GateName:      "gate",
		NewFacade: func(_ base.APICaller) (modelregistrar.Facade, error) {
			return expectFacade, nil
		},
		NewWorker: func(config modelregistrar.Config) (worker.Worker, error) {
			c.Check(config.Facade, gc.Equals, expectFacade)
			return nil, errors.New("boof")
		},
	})

	worker, err := manifold.Start(context)
	c.Check(worker, gc.IsNil)
	c.Check(err, gc.ErrorMatches, "boof")
}

func (*ManifoldSuite) TestNewWorkerSuccess(c *gc.C) {
	expectWorker := &struct{ worker.Worker }{}
	context := dt.StubContext(nil, map[string]interface{}{
		"api-caller": struct{ base.APICaller }{},
		"environ":    struct{ environs.Environ }{},
		"gate":       struct{ gate.Unlocker }{},
	})
	manifold := modelregistrar.Manifold(modelregistrar.ManifoldConfig{
		APICallerName: "api-caller",
		EnvironName:   "environ",
		GateName:      "gate",
		NewFacade: func(_ base.APICaller) (modelregistrar.Facade, error) {
			return struct{ modelregistrar.Facade }{}, nil
		},
		NewWorker: func(_ modelregistrar.Config) (worker.Worker, error) {
			return expectWorker, nil
		},
	})

	worker, err := manifold.Start(context)
	c.Check(worker, gc.Equals, expectWorker)
	c.Check(err, jc.ErrorIsNil)
}
