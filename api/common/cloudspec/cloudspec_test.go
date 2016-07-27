// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloudspec_test

import (
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	apitesting "github.com/juju/juju/api/base/testing"
	"github.com/juju/juju/api/common/cloudspec"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/environs"
)

var _ = gc.Suite(&CloudSpecSuite{})

type CloudSpecSuite struct {
	testing.IsolationSuite
}

func (s *CloudSpecSuite) TestNewCloudSpecAPI(c *gc.C) {
	api := cloudspec.NewCloudSpecAPI(nil)
	c.Check(api, gc.NotNil)
}

func (s *CloudSpecSuite) TestCloudSpec(c *gc.C) {
	facadeCaller := apitesting.StubFacadeCaller{Stub: &testing.Stub{}}
	facadeCaller.FacadeCallFn = func(name string, args, response interface{}) error {
		c.Assert(name, gc.Equals, "CloudSpec")
		c.Assert(args, gc.IsNil)
		*(response.(*params.CloudSpecResult)) = params.CloudSpecResult{
			Result: &params.CloudSpec{
				Type:            "type",
				Cloud:           "cloud",
				Region:          "region",
				Endpoint:        "endpoint",
				StorageEndpoint: "storage-endpoint",
				Credential: &params.CloudCredential{
					AuthType:   "auth-type",
					Attributes: map[string]string{"k": "v"},
				},
			},
		}
		return nil
	}
	api := cloudspec.NewCloudSpecAPI(&facadeCaller)
	cloudSpec, err := api.CloudSpec()
	c.Assert(err, jc.ErrorIsNil)

	credential := cloud.NewCredential(
		"auth-type",
		map[string]string{"k": "v"},
	)
	c.Assert(cloudSpec, jc.DeepEquals, environs.CloudSpec{
		Type:            "type",
		Cloud:           "cloud",
		Region:          "region",
		Endpoint:        "endpoint",
		StorageEndpoint: "storage-endpoint",
		Credential:      &credential,
	})

	//outputCfg, err := st.ModelConfig()
	//c.Assert(err, jc.ErrorIsNil)
	//c.Assert(outputCfg.AllAttrs(), jc.DeepEquals, inputCfg.AllAttrs())
}
