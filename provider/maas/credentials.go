// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package maas

import (
	"github.com/juju/errors"
	"github.com/juju/juju/cloud"
)

type environProviderCredentials struct{}

func (environProviderCredentials) CredentialSchemas() map[cloud.AuthType]cloud.CredentialSchema {
	return map[cloud.AuthType]cloud.CredentialSchema{
		cloud.OAuth1AuthType: {
			"maas-oauth": {
				Description: "OAuth/API-key credentials",
			},
		},
	}
}

func (environProviderCredentials) DetectCredentials() (*cloud.Credential, error) {
	return nil, errors.NotFoundf("credentials")
}
