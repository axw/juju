// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package joyent

import (
	"github.com/juju/errors"

	"github.com/juju/juju/cloud"
)

type environProviderCredentials struct{}

func (environProviderCredentials) CredentialSchemas() map[cloud.AuthType]cloud.CredentialSchema {
	return map[cloud.AuthType]cloud.CredentialSchema{
		// TODO(axw) need to set private-key from private-key-file if set.
		// TODO(axw) we need a more appropriate name for this authentication
		//           type. ssh?
		cloud.UserPassAuthType: {
			sdcUser: {
				Description: "SmartDataCenter user ID",
			},
			sdcKeyId: {
				Description: "SmartDataCenter key ID",
			},
			mantaUser: {
				Description: "Manta user ID",
			},
			mantaKeyId: {
				Description: "Manta key ID",
			},
			privateKey: {
				Description: "Private key used to sign requests",
			},
			algorithm: {
				Description: "Algorithm used to generate the private key",
			},
		},
	}
}

func (environProviderCredentials) DetectCredentials() (*cloud.Credential, error) {
	return nil, errors.NotFoundf("credentials")
}
