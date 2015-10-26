// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package imageutils_test

import (
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/go-autorest/autorest/to"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/provider/azure/internal/imageutils"
	"github.com/juju/juju/testing"
)

type imageutilsSuite struct {
	testing.BaseSuite
}

var _ = gc.Suite(&imageutilsSuite{})

func (*imageutilsSuite) TestImageReferenceUbuntuImageId(c *gc.C) {
	testImageReference(c,
		"b39f27a8b8c64d52b05eac6a62ebad85__Ubuntu-15_04-amd64-server-20151015-en-us-30GB",
		"Canonical", "UbuntuServer", "15.04", "15.04.20151015",
	)
	testImageReference(c,
		"b39f27a8b8c64d52b05eac6a62ebad85__Ubuntu_DAILY_BUILD-15_04-amd64-server-20151015-en-us-30GB",
		"Canonical", "UbuntuServer", "15.04-DAILY", "15.04.20151015",
	)
	testImageReference(c,
		"b39f27a8b8c64d52b05eac6a62ebad85__Ubuntu-14_04_1-LTS-amd64-server-20151015-en-us-30GB",
		"Canonical", "UbuntuServer", "14.04.1-LTS", "14.04.20151015",
	)
	testImageReference(c,
		"b39f27a8b8c64d52b05eac6a62ebad85__Ubuntu_DAILY_BUILD-14_04_1-LTS-amd64-server-20151015-en-us-30GB",
		"Canonical", "UbuntuServer", "14.04.1-DAILY-LTS", "14.04.20151015",
	)
}

func (*imageutilsSuite) TestImageReferenceURN(c *gc.C) {
	testImageReference(c,
		"Canonical:UbuntuServer:14.04.3-DAILY-LTS:14.04.201509280",
		"Canonical", "UbuntuServer", "14.04.3-DAILY-LTS", "14.04.201509280",
	)
	testImageReference(c,
		"MicrosoftWindowsServer:WindowsServer:2012-R2-Datacenter:current",
		"MicrosoftWindowsServer", "WindowsServer", "2012-R2-Datacenter", "current",
	)
}

func testImageReference(c *gc.C, id, publisher, offer, sku, version string) {
	ref, err := imageutils.ImageReference(instances.Image{Id: id})
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(ref, gc.NotNil)
	c.Assert(ref, jc.DeepEquals, &compute.ImageReference{
		Publisher: to.StringPtr(publisher),
		Offer:     to.StringPtr(offer),
		Sku:       to.StringPtr(sku),
		Version:   to.StringPtr(version),
	})
}
