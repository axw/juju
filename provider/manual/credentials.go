// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package manual

import "github.com/juju/juju/cloud"

type environProviderCredentials struct{}

func (environProviderCredentials) CredentialSchemas() map[cloud.AuthType]cloud.CredentialSchema {
	return nil
}

func (environProviderCredentials) DetectCredentials() (*cloud.Credential, error) {
	emptyCredential := cloud.NewEmptyCredential()
	return &emptyCredential, nil
}
