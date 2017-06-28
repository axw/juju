// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelupgrader_test

import (
	jc "github.com/juju/testing/checkers"
	"github.com/juju/version"
	gc "gopkg.in/check.v1"
	names "gopkg.in/juju/names.v2"

	"github.com/juju/juju/api/base/testing"
	"github.com/juju/juju/api/modelupgrader"
	"github.com/juju/juju/apiserver/params"
	coretesting "github.com/juju/juju/testing"
)

var (
	modelTag = names.NewModelTag("e5757df7-c86a-4835-84bc-7174af535d25")
	version1 = version.MustParse("1.0.0")
)

var _ = gc.Suite(&ModelUpgraderSuite{})

type ModelUpgraderSuite struct {
	coretesting.BaseSuite
}

var nullAPICaller = testing.APICallerFunc(
	func(objType string, version int, id, request string, arg, result interface{}) error {
		return nil
	},
)

func (s *ModelUpgraderSuite) TestModelEnvironVersion(c *gc.C) {
	apiCaller := testing.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		c.Check(objType, gc.Equals, "ModelUpgrader")
		c.Check(version, gc.Equals, 0)
		c.Check(id, gc.Equals, "")
		c.Check(request, gc.Equals, "ModelEnvironVersion")
		c.Check(arg, jc.DeepEquals, &params.Entities{
			Entities: []params.Entity{{Tag: modelTag.String()}},
		})
		c.Assert(result, gc.FitsTypeOf, &params.VersionResults{})
		*(result.(*params.VersionResults)) = params.VersionResults{
			Results: []params.VersionResult{{
				Version: &version1,
			}},
		}
		return nil
	})

	client := modelupgrader.NewClient(apiCaller)
	version, err := client.ModelEnvironVersion(modelTag)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(version, jc.DeepEquals, version1)
}

func (s *ModelUpgraderSuite) TestModelEnvironVersionError(c *gc.C) {
	apiCaller := testing.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		*(result.(*params.VersionResults)) = params.VersionResults{
			Results: []params.VersionResult{{
				Error: &params.Error{Message: "foo"},
			}},
		}
		return nil
	})

	client := modelupgrader.NewClient(apiCaller)
	_, err := client.ModelEnvironVersion(modelTag)
	c.Assert(err, gc.ErrorMatches, "foo")
}

func (s *ModelUpgraderSuite) TestModelEnvironNilVersion(c *gc.C) {
	apiCaller := testing.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		*(result.(*params.VersionResults)) = params.VersionResults{
			Results: []params.VersionResult{{}},
		}
		return nil
	})

	client := modelupgrader.NewClient(apiCaller)
	_, err := client.ModelEnvironVersion(modelTag)
	c.Assert(err, gc.ErrorMatches, "nil version returned")
}

func (s *ModelUpgraderSuite) TestModelEnvironArityMismatch(c *gc.C) {
	apiCaller := testing.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		*(result.(*params.VersionResults)) = params.VersionResults{
			Results: []params.VersionResult{{}, {}},
		}
		return nil
	})

	client := modelupgrader.NewClient(apiCaller)
	_, err := client.ModelEnvironVersion(modelTag)
	c.Assert(err, gc.ErrorMatches, "expected 1 result, got 2")
}

func (s *ModelUpgraderSuite) TestSetModelEnvironVersion(c *gc.C) {
	apiCaller := testing.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		c.Check(objType, gc.Equals, "ModelUpgrader")
		c.Check(version, gc.Equals, 0)
		c.Check(id, gc.Equals, "")
		c.Check(request, gc.Equals, "SetModelEnvironVersion")
		c.Check(arg, jc.DeepEquals, &params.EntityVersionNumbers{
			Entities: []params.EntityVersionNumber{{
				Tag:     modelTag.String(),
				Version: version1.String(),
			}},
		})
		c.Assert(result, gc.FitsTypeOf, &params.ErrorResults{})
		*(result.(*params.ErrorResults)) = params.ErrorResults{
			Results: []params.ErrorResult{{Error: &params.Error{Message: "foo"}}},
		}
		return nil
	})

	client := modelupgrader.NewClient(apiCaller)
	err := client.SetModelEnvironVersion(modelTag, version1)
	c.Assert(err, gc.ErrorMatches, "foo")
}

func (s *ModelUpgraderSuite) TestSetModelEnvironVersionArityMismatch(c *gc.C) {
	apiCaller := testing.APICallerFunc(func(objType string, version int, id, request string, arg, result interface{}) error {
		*(result.(*params.ErrorResults)) = params.ErrorResults{
			Results: []params.ErrorResult{{}, {}},
		}
		return nil
	})

	client := modelupgrader.NewClient(apiCaller)
	err := client.SetModelEnvironVersion(modelTag, version1)
	c.Assert(err, gc.ErrorMatches, "expected 1 result, got 2")
}
