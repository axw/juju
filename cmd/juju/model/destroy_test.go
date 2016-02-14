// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package model_test

import (
	"bytes"
	"time"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/cmd/juju/model"
	"github.com/juju/juju/cmd/modelcmd"
	cmdtesting "github.com/juju/juju/cmd/testing"
	"github.com/juju/juju/jujuclient"
	"github.com/juju/juju/jujuclient/jujuclienttesting"
	_ "github.com/juju/juju/provider/dummy"
	"github.com/juju/juju/testing"
)

type DestroySuite struct {
	testing.FakeJujuXDGDataHomeSuite
	api            *fakeDestroyAPI
	store          jujuclient.ClientStore
	controllerName string
}

var _ = gc.Suite(&DestroySuite{})

// fakeDestroyAPI mocks out the cient API
type fakeDestroyAPI struct {
	err error
	env map[string]interface{}
}

func (f *fakeDestroyAPI) Close() error { return nil }

func (f *fakeDestroyAPI) DestroyModel() error {
	return f.err
}

func (s *DestroySuite) SetUpTest(c *gc.C) {
	s.FakeJujuXDGDataHomeSuite.SetUpTest(c)
	s.api = &fakeDestroyAPI{}
	s.api.err = nil
	s.store = jujuclienttesting.NewMemControllerStore()
	s.controllerName = "test-controller"

	err := s.store.UpdateController(s.controllerName, jujuclient.ControllerDetails{
		APIEndpoints:   []string{"localhost:1234"},
		CACert:         testing.CACert,
		ControllerUUID: "test1-uuid",
	})
	c.Assert(err, jc.ErrorIsNil)
	err = modelcmd.WriteCurrentController(s.controllerName)
	c.Assert(err, jc.ErrorIsNil)

	var modelList = []struct {
		name string
		uuid string
	}{{
		name: "test1",
		uuid: "test1-uuid",
	}, {
		name: "test2",
		uuid: "test2-uuid",
	}}
	for _, model := range modelList {
		s.addModel(c, model.name, model.uuid)
	}
}

func (s *DestroySuite) addModel(c *gc.C, name, uuid string) {
	err := s.store.UpdateModel("test-controller", name, jujuclient.ModelDetails{
		uuid,
	})
	c.Assert(err, jc.ErrorIsNil)
}

func (s *DestroySuite) runDestroyCommand(c *gc.C, args ...string) (*cmd.Context, error) {
	cmd := model.NewDestroyCommandForTest(s.api, s.store)
	return testing.RunCommand(c, cmd, args...)
}

func (s *DestroySuite) NewDestroyCommand() cmd.Command {
	return model.NewDestroyCommandForTest(s.api, s.store)
}

func (s *DestroySuite) checkModelExistsInStore(c *gc.C, name string) {
	_, err := s.store.ModelByName(s.controllerName, name)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *DestroySuite) checkModelRemovedFromStore(c *gc.C, name string) {
	_, err := s.store.ModelByName(s.controllerName, name)
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
}

func (s *DestroySuite) TestDestroyNoEnvironmentNameError(c *gc.C) {
	_, err := s.runDestroyCommand(c)
	c.Assert(err, gc.ErrorMatches, "no model specified")
}

func (s *DestroySuite) TestDestroyBadFlags(c *gc.C) {
	_, err := s.runDestroyCommand(c, "-n")
	c.Assert(err, gc.ErrorMatches, "flag provided but not defined: -n")
}

func (s *DestroySuite) TestDestroyUnknownArgument(c *gc.C) {
	_, err := s.runDestroyCommand(c, "model", "whoops")
	c.Assert(err, gc.ErrorMatches, `unrecognized args: \["whoops"\]`)
}

func (s *DestroySuite) TestDestroyUnknownEnvironment(c *gc.C) {
	_, err := s.runDestroyCommand(c, "foo")
	c.Assert(err, gc.ErrorMatches, "model test-controller:foo not found")
}

func (s *DestroySuite) TestDestroyCannotConnectToAPI(c *gc.C) {
	s.api.err = errors.New("connection refused")
	_, err := s.runDestroyCommand(c, "test2", "-y")
	c.Assert(err, gc.ErrorMatches, "cannot destroy model: connection refused")
	c.Check(c.GetTestLog(), jc.Contains, "failed to destroy model \"test2\"")
	s.checkModelExistsInStore(c, "test2")
}

func (s *DestroySuite) TestSystemDestroyFails(c *gc.C) {
	_, err := s.runDestroyCommand(c, "test1", "-y")
	c.Assert(err, gc.ErrorMatches, `"test1" is a controller; use 'juju destroy-controller' to destroy it`)
	s.checkModelExistsInStore(c, "test1")
}

func (s *DestroySuite) TestDestroy(c *gc.C) {
	s.checkModelExistsInStore(c, "test2")
	_, err := s.runDestroyCommand(c, "test2", "-y")
	c.Assert(err, jc.ErrorIsNil)
	s.checkModelRemovedFromStore(c, "test2")
}

func (s *DestroySuite) TestFailedDestroyEnvironment(c *gc.C) {
	s.api.err = errors.New("permission denied")
	_, err := s.runDestroyCommand(c, "test2", "-y")
	c.Assert(err, gc.ErrorMatches, "cannot destroy model: permission denied")
	s.checkModelExistsInStore(c, "test2")
}

func (s *DestroySuite) TestDestroyCommandConfirmation(c *gc.C) {
	var stdin, stdout bytes.Buffer
	ctx, err := cmd.DefaultContext()
	c.Assert(err, jc.ErrorIsNil)
	ctx.Stdout = &stdout
	ctx.Stdin = &stdin

	// Ensure confirmation is requested if "-y" is not specified.
	stdin.WriteString("n")
	_, errc := cmdtesting.RunCommand(ctx, s.NewDestroyCommand(), "test2")
	select {
	case err := <-errc:
		c.Check(err, gc.ErrorMatches, "model destruction: aborted")
	case <-time.After(testing.LongWait):
		c.Fatalf("command took too long")
	}
	c.Check(testing.Stdout(ctx), gc.Matches, "WARNING!.*test2(.|\n)*")
	s.checkModelExistsInStore(c, "test1")

	// EOF on stdin: equivalent to answering no.
	stdin.Reset()
	stdout.Reset()
	_, errc = cmdtesting.RunCommand(ctx, s.NewDestroyCommand(), "test2")
	select {
	case err := <-errc:
		c.Check(err, gc.ErrorMatches, "model destruction: aborted")
	case <-time.After(testing.LongWait):
		c.Fatalf("command took too long")
	}
	c.Check(testing.Stdout(ctx), gc.Matches, "WARNING!.*test2(.|\n)*")
	s.checkModelExistsInStore(c, "test1")

	for _, answer := range []string{"y", "Y", "yes", "YES"} {
		stdin.Reset()
		stdout.Reset()
		stdin.WriteString(answer)
		_, errc = cmdtesting.RunCommand(ctx, s.NewDestroyCommand(), "test2")
		select {
		case err := <-errc:
			c.Check(err, jc.ErrorIsNil)
		case <-time.After(testing.LongWait):
			c.Fatalf("command took too long")
		}
		s.checkModelRemovedFromStore(c, "test2")

		// Add the test2 model back into the store for the next test
		s.addModel(c, "test2", "test2-uuid")
	}
}

func (s *DestroySuite) TestBlockedDestroy(c *gc.C) {
	s.api.err = &params.Error{Code: params.CodeOperationBlocked}
	s.runDestroyCommand(c, "test2", "-y")
	c.Check(c.GetTestLog(), jc.Contains, "To remove the block")
}
