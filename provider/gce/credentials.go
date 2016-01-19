// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package gce

import (
	"github.com/juju/errors"
	"github.com/juju/juju/cloud"
)

type environProviderCredentials struct{}

func (environProviderCredentials) CredentialSchemas() map[cloud.AuthType]cloud.CredentialSchema {
	return map[cloud.AuthType]cloud.CredentialSchema{
		cloud.OAuth2AuthType: {
			"client-id": {
				Description: "client ID",
			},
			"client-email": {
				Description: "client e-mail address",
			},
			"private-key": {
				Description: "client secret",
			},
			"project-id": {
				Description: "project ID",
			},
		},
	}
}

func (environProviderCredentials) DetectCredentials() (*cloud.Credential, error) {
	return nil, errors.NotFoundf("credentials")
}
