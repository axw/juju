// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloudspec

import (
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/environs"
)

// CloudSpecAPI implements common methods for use by various
// facades for querying the cloud spec of models.
type CloudSpecAPI struct {
	getCloudSpec func() (environs.CloudSpec, error)
}

// NewCloudSpec returns a new NewCloudSpecAPI.
func NewCloudSpec(getCloudSpec func() (environs.CloudSpec, error)) CloudSpecAPI {
	return CloudSpecAPI{getCloudSpec}
}

// CloudSpec returns the model's cloud spec.
func (s CloudSpecAPI) CloudSpec() (params.CloudSpecResult, error) {
	spec, err := s.getCloudSpec()
	if err != nil {
		return params.CloudSpecResult{}, err
	}
	var paramsCloudCredential *params.CloudCredential
	if spec.Credential != nil && spec.Credential.AuthType() != "" {
		paramsCloudCredential = &params.CloudCredential{
			string(spec.Credential.AuthType()),
			spec.Credential.Attributes(),
		}
	}
	paramsSpec := params.CloudSpec{
		spec.Type,
		spec.Cloud,
		spec.Region,
		spec.Endpoint,
		spec.StorageEndpoint,
		paramsCloudCredential,
	}
	return params.CloudSpecResult{Result: &paramsSpec}, nil
}
