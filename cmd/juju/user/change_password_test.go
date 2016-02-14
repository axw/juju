// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package user_test

import (
	"errors"

	"github.com/juju/cmd"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/cmd/juju/user"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/jujuclient"
	"github.com/juju/juju/jujuclient/jujuclienttesting"
	coretesting "github.com/juju/juju/testing"
)

type ChangePasswordCommandSuite struct {
	coretesting.FakeJujuXDGDataHomeSuite
	mockAPI        *mockChangePasswordAPI
	store          jujuclient.ClientStore
	randomPassword string
}

var _ = gc.Suite(&ChangePasswordCommandSuite{})

func (s *ChangePasswordCommandSuite) SetUpTest(c *gc.C) {
	s.FakeJujuXDGDataHomeSuite.SetUpTest(c)
	s.mockAPI = &mockChangePasswordAPI{}
	s.store = jujuclienttesting.NewMemControllerStore()
	s.randomPassword = ""
	s.PatchValue(user.RandomPasswordNotify, func(pwd string) {
		s.randomPassword = pwd
	})

	err := s.store.UpdateAccount("testing", "current-user@local", jujuclient.AccountDetails{
		User:     "current-user@local",
		Password: "old-password",
	})
	c.Assert(err, jc.ErrorIsNil)
	err = s.store.UpdateAccount("testing", "other@local", jujuclient.AccountDetails{
		User:     "other@local",
		Password: "old-password",
	})
	c.Assert(err, jc.ErrorIsNil)

	err = s.store.SetCurrentAccount("testing", "current-user@local")
	c.Assert(err, jc.ErrorIsNil)

	s.PatchValue(user.ReadPassword, func() (string, error) {
		return "sekrit", nil
	})
	err = modelcmd.WriteCurrentController("testing")
}

func (s *ChangePasswordCommandSuite) run(c *gc.C, args ...string) (*cmd.Context, error) {
	wrapped, underlying := user.NewChangePasswordCommandForTest(s.mockAPI)
	underlying.SetClientStore(s.store)
	return coretesting.RunCommand(c, wrapped, args...)
}

func (s *ChangePasswordCommandSuite) TestInit(c *gc.C) {
	for i, test := range []struct {
		args        []string
		user        string
		outPath     string
		generate    bool
		errorString string
	}{
		{
		// no args is fine
		}, {
			args:     []string{"--generate"},
			generate: true,
		}, {
			args:     []string{"foobar"},
			user:     "foobar",
			generate: true,
		}, {
			args:     []string{"foobar", "--generate"},
			user:     "foobar",
			generate: true,
		}, {
			args:        []string{"--foobar"},
			errorString: "flag provided but not defined: --foobar",
		}, {
			args:        []string{"foobar", "extra"},
			errorString: `unrecognized args: \["extra"\]`,
		},
	} {
		c.Logf("test %d", i)
		wrappedCommand, command := user.NewChangePasswordCommandForTest(nil)
		err := coretesting.InitCommand(wrappedCommand, test.args)
		command.SetClientStore(s.store)
		if test.errorString == "" {
			c.Check(command.User, gc.Equals, test.user)
			c.Check(command.Generate, gc.Equals, test.generate)
		} else {
			c.Check(err, gc.ErrorMatches, test.errorString)
		}
	}
}

func (s *ChangePasswordCommandSuite) assertSetPassword(c *gc.C, user, pass string) {
	s.assertSetPasswordN(c, 0, user, pass)
}

func (s *ChangePasswordCommandSuite) assertSetPasswordN(c *gc.C, n int, user, pass string) {
	s.mockAPI.CheckCall(c, n, "SetPassword", user, pass)
}

func (s *ChangePasswordCommandSuite) assertStorePassword(c *gc.C, user, pass string) {
	details, err := s.store.AccountByName("testing", user)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(details.Password, gc.Equals, pass)
}

func (s *ChangePasswordCommandSuite) TestChangePassword(c *gc.C) {
	context, err := s.run(c)
	c.Assert(err, jc.ErrorIsNil)
	s.assertSetPassword(c, "current-user@local", "sekrit")
	expected := `
password: 
type password again: 
`[1:]
	c.Assert(coretesting.Stdout(context), gc.Equals, expected)
	c.Assert(coretesting.Stderr(context), gc.Equals, "Your password has been updated.\n")
}

func (s *ChangePasswordCommandSuite) TestChangePasswordGenerate(c *gc.C) {
	context, err := s.run(c, "--generate")
	c.Assert(err, jc.ErrorIsNil)
	s.assertSetPassword(c, "current-user@local", s.randomPassword)
	c.Assert(coretesting.Stderr(context), gc.Equals, "Your password has been updated.\n")
}

func (s *ChangePasswordCommandSuite) TestChangePasswordFail(c *gc.C) {
	s.mockAPI.SetErrors(errors.New("failed to do something"))
	_, err := s.run(c, "--generate")
	c.Assert(err, gc.ErrorMatches, "failed to do something")
	s.assertSetPassword(c, "current-user@local", s.randomPassword)
	s.assertStorePassword(c, "current-user@local", "old-password")
}

// The first write fails, so we try to revert the password which succeeds
func (s *ChangePasswordCommandSuite) TestRevertPasswordAfterFailedWrite(c *gc.C) {
	store := jujuclienttesting.NewStubStore()
	store.CurrentAccountFunc = func(string) (string, error) {
		return "account-name", nil
	}
	store.AccountByNameFunc = func(string, string) (*jujuclient.AccountDetails, error) {
		return &jujuclient.AccountDetails{"user", "old-password"}, nil
	}
	s.store = store
	store.SetErrors(errors.New("failed to write"))

	_, err := s.run(c, "--generate")
	c.Assert(err, gc.ErrorMatches, "failed to record password change for client: failed to write")
	s.assertSetPasswordN(c, 0, "user", s.randomPassword)
	s.assertSetPasswordN(c, 1, "user", "old-password")
}

// SetPassword api works the first time, but the write fails, our second call to set password fails
func (s *ChangePasswordCommandSuite) TestChangePasswordRevertApiFails(c *gc.C) {
	s.mockAPI.SetErrors(nil, errors.New("failed to do something"))
	store := jujuclienttesting.NewStubStore()
	store.CurrentAccountFunc = func(string) (string, error) {
		return "account-name", nil
	}
	store.AccountByNameFunc = func(string, string) (*jujuclient.AccountDetails, error) {
		return &jujuclient.AccountDetails{"user", "old-password"}, nil
	}
	s.store = store
	store.SetErrors(errors.New("failed to write"))

	_, err := s.run(c, "--generate")
	c.Assert(err, gc.ErrorMatches, "failed to set password back: failed to do something")
}

func (s *ChangePasswordCommandSuite) TestChangeOthersPassword(c *gc.C) {
	// The checks for user existence and admin rights are tested
	// at the apiserver level.
	_, err := s.run(c, "other")
	c.Assert(err, jc.ErrorIsNil)
	s.assertSetPassword(c, "other@local", s.randomPassword)
}

type mockChangePasswordAPI struct {
	testing.Stub
}

func (m *mockChangePasswordAPI) SetPassword(username, password string) error {
	m.MethodCall(m, "SetPassword", username, password)
	return m.NextErr()
}

func (*mockChangePasswordAPI) Close() error {
	return nil
}
