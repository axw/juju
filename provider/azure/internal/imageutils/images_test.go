// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package imageutils_test

import (
	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/mocks"
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils/arch"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/provider/azure/internal/imageutils"
	"github.com/juju/juju/testing"
)

type imageutilsSuite struct {
	testing.BaseSuite

	mockSender *mocks.Sender
	client     compute.VirtualMachineImagesClient
}

var _ = gc.Suite(&imageutilsSuite{})

func (s *imageutilsSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	s.mockSender = mocks.NewSender()
	s.client.Sender = s.mockSender
}

func (s *imageutilsSuite) TestSeriesImage(c *gc.C) {
	s.mockSender.EmitContent(
		`[{"name": "14.04.3"}, {"name": "14.04.1-LTS"}, {"name": "12.04.5"}]`,
	)
	image, err := imageutils.SeriesImage("trusty", "released", "westus", s.client)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(image, gc.NotNil)
	c.Assert(image, jc.DeepEquals, &instances.Image{
		Id:       "Canonical:UbuntuServer:14.04.3:current",
		Arch:     arch.AMD64,
		VirtType: "Hyper-V",
	})
}

func (s *imageutilsSuite) TestSeriesImageInvalidSKU(c *gc.C) {
	s.mockSender.EmitContent(
		`[{"name": "12.04.invalid"}, {"name": "12.04.5-LTS"}]`,
	)
	image, err := imageutils.SeriesImage("precise", "released", "westus", s.client)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(image, gc.NotNil)
	c.Assert(image, jc.DeepEquals, &instances.Image{
		Id:       "Canonical:UbuntuServer:12.04.5-LTS:current",
		Arch:     arch.AMD64,
		VirtType: "Hyper-V",
	})
}

func (s *imageutilsSuite) TestSeriesImageWindows(c *gc.C) {
	s.assertImageId(c, "win2012r2", "daily", "MicrosoftWindowsServer:WindowsServer:2012-R2-Datacenter:current")
	s.assertImageId(c, "win2012", "daily", "MicrosoftWindowsServer:WindowsServer:2012-Datacenter:current")
}

func (s *imageutilsSuite) TestSeriesImageCentOS(c *gc.C) {
	_, err := imageutils.SeriesImage("centos7", "released", "westus", s.client)
	c.Assert(err, gc.ErrorMatches, "deploying CentOS not supported")
}

func (s *imageutilsSuite) TestSeriesImageStream(c *gc.C) {
	s.mockSender.EmitContent(`[{"name": "14.04.2"}, {"name": "14.04.3-DAILY"}, {"name": "14.04.1-LTS"}]`)
	s.assertImageId(c, "trusty", "daily", "Canonical:UbuntuServer:14.04.3-DAILY:current")
	s.assertImageId(c, "trusty", "released", "Canonical:UbuntuServer:14.04.2:current")
}

func (s *imageutilsSuite) TestSeriesImageNotFound(c *gc.C) {
	s.mockSender.EmitContent(`[]`)
	image, err := imageutils.SeriesImage("trusty", "released", "westus", s.client)
	c.Assert(err, gc.ErrorMatches, "selecting SKU for trusty: Ubuntu SKUs not found")
	c.Assert(image, gc.IsNil)
}

func (s *imageutilsSuite) TestSeriesImageStreamNotFound(c *gc.C) {
	s.mockSender.EmitContent(`[{"name": "14.04.3-beta1"}]`)
	_, err := imageutils.SeriesImage("trusty", "whatever", "westus", s.client)
	c.Assert(err, gc.ErrorMatches, "selecting SKU for trusty: Ubuntu SKUs for whatever stream not found")
}

func (s *imageutilsSuite) assertImageId(c *gc.C, series, stream, id string) {
	image, err := imageutils.SeriesImage(series, stream, "westus", s.client)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(image.Id, gc.Equals, id)
}
