// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelupgrader_test

import (
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/version"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/apiserver/common"
	"github.com/juju/juju/apiserver/modelupgrader"
	"github.com/juju/juju/apiserver/params"
	apiservertesting "github.com/juju/juju/apiserver/testing"
)

var (
	modelTag1 = names.NewModelTag("6e114b25-fc6d-448e-b58a-22fff690689e")
	modelTag2 = names.NewModelTag("631d2cbe-1085-4b74-ab76-41badfc73d9a")
	version0  = version.Zero
	version1  = version.MustParse("1.0.0")
)

type ModelUpgraderSuite struct {
	testing.IsolationSuite
	backend    mockBackend
	authorizer apiservertesting.FakeAuthorizer
}

var _ = gc.Suite(&ModelUpgraderSuite{})

func (s *ModelUpgraderSuite) SetUpTest(c *gc.C) {
	s.IsolationSuite.SetUpTest(c)
	s.authorizer = apiservertesting.FakeAuthorizer{
		Controller: true,
		Tag:        names.NewMachineTag("0"),
	}
	s.backend = mockBackend{
		models: map[names.ModelTag]*mockModel{
			modelTag1: {v: version0},
			modelTag2: {v: version1},
		},
	}
}

func (s *ModelUpgraderSuite) TestAuthController(c *gc.C) {
	_, err := modelupgrader.NewFacade(&s.backend, &s.authorizer)
	c.Assert(err, jc.ErrorIsNil)
}

func (s *ModelUpgraderSuite) TestAuthNonController(c *gc.C) {
	s.authorizer.Controller = false
	s.authorizer.Tag = names.NewUserTag("admin")
	_, err := modelupgrader.NewFacade(&s.backend, &s.authorizer)
	c.Assert(err, gc.Equals, common.ErrPerm)
}

func (s *ModelUpgraderSuite) TestModelEnvironVersion(c *gc.C) {
	facade, err := modelupgrader.NewFacade(&s.backend, &s.authorizer)
	c.Assert(err, jc.ErrorIsNil)
	results, err := facade.ModelEnvironVersion(params.Entities{
		Entities: []params.Entity{
			{Tag: modelTag1.String()},
			{Tag: modelTag2.String()},
			{Tag: "machine-0"},
		},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, jc.DeepEquals, params.VersionResults{
		Results: []params.VersionResult{{
			Version: &version0,
		}, {
			Version: &version1,
		}, {
			Error: &params.Error{Message: `"machine-0" is not a valid model tag`},
		}},
	})
	s.backend.CheckCalls(c, []testing.StubCall{
		{"GetModel", []interface{}{modelTag1}},
		{"GetModel", []interface{}{modelTag2}},
	})
	s.backend.models[modelTag1].CheckCallNames(c, "EnvironVersion")
	s.backend.models[modelTag2].CheckCallNames(c, "EnvironVersion")
}

func (s *ModelUpgraderSuite) TestSetModelEnvironVersion(c *gc.C) {
	facade, err := modelupgrader.NewFacade(&s.backend, &s.authorizer)
	c.Assert(err, jc.ErrorIsNil)
	results, err := facade.SetModelEnvironVersion(params.EntityVersionNumbers{
		Entities: []params.EntityVersionNumber{
			{Tag: modelTag1.String(), Version: version1.String()},
			{Tag: modelTag2.String(), Version: "blargh"},
			{Tag: "machine-0", Version: version0.String()},
		},
	})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, jc.DeepEquals, params.ErrorResults{
		Results: []params.ErrorResult{
			{},
			{&params.Error{Message: `invalid version "blargh"`}},
			{&params.Error{Message: `"machine-0" is not a valid model tag`}},
		},
	})
	s.backend.CheckCalls(c, []testing.StubCall{
		{"GetModel", []interface{}{modelTag1}},
	})
	s.backend.models[modelTag1].CheckCalls(c, []testing.StubCall{
		{"SetEnvironVersion", []interface{}{version1}},
	})
	s.backend.models[modelTag2].CheckNoCalls(c)
}

type mockBackend struct {
	testing.Stub
	models map[names.ModelTag]*mockModel
}

func (b *mockBackend) GetModel(tag names.ModelTag) (modelupgrader.Model, error) {
	b.MethodCall(b, "GetModel", tag)
	return b.models[tag], b.NextErr()
}

type mockModel struct {
	testing.Stub
	v version.Number
}

func (m *mockModel) EnvironVersion() version.Number {
	m.MethodCall(m, "EnvironVersion")
	m.PopNoErr()
	return m.v
}

func (m *mockModel) SetEnvironVersion(v version.Number) error {
	m.MethodCall(m, "SetEnvironVersion", v)
	return m.NextErr()
}
