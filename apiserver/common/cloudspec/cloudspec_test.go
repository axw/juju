// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloudspec_test

import (
	"errors"

	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/apiserver/common/cloudspec"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/environs"
)

type CloudSpecSuite struct {
	testing.IsolationSuite
	testing.Stub
	result environs.CloudSpec
	api    cloudspec.CloudSpecAPI
}

var _ = gc.Suite(&CloudSpecSuite{})

func (s *CloudSpecSuite) SetUpTest(c *gc.C) {
	s.IsolationSuite.SetUpTest(c)
	s.Stub.ResetCalls()
	s.api = cloudspec.NewCloudSpec(func() (environs.CloudSpec, error) {
		return s.result, s.NextErr()
	})

	credential := cloud.NewCredential(
		"auth-type",
		map[string]string{"k": "v"},
	)
	s.result = environs.CloudSpec{
		"type",
		"cloud",
		"region",
		"endpoint",
		"storage-endpoint",
		&credential,
	}
}

func (s *CloudSpecSuite) TestCloudSpec(c *gc.C) {
	result, err := s.api.CloudSpec()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result, jc.DeepEquals, params.CloudSpecResult{
		Result: &params.CloudSpec{
			"type",
			"cloud",
			"region",
			"endpoint",
			"storage-endpoint",
			&params.CloudCredential{
				"auth-type",
				map[string]string{"k": "v"},
			},
		},
	})
}

func (s *CloudSpecSuite) TestCloudSpecNilCredential(c *gc.C) {
	s.result.Credential = nil
	result, err := s.api.CloudSpec()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result, jc.DeepEquals, params.CloudSpecResult{
		Result: &params.CloudSpec{
			"type",
			"cloud",
			"region",
			"endpoint",
			"storage-endpoint",
			nil,
		},
	})
}

func (s *CloudSpecSuite) TestCloudSpecError(c *gc.C) {
	expect := errors.New("bewm")
	s.SetErrors(expect)
	_, err := s.api.CloudSpec()
	c.Assert(err, gc.Equals, expect)
}
