// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import "github.com/juju/juju/environs"

const (
	providerType = "azure"
)

func init() {
	provider, err := NewEnvironProvider(EnvironProviderConfig{})
	if err != nil {
		panic(err)
	}
	environs.RegisterProvider(providerType, provider)
	// TODO(axw) register an image metadata data source that queries
	// the Azure image registry, and introduce a way to disable the
	// common simplestreams source.
}
