// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"github.com/juju/errors"
	"github.com/juju/juju/cloud"
)

type environProviderCredentials struct{}

func (environProviderCredentials) CredentialSchemas() map[cloud.AuthType]cloud.CredentialSchema {
	return map[cloud.AuthType]cloud.CredentialSchema{
		cloud.UserPassAuthType: {
			configAttrAppId: {
				Description: "Azure Active Directory application ID",
			},
			configAttrSubscriptionId: {
				Description: "Azure subscription ID",
			},
			configAttrTenantId: {
				Description: "Azure Active Directory tenant ID",
			},
			configAttrAppPassword: {
				Description: "Azure Active Directory application password",
				Secret:      true,
			},
		},
	}
}

func (environProviderCredentials) DetectCredentials() (*cloud.Credential, error) {
	return nil, errors.NotFoundf("credentials")
}
