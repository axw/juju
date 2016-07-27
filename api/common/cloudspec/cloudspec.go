// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloudspec

import (
	"github.com/juju/juju/api/base"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/environs"
)

// CloudSpecAPI provides common client-side API functions
// to call into apiserver/common/cloudspec.CloudSpec.
type CloudSpecAPI struct {
	facade base.FacadeCaller
}

// NewCloudSpecAPI creates a CloudSpecAPI on the specified facade,
// and uses this name when calling through the caller.
func NewCloudSpecAPI(facade base.FacadeCaller) *CloudSpecAPI {
	return &CloudSpecAPI{facade}
}

// CloudSpec returns the cloud specification for the given model.
func (e *CloudSpecAPI) CloudSpec() (environs.CloudSpec, error) {
	var result params.CloudSpecResult
	err := e.facade.FacadeCall("CloudSpec", nil, &result)
	if err != nil {
		return environs.CloudSpec{}, err
	}
	if result.Error != nil {
		return environs.CloudSpec{}, result.Error
	}
	var credential *cloud.Credential
	if result.Result.Credential != nil {
		credentialValue := cloud.NewCredential(
			cloud.AuthType(result.Result.Credential.AuthType),
			result.Result.Credential.Attributes,
		)
		credential = &credentialValue
	}
	return environs.CloudSpec{
		result.Result.Type,
		result.Result.Cloud,
		result.Result.Region,
		result.Result.Endpoint,
		result.Result.StorageEndpoint,
		credential,
	}, nil
}
