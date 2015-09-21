// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import "github.com/juju/juju/environs"

const (
	providerType = "azure"
)

func init() {
	environs.RegisterProvider(providerType, azureEnvironProvider{})
}
