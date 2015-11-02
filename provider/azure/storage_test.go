// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure_test

import (
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/to"
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/instance"
	"github.com/juju/juju/provider/azure"
	"github.com/juju/juju/provider/azure/internal/azuretesting"
	"github.com/juju/juju/storage"
	"github.com/juju/juju/testing"
)

type storageSuite struct {
	testing.BaseSuite

	provider storage.Provider
	requests []*http.Request
	sender   azuretesting.Senders
}

var _ = gc.Suite(&storageSuite{})

func (s *storageSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	_, s.provider = newProviders(c, &s.sender, &s.requests)
	s.sender = nil
}

func (s *storageSuite) volumeSource(c *gc.C, attrs ...testing.Attrs) storage.VolumeSource {
	storageConfig, err := storage.NewConfig("azure", "azure", nil)
	c.Assert(err, jc.ErrorIsNil)

	attrs = append([]testing.Attrs{{"storage-account": fakeStorageAccount}}, attrs...)
	cfg := makeTestEnvironConfig(c, attrs...)
	volumeSource, err := s.provider.VolumeSource(cfg, storageConfig)
	c.Assert(err, jc.ErrorIsNil)

	// Force an explicit refresh of the access token, so it isn't done
	// implicitly during the tests.
	s.sender = azuretesting.Senders{tokenRefreshSender()}
	err = azure.ForceVolumeSourceTokenRefresh(volumeSource)
	c.Assert(err, jc.ErrorIsNil)
	return volumeSource
}

func (s *storageSuite) TestVolumeSource(c *gc.C) {
	s.volumeSource(c)
}

func (s *storageSuite) TestSupports(c *gc.C) {
	c.Assert(s.provider.Supports(storage.StorageKindBlock), jc.IsTrue)
	c.Assert(s.provider.Supports(storage.StorageKindFilesystem), jc.IsFalse)
}

func (s *storageSuite) TestDynamic(c *gc.C) {
	c.Assert(s.provider.Dynamic(), jc.IsTrue)
}

func (s *storageSuite) TestCreateVolumes(c *gc.C) {
	// machine-1 has a single data disk with LUN 0.
	machine1DataDisks := []compute.DataDisk{{Lun: to.IntPtr(0)}}
	// machine-2 has 32 data disks; no LUNs free.
	machine2DataDisks := make([]compute.DataDisk, 32)
	for i := range machine2DataDisks {
		machine2DataDisks[i].Lun = to.IntPtr(i)
	}

	// volume-0 and volume-2 are attached to machine-0
	// volume-1 is attached to machine-1
	// volume-3 is attached to machine-42, but machine-42 is missing
	// volume-42 is attached to machine-2, but machine-2 has no free LUNs
	makeVolumeParams := func(volume, machine string, size uint64) storage.VolumeParams {
		return storage.VolumeParams{
			Tag:      names.NewVolumeTag(volume),
			Size:     size,
			Provider: "azure",
			Attachment: &storage.VolumeAttachmentParams{
				AttachmentParams: storage.AttachmentParams{
					Provider:   "azure",
					Machine:    names.NewMachineTag(machine),
					InstanceId: instance.Id("machine-" + machine),
				},
				Volume: names.NewVolumeTag(volume),
			},
		}
	}
	params := []storage.VolumeParams{
		makeVolumeParams("0", "0", 1),
		makeVolumeParams("1", "1", 1025),
		makeVolumeParams("2", "0", 1024),
		makeVolumeParams("3", "42", 40),
		makeVolumeParams("42", "2", 50),
	}

	virtualMachines := []compute.VirtualMachine{{
		Name: to.StringPtr("machine-0"),
		Properties: &compute.VirtualMachineProperties{
			StorageProfile: &compute.StorageProfile{},
		},
	}, {
		Name: to.StringPtr("machine-1"),
		Properties: &compute.VirtualMachineProperties{
			StorageProfile: &compute.StorageProfile{DataDisks: &machine1DataDisks},
		},
	}, {
		Name: to.StringPtr("machine-2"),
		Properties: &compute.VirtualMachineProperties{
			StorageProfile: &compute.StorageProfile{DataDisks: &machine2DataDisks},
		},
	}}

	// There should be a single instance listing API call,
	// and one update per modified instance.
	virtualMachinesSender := azuretesting.NewSenderWithValue(compute.VirtualMachineListResult{
		Value: &virtualMachines,
	})
	virtualMachinesSender.PathPattern = `.*/Microsoft\.Compute/virtualMachines`
	updateVirtualMachine0Sender := azuretesting.NewSenderWithValue(&compute.VirtualMachine{})
	updateVirtualMachine0Sender.PathPattern = `.*/Microsoft\.Compute/virtualMachines/machine-0`
	updateVirtualMachine1Sender := azuretesting.NewSenderWithValue(&compute.VirtualMachine{})
	updateVirtualMachine1Sender.PathPattern = `.*/Microsoft\.Compute/virtualMachines/machine-1`
	volumeSource := s.volumeSource(c)
	s.sender = azuretesting.Senders{
		virtualMachinesSender,
		updateVirtualMachine0Sender,
		updateVirtualMachine1Sender,
	}

	results, err := volumeSource.CreateVolumes(params)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(results, gc.HasLen, len(params))

	c.Check(results[0].Error, jc.ErrorIsNil)
	c.Check(results[1].Error, jc.ErrorIsNil)
	c.Check(results[2].Error, jc.ErrorIsNil)
	c.Check(results[3].Error, gc.ErrorMatches, "instance machine-42 not found")
	c.Check(results[4].Error, gc.ErrorMatches, "choosing LUN: all LUNs are in use")

	// Validate HTTP request bodies.
	c.Assert(s.requests, gc.HasLen, 3)
	c.Assert(s.requests[0].Method, gc.Equals, "GET") // list virtual machines
	c.Assert(s.requests[1].Method, gc.Equals, "PUT") // update machine-0
	c.Assert(s.requests[2].Method, gc.Equals, "PUT") // update machine-1

	machine0DataDisks := []compute.DataDisk{{
		Lun:        to.IntPtr(0),
		DiskSizeGB: to.IntPtr(1),
		Name:       to.StringPtr("volume-0"),
		Vhd: &compute.VirtualHardDisk{URI: to.StringPtr(fmt.Sprintf(
			"https://%s.blob.core.windows.net/datavhds/volume-0.vhd",
			fakeStorageAccount,
		))},
		Caching:      compute.ReadWrite,
		CreateOption: compute.Empty,
	}, {
		Lun:        to.IntPtr(1),
		DiskSizeGB: to.IntPtr(1),
		Name:       to.StringPtr("volume-2"),
		Vhd: &compute.VirtualHardDisk{URI: to.StringPtr(fmt.Sprintf(
			"https://%s.blob.core.windows.net/datavhds/volume-2.vhd",
			fakeStorageAccount,
		))},
		Caching:      compute.ReadWrite,
		CreateOption: compute.Empty,
	}}
	virtualMachines[0].Properties.StorageProfile.DataDisks = &machine0DataDisks
	assertRequestBody(c, s.requests[1], &virtualMachines[0])

	machine1DataDisks = append(machine1DataDisks, compute.DataDisk{
		Lun:        to.IntPtr(1),
		DiskSizeGB: to.IntPtr(2),
		Name:       to.StringPtr("volume-1"),
		Vhd: &compute.VirtualHardDisk{URI: to.StringPtr(fmt.Sprintf(
			"https://%s.blob.core.windows.net/datavhds/volume-1.vhd",
			fakeStorageAccount,
		))},
		Caching:      compute.ReadWrite,
		CreateOption: compute.Empty,
	})
	assertRequestBody(c, s.requests[2], &virtualMachines[1])
}
