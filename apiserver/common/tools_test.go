// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common_test

import (
	"fmt"
	"strings"

	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	gc "launchpad.net/gocheck"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/params"
	apiservertesting "github.com/juju/juju/apiserver/testing"
	"github.com/juju/juju/juju/testing"
	"github.com/juju/juju/state"
	"github.com/juju/juju/version"
)

type toolsSuite struct {
	testing.JujuConnSuite
	machine0 *state.Machine
}

var _ = gc.Suite(&toolsSuite{})

func (s *toolsSuite) SetUpTest(c *gc.C) {
	s.JujuConnSuite.SetUpTest(c)
	var err error
	s.machine0, err = s.State.AddMachine("series", state.JobHostUnits)
	c.Assert(err, gc.IsNil)
}

func (s *toolsSuite) TestTools(c *gc.C) {
	getCanRead := func() (common.AuthFunc, error) {
		return func(tag names.Tag) bool {
			return tag == names.NewMachineTag("0") || tag == names.NewMachineTag("42")
		}, nil
	}
	tg := common.NewToolsGetter(s.State, getCanRead)
	c.Assert(tg, gc.NotNil)

	err := s.machine0.SetAgentVersion(version.Current)
	c.Assert(err, gc.IsNil)

	err = s.State.AddTools(strings.NewReader(""), state.ToolsMetadata{Version: version.Current})
	c.Assert(err, gc.IsNil)

	args := params.Entities{
		Entities: []params.Entity{
			{Tag: "machine-0"},
			{Tag: "machine-1"},
			{Tag: "machine-42"},
		}}
	result, err := tg.Tools(args)
	c.Assert(err, gc.IsNil)
	c.Assert(result.Results, gc.HasLen, 3)
	c.Assert(result.Results[0].Tools, gc.NotNil)
	c.Assert(result.Results[0].Tools.Version, gc.DeepEquals, version.Current)
	c.Assert(result.Results[1].Error, gc.DeepEquals, apiservertesting.ErrUnauthorized)
	c.Assert(result.Results[2].Error, gc.DeepEquals, apiservertesting.NotFoundError("machine 42"))
}

func (s *toolsSuite) TestToolsError(c *gc.C) {
	getCanRead := func() (common.AuthFunc, error) {
		return nil, fmt.Errorf("splat")
	}
	tg := common.NewToolsGetter(s.State, getCanRead)
	args := params.Entities{
		Entities: []params.Entity{{Tag: "machine-42"}},
	}
	result, err := tg.Tools(args)
	c.Assert(err, gc.ErrorMatches, "splat")
	c.Assert(result.Results, gc.HasLen, 1)
}

func (s *toolsSuite) TestSetTools(c *gc.C) {
	getCanWrite := func() (common.AuthFunc, error) {
		return func(tag names.Tag) bool {
			return tag == names.NewMachineTag("0") || tag == names.NewMachineTag("42")
		}, nil
	}
	ts := common.NewToolsSetter(s.State, getCanWrite)
	c.Assert(ts, gc.NotNil)

	err := s.machine0.SetAgentVersion(version.Current)
	c.Assert(err, gc.IsNil)

	args := params.EntitiesVersion{
		AgentTools: []params.EntityVersion{{
			Tag: "machine-0",
			Tools: &params.Version{
				Version: version.Current,
			},
		}, {
			Tag: "machine-1",
			Tools: &params.Version{
				Version: version.Current,
			},
		}, {
			Tag: "machine-42",
			Tools: &params.Version{
				Version: version.Current,
			},
		}},
	}
	result, err := ts.SetTools(args)
	c.Assert(err, gc.IsNil)
	c.Assert(result.Results, gc.HasLen, 3)
	c.Assert(result.Results[0].Error, gc.IsNil)
	agentTools, err := s.machine0.AgentTools()
	c.Assert(err, gc.IsNil)
	c.Assert(agentTools.Version, gc.DeepEquals, version.Current)
	c.Assert(result.Results[1].Error, gc.DeepEquals, apiservertesting.ErrUnauthorized)
	c.Assert(result.Results[2].Error, gc.DeepEquals, apiservertesting.NotFoundError("machine 42"))
}

func (s *toolsSuite) TestToolsSetError(c *gc.C) {
	getCanWrite := func() (common.AuthFunc, error) {
		return nil, fmt.Errorf("splat")
	}
	ts := common.NewToolsSetter(s.State, getCanWrite)
	args := params.EntitiesVersion{
		AgentTools: []params.EntityVersion{{
			Tag: "machine-42",
			Tools: &params.Version{
				Version: version.Current,
			},
		}},
	}
	result, err := ts.SetTools(args)
	c.Assert(err, gc.ErrorMatches, "splat")
	c.Assert(result.Results, gc.HasLen, 1)
}

type mockToolsSource []state.ToolsMetadata

func (s mockToolsSource) AllToolsMetadata() ([]state.ToolsMetadata, error) {
	return []state.ToolsMetadata(s), nil
}

func (s *toolsSuite) TestFindTools(c *gc.C) {
	toolsSource := mockToolsSource([]state.ToolsMetadata{{
		Version: version.Current,
	}, {
		Version: version.Binary{
			Number: version.MustParse("123.456.789"),
			Series: "win81",
			Arch:   "alpha",
		},
	}})
	result, err := common.FindTools(toolsSource, params.FindToolsParams{
		MajorVersion: 123,
		MinorVersion: 456,
		Series:       "win81",
		Arch:         "alpha",
	})
	c.Assert(err, gc.IsNil)
	c.Assert(result.List, gc.HasLen, 1)
	c.Assert(result.List[0].Version.String(), gc.Equals, "123.456.789-win81-alpha")
}

func (s *toolsSuite) TestFindToolsNotFound(c *gc.C) {
	result, err := common.FindTools(mockToolsSource{}, params.FindToolsParams{})
	c.Assert(err, gc.IsNil)
	c.Assert(result.Error, jc.Satisfies, params.IsCodeNotFound)
}
