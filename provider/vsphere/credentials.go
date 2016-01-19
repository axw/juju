// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

// +build !gccgo

package vsphere

import (
	"github.com/juju/errors"

	"github.com/juju/juju/cloud"
)

type environProviderCredentials struct{}

func (environProviderCredentials) CredentialSchemas() map[cloud.AuthType]cloud.CredentialSchema {
	return map[cloud.AuthType]cloud.CredentialSchema{
		cloud.UserPassAuthType: {
			"user": {
				Description: "The username to authenticate with.",
			},
			"password": {
				Description: "The password to authenticate with.",
			},
		},
	}
}

func (environProviderCredentials) DetectCredentials() (*cloud.Credential, error) {
	return nil, errors.NotFoundf("credentials")
}
