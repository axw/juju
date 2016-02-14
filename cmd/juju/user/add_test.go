// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package user_test

import (
	"strings"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/cmd/juju/user"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/jujuclient"
	"github.com/juju/juju/jujuclient/jujuclienttesting"
	"github.com/juju/juju/testing"
)

// All of the functionality of the AddUser api call is contained elsewhere.
// This suite provides basic tests for the "add-user" command
type UserAddCommandSuite struct {
	testing.FakeJujuXDGDataHomeSuite
	mockAPI *mockAddUserAPI
	store   jujuclient.ClientStore
}

var _ = gc.Suite(&UserAddCommandSuite{})

func (s *UserAddCommandSuite) SetUpTest(c *gc.C) {
	s.FakeJujuXDGDataHomeSuite.SetUpTest(c)
	s.mockAPI = &mockAddUserAPI{}
	s.mockAPI.secretKey = []byte(strings.Repeat("X", 32))

	store := jujuclienttesting.NewMemControllerStore()
	store.UpdateController("testing", jujuclient.ControllerDetails{
		APIEndpoints:   []string{"127.0.0.1:12345"},
		CACert:         testing.CACert,
		ControllerUUID: testing.ModelTag.Id(),
	})
	s.store = store
	err := modelcmd.WriteCurrentController("testing")
	c.Assert(err, jc.ErrorIsNil)
}

func (s *UserAddCommandSuite) run(c *gc.C, args ...string) (*cmd.Context, error) {
	addCommand, underlying := user.NewAddCommandForTest(s.mockAPI)
	underlying.SetClientStore(s.store)
	return testing.RunCommand(c, addCommand, args...)
}

func (s *UserAddCommandSuite) TestInit(c *gc.C) {
	for i, test := range []struct {
		args        []string
		user        string
		displayname string
		outPath     string
		errorString string
	}{{
		errorString: "no username supplied",
	}, {
		args: []string{"foobar"},
		user: "foobar",
	}, {
		args:        []string{"foobar", "Foo Bar"},
		user:        "foobar",
		displayname: "Foo Bar",
	}, {
		args:        []string{"foobar", "Foo Bar", "extra"},
		errorString: `unrecognized args: \["extra"\]`,
	}} {
		c.Logf("test %d (%q)", i, test.args)
		wrappedCommand, command := user.NewAddCommandForTest(s.mockAPI)
		command.SetClientStore(s.store)
		err := testing.InitCommand(wrappedCommand, test.args)
		if test.errorString == "" {
			c.Check(err, jc.ErrorIsNil)
			c.Check(command.User, gc.Equals, test.user)
			c.Check(command.DisplayName, gc.Equals, test.displayname)
		} else {
			c.Check(err, gc.ErrorMatches, test.errorString)
		}
	}
}

/*
func (s *UserAddCommandSuite) TestRandomPassword(c *gc.C) {
	_, err := s.run(c, "foobar")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(s.randomPassword, gc.HasLen, 24)
}
*/

func (s *UserAddCommandSuite) TestAddUserWithWithUsername(c *gc.C) {
	context, err := s.run(c, "foobar")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(s.mockAPI.username, gc.Equals, "foobar")
	c.Assert(s.mockAPI.displayname, gc.Equals, "")
	expected := `
User "foobar" added
Please send this command to foobar:
    juju register MD0TBmZvb2JhcjAREw8xMjcuMC4wLjE6MTIzNDUEIFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhY
`[1:]
	c.Assert(testing.Stdout(context), gc.Equals, expected)
	c.Assert(testing.Stderr(context), gc.Equals, "")
}

func (s *UserAddCommandSuite) TestAddUserWithUsernameAndDisplayname(c *gc.C) {
	context, err := s.run(c, "foobar", "Foo Bar")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(s.mockAPI.username, gc.Equals, "foobar")
	c.Assert(s.mockAPI.displayname, gc.Equals, "Foo Bar")
	expected := `
User "Foo Bar (foobar)" added
Please send this command to foobar:
    juju register MD0TBmZvb2JhcjAREw8xMjcuMC4wLjE6MTIzNDUEIFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhY
`[1:]
	c.Assert(testing.Stdout(context), gc.Equals, expected)
	c.Assert(testing.Stderr(context), gc.Equals, "")
}

func (s *UserAddCommandSuite) TestBlockAddUser(c *gc.C) {
	// Block operation
	s.mockAPI.blocked = true
	_, err := s.run(c, "foobar", "Foo Bar")
	c.Assert(err, gc.ErrorMatches, cmd.ErrSilent.Error())
	// msg is logged
	stripped := strings.Replace(c.GetTestLog(), "\n", "", -1)
	c.Check(stripped, gc.Matches, ".*To unblock changes.*")
}

func (s *UserAddCommandSuite) TestAddUserErrorResponse(c *gc.C) {
	s.mockAPI.failMessage = "failed to create user, chaos ensues"
	_, err := s.run(c, "foobar")
	c.Assert(err, gc.ErrorMatches, s.mockAPI.failMessage)
}

type mockAddUserAPI struct {
	failMessage string
	blocked     bool
	secretKey   []byte

	username    string
	displayname string
	password    string
}

func (m *mockAddUserAPI) AddUser(username, displayname, password string) (names.UserTag, []byte, error) {
	if m.blocked {
		return names.UserTag{}, nil, common.OperationBlockedError("the operation has been blocked")
	}
	m.username = username
	m.displayname = displayname
	m.password = password
	if m.failMessage != "" {
		return names.UserTag{}, nil, errors.New(m.failMessage)
	}
	return names.NewLocalUserTag(username), m.secretKey, nil
}

func (*mockAddUserAPI) Close() error {
	return nil
}
