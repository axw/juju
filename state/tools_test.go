// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package state_test

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"

	"github.com/juju/errors"
	jc "github.com/juju/testing/checkers"
	gc "launchpad.net/gocheck"

	"github.com/juju/juju/state"
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
	s.testAddTools(c, "some-tools")
}

func (s *ToolsSuite) TestAddToolsReplaces(c *gc.C) {
	s.testAddTools(c, "abc")
	s.testAddTools(c, "def")
}

func (s *ToolsSuite) testAddTools(c *gc.C, content string) {
	var r io.Reader = bytes.NewReader([]byte(content))
	addedMetadata := state.ToolsMetadata{
		Version: version.Current,
		Size:    int64(len(content)),
		SHA256:  "hash(" + content + ")",
	}
	err := s.State.AddTools(r, addedMetadata)
	c.Assert(err, gc.IsNil)

	metadata, rc, err := s.State.Tools(version.Current)
	c.Assert(err, gc.IsNil)
	c.Assert(r, gc.NotNil)
	defer rc.Close()
	c.Assert(metadata, gc.Equals, addedMetadata)

	data, err := ioutil.ReadAll(rc)
	c.Assert(err, gc.IsNil)
	c.Assert(string(data), gc.Equals, content)
}

func bumpVersion(v version.Binary) version.Binary {
	v.Build++
	return v
}

func (s *ToolsSuite) TestAddToolsAlias(c *gc.C) {
	s.testAddTools(c, "abc")
	alias := bumpVersion(version.Current)
	err := s.State.AddToolsAlias(alias, version.Current)
	c.Assert(err, gc.IsNil)

	md1, r1, err := s.State.Tools(version.Current)
	c.Assert(err, gc.IsNil)
	defer r1.Close()
	c.Assert(md1.Version, gc.Equals, version.Current)

	md2, r2, err := s.State.Tools(alias)
	c.Assert(err, gc.IsNil)
	defer r2.Close()
	c.Assert(md2.Version, gc.Equals, alias)

	c.Assert(md1.Size, gc.Equals, md2.Size)
	c.Assert(md1.SHA256, gc.Equals, md2.SHA256)
	data1, err := ioutil.ReadAll(r1)
	c.Assert(err, gc.IsNil)
	data2, err := ioutil.ReadAll(r2)
	c.Assert(err, gc.IsNil)
	c.Assert(string(data1), gc.Equals, string(data2))
}

func (s *ToolsSuite) TestAddToolsAliasDoesNotReplace(c *gc.C) {
	s.testAddTools(c, "abc")
	alias := bumpVersion(version.Current)
	err := s.State.AddToolsAlias(alias, version.Current)
	c.Assert(err, gc.IsNil)
	err = s.State.AddToolsAlias(alias, version.Current)
	c.Assert(err, jc.Satisfies, errors.IsAlreadyExists)
}

func (s *ToolsSuite) TestAddToolsAliasNotExist(c *gc.C) {
	// try to alias a non-existent version
	alias := bumpVersion(version.Current)
	err := s.State.AddToolsAlias(alias, version.Current)
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
	_, _, err = s.State.Tools(alias)
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
}
