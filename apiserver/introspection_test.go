// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver_test

import (
	"io/ioutil"
	"net/http"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/permission"
	"github.com/juju/juju/state"
)

type introspectionSuite struct {
	apiserverBaseSuite
	bob *state.User
	url string
}

var _ = gc.Suite(&introspectionSuite{})

func (s *introspectionSuite) SetUpTest(c *gc.C) {
	s.apiserverBaseSuite.SetUpTest(c)
	bob, err := s.State.AddUser("bob", "", "hunter2", "admin")
	c.Assert(err, jc.ErrorIsNil)
	s.bob = bob
	s.url = s.server.URL + "/introspection/navel"
}

func (s *introspectionSuite) TestAccess(c *gc.C) {
	s.testAccess(c, s.Owner.String(), ownerPassword)

	model, err := s.State.Model()
	c.Assert(err, jc.ErrorIsNil)
	_, err = model.AddUser(
		state.UserAccessSpec{
			User:      s.bob.UserTag(),
			CreatedBy: s.Owner,
			Access:    permission.ReadAccess,
		},
	)
	c.Assert(err, jc.ErrorIsNil)
	s.testAccess(c, "user-bob", "hunter2")
}

func (s *introspectionSuite) testAccess(c *gc.C, tag, password string) {
	resp := sendHTTPRequest(c, httpRequestParams{
		method:   "GET",
		url:      s.url,
		tag:      tag,
		password: password,
	})
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, gc.Equals, http.StatusOK)
	content, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(content), gc.Equals, "gazing")
}

func (s *introspectionSuite) TestAccessDenied(c *gc.C) {
	resp := sendHTTPRequest(c, httpRequestParams{
		method:   "GET",
		url:      s.url,
		tag:      "user-bob",
		password: "hunter2",
	})
	defer resp.Body.Close()
	c.Assert(resp.StatusCode, gc.Equals, http.StatusForbidden)
}
