// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state_test

import (
	"fmt"

	"github.com/juju/errors"
	jc "github.com/juju/testing/checkers"
	gc "launchpad.net/gocheck"

	"github.com/juju/juju/tools"
	"github.com/juju/juju/version"
)

type tooler interface {
	lifer
	AgentTools() (*tools.Tools, error)
	SetAgentVersion(v version.Binary) error
	Refresh() error
}

func newTools(vers, url string) *tools.Tools {
	return &tools.Tools{
		Version: version.MustParseBinary(vers),
		URL:     url,
		Size:    10,
		SHA256:  "1234",
	}
}

func testAgentTools(c *gc.C, obj tooler, agent string) {
	// object starts with zero'd tools.
	t, err := obj.AgentTools()
	c.Assert(t, gc.IsNil)
	c.Assert(err, jc.Satisfies, errors.IsNotFound)

	err = obj.SetAgentVersion(version.Binary{})
	c.Assert(err, gc.ErrorMatches, fmt.Sprintf("cannot set agent version for %s: empty series or arch", agent))

	v2 := version.MustParseBinary("7.8.9-quantal-amd64")
	err = obj.SetAgentVersion(v2)
	c.Assert(err, gc.IsNil)
	t3, err := obj.AgentTools()
	c.Assert(err, gc.IsNil)
	c.Assert(t3.Version, gc.DeepEquals, v2)
	err = obj.Refresh()
	c.Assert(err, gc.IsNil)
	t3, err = obj.AgentTools()
	c.Assert(err, gc.IsNil)
	c.Assert(t3.Version, gc.DeepEquals, v2)

	testWhenDying(c, obj, noErr, deadErr, func() error {
		return obj.SetAgentVersion(v2)
	})
}

var _ = gc.Suite(&ToolsSuite{})

type ToolsSuite struct {
	ConnSuite
}

func (s *ToolsSuite) TestAddTools(c *gc.C) {
	currentTools := &tools.Tools{
		Version: version.Current,
		Size:    123,
		SHA256:  "abcdef",
	}
	err := s.State.AddTools(currentTools)
	c.Assert(err, gc.IsNil)
	t, err := s.State.Tools(version.Current)
	c.Assert(err, gc.IsNil)
	c.Assert(*t, gc.Equals, *currentTools)
	err = s.State.AddTools(currentTools)
	c.Assert(err, jc.Satisfies, errors.IsAlreadyExists)
}

func (s *ToolsSuite) TestReplaceTools(c *gc.C) {
	currentTools := &tools.Tools{
		Version: version.Current,
		Size:    123,
		SHA256:  "abcdef",
	}
	for i := 0; i < 2; i++ {
		err := s.State.ReplaceTools(currentTools)
		c.Assert(err, gc.IsNil)
		t, err := s.State.Tools(version.Current)
		c.Assert(err, gc.IsNil)
		c.Assert(*t, gc.Equals, *currentTools)
	}
}
