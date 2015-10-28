// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure_test

import (
	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest"
	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/mocks"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/provider/azure"
)

func newEnvironProvider(c *gc.C) (environs.EnvironProvider, *mocks.Sender) {
	sender := mocks.NewSender()
	return newEnvironProviderWithSender(c, sender), sender
}

func newEnvironProviderWithSender(c *gc.C, sender autorest.Sender) environs.EnvironProvider {
	config := azure.EnvironProviderConfig{sender}
	provider, err := azure.NewEnvironProvider(config)
	c.Assert(err, jc.ErrorIsNil)
	return provider
}
