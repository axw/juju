// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure_test

import (
	"fmt"
	"time"

	autorestazure "github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/to"
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"
	"github.com/Azure/azure-sdk-for-go/arm/storage"
	"github.com/juju/names"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils/arch"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/api"
	"github.com/juju/juju/cloudconfig/instancecfg"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/imagemetadata"
	"github.com/juju/juju/mongo"
	"github.com/juju/juju/provider/azure"
	"github.com/juju/juju/provider/azure/internal/azuretesting"
	"github.com/juju/juju/testing"
	"github.com/juju/juju/tools"
	"github.com/juju/juju/version"
)

type environSuite struct {
	testing.BaseSuite

	provider environs.EnvironProvider

	vmSizes              *compute.VirtualMachineSizeListResult
	storageAccount       *storage.Account
	flatSubnet           *network.Subnet
	ubuntuServerSKUs     []compute.VirtualMachineImageResource
	publicIPAddress      *network.PublicIPAddress
	oldNetworkInterfaces *network.InterfaceListResult
	newNetworkInterface  *network.Interface
	jujuAvailabilitySet  *compute.AvailabilitySet
	virtualMachine       *compute.VirtualMachine

	sender azuretesting.Senders
}

var _ = gc.Suite(&environSuite{})

func (s *environSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	s.provider = newEnvironProviderWithSender(c, &s.sender)
	s.sender = nil

	vmSizes := []compute.VirtualMachineSize{{
		Name:                 to.StringPtr("Standard_D1"),
		NumberOfCores:        to.IntPtr(1),
		OsDiskSizeInMB:       to.IntPtr(1047552),
		ResourceDiskSizeInMB: to.IntPtr(51200),
		MemoryInMB:           to.IntPtr(3584),
		MaxDataDiskCount:     to.IntPtr(2),
	}}
	s.vmSizes = &compute.VirtualMachineSizeListResult{Value: &vmSizes}

	s.storageAccount = &storage.Account{
		Name: to.StringPtr("my-storage-account"),
		Type: to.StringPtr("Standard_LRS"),
		Properties: &storage.AccountProperties{
			PrimaryEndpoints: &storage.Endpoints{
				Blob: to.StringPtr("http://mrblobby.example.com/"),
			},
		},
	}

	s.flatSubnet = &network.Subnet{
		ID:   to.StringPtr("subnet-id"),
		Name: to.StringPtr("vnet-flat-subnet"),
		Properties: &network.SubnetPropertiesFormat{
			AddressPrefix: to.StringPtr("10.0.0.0/8"),
		},
	}

	s.ubuntuServerSKUs = []compute.VirtualMachineImageResource{
		{Name: to.StringPtr("12.04-LTS")},
		{Name: to.StringPtr("12.10")},
		{Name: to.StringPtr("14.04-LTS")},
		{Name: to.StringPtr("15.04")},
		{Name: to.StringPtr("15.10")},
	}

	s.publicIPAddress = &network.PublicIPAddress{
		ID:   to.StringPtr("public-ip-id"),
		Name: to.StringPtr("juju-machine-1-public-ip"),
		Properties: &network.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: network.Dynamic,
			IPAddress:                to.StringPtr("1.2.3.4"),
		},
	}

	// Existing IPs/NICs. These are the results of querying NICs so we
	// can tell which IP to allocate.
	oldIPConfigurations := []network.InterfaceIPConfiguration{{
		ID:   to.StringPtr("ip-configuration-0-id"),
		Name: to.StringPtr("ip-configuration-0"),
		Properties: &network.InterfaceIPConfigurationPropertiesFormat{
			PrivateIPAddress:          to.StringPtr("10.0.0.4"),
			PrivateIPAllocationMethod: network.Static,
			Subnet: &network.SubResource{ID: s.flatSubnet.ID},
		},
	}}
	oldNetworkInterfaces := []network.Interface{{
		ID:   to.StringPtr("network-interface-0-id"),
		Name: to.StringPtr("network-interface-0"),
		Properties: &network.InterfacePropertiesFormat{
			IPConfigurations: &oldIPConfigurations,
			Primary:          to.BoolPtr(true),
		},
	}}
	s.oldNetworkInterfaces = &network.InterfaceListResult{
		Value: &oldNetworkInterfaces,
	}

	// The newly created IP/NIC.
	newIPConfigurations := []network.InterfaceIPConfiguration{{
		ID:   to.StringPtr("ip-configuration-1-id"),
		Name: to.StringPtr("ip-configuration-1"),
		Properties: &network.InterfaceIPConfigurationPropertiesFormat{
			PrivateIPAddress:          to.StringPtr("10.0.0.5"),
			PrivateIPAllocationMethod: network.Static,
			Subnet:          &network.SubResource{ID: s.flatSubnet.ID},
			PublicIPAddress: &network.SubResource{ID: s.publicIPAddress.ID},
		},
	}}
	s.newNetworkInterface = &network.Interface{
		ID:   to.StringPtr("network-interface-1-id"),
		Name: to.StringPtr("network-interface-1"),
		Properties: &network.InterfacePropertiesFormat{
			IPConfigurations: &newIPConfigurations,
			Primary:          to.BoolPtr(true),
		},
	}

	s.jujuAvailabilitySet = &compute.AvailabilitySet{
		ID:   to.StringPtr("juju-availability-set-id"),
		Name: to.StringPtr("juju"),
	}

	s.virtualMachine = &compute.VirtualMachine{
		ID:   to.StringPtr("juju-machine-1-id"),
		Name: to.StringPtr("juju-machine-1"),
		Properties: &compute.VirtualMachineProperties{
			HardwareProfile: &compute.HardwareProfile{
				VMSize: "Standard_D1",
			},
			StorageProfile: &compute.StorageProfile{
				ImageReference: &compute.ImageReference{
					Publisher: to.StringPtr("Canonical"),
					Offer:     to.StringPtr("UbuntuServer"),
					Sku:       to.StringPtr("12.10"),
					Version:   to.StringPtr("latest"),
				},
				OsDisk: &compute.OSDisk{
					Name:         to.StringPtr("juju-machine-1-osdisk"),
					CreateOption: compute.FromImage,
					Caching:      compute.ReadWrite,
					Vhd: &compute.VirtualHardDisk{
						URI: to.StringPtr(
							"http://mrblobby.example.com/vhds/juju-machine-1-osdisk.vhd",
						),
					},
				},
			},
			OsProfile: &compute.OSProfile{
				ComputerName:  to.StringPtr("juju-machine-1"),
				CustomData:    to.StringPtr("<juju-goes-here>"),
				AdminUsername: to.StringPtr("ubuntu"),
				LinuxConfiguration: &compute.LinuxConfiguration{
					DisablePasswordAuthentication: to.BoolPtr(true),
				},
			},
			NetworkProfile:    &compute.NetworkProfile{},
			AvailabilitySet:   &compute.SubResource{ID: s.jujuAvailabilitySet.ID},
			ProvisioningState: to.StringPtr("Successful"),
		},
	}
}

func (s *environSuite) openEnviron(c *gc.C, attrs ...testing.Attrs) environs.Environ {
	// Opening the environment should not incur network communication,
	// so we don't set s.sender until after opening.
	cfg := makeTestEnvironConfig(c, attrs...)
	env, err := s.provider.Open(cfg)
	c.Assert(err, jc.ErrorIsNil)

	// Force an explicit refresh of the access token, so it isn't done
	// implicitly during the tests.
	tokenRefreshSender := azuretesting.NewSenderWithValue(&autorestazure.Token{
		AccessToken: "access-token",
		ExpiresOn:   fmt.Sprint(time.Now().Add(time.Hour).Unix()),
		Type:        "Bearer",
	})
	tokenRefreshSender.PathPattern = ".*/oauth2/token"
	s.sender = azuretesting.Senders{tokenRefreshSender}
	err = azure.ForceTokenRefresh(env)
	c.Assert(err, jc.ErrorIsNil)
	return env
}

func (s *environSuite) startInstanceSenders() azuretesting.Senders {
	sender := func(pattern string, v interface{}) *azuretesting.MockSender {
		sender := azuretesting.NewSenderWithValue(v)
		sender.PathPattern = pattern
		return sender
	}
	return azuretesting.Senders{
		sender(".*/vmSizes", s.vmSizes),
		sender(".*/storageAccounts", s.storageAccount),
		sender(".*/subnets/vnet-flat-subnet", s.flatSubnet),
		sender(".*/Canonical/.*/UbuntuServer/skus", s.ubuntuServerSKUs),
		sender(".*/publicIPAddresses/juju-machine-1-public-ip", s.publicIPAddress),
		sender(".*/networkInterfaces", s.oldNetworkInterfaces),
		sender(".*/networkInterfaces/juju-machine-1-primary", s.newNetworkInterface),
		sender(".*/availabilitySets/juju", s.jujuAvailabilitySet),
		sender(".*/virtualMachines/juju-machine-1", s.virtualMachine),
	}
}

func (s *environSuite) TestOpen(c *gc.C) {
	cfg := makeTestEnvironConfig(c)
	env, err := s.provider.Open(cfg)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(env, gc.NotNil)
}

func (s *environSuite) TestStartInstance(c *gc.C) {
	env := s.openEnviron(c)
	s.sender = s.startInstanceSenders()
	result, err := env.StartInstance(makeStartInstanceParams(c, "quantal"))
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(result, gc.NotNil)

	// TODO(axw) validate result
	// TODO(axw) validate HTTP requests
}

func makeStartInstanceParams(c *gc.C, series string) environs.StartInstanceParams {
	machineTag := names.NewMachineTag("1")
	stateInfo := &mongo.MongoInfo{
		Info: mongo.Info{
			CACert: testing.CACert,
			Addrs:  []string{"localhost:123"},
		},
		Password: "password",
		Tag:      machineTag,
	}
	apiInfo := &api.Info{
		Addrs:      []string{"localhost:246"},
		CACert:     testing.CACert,
		Password:   "admin",
		Tag:        machineTag,
		EnvironTag: testing.EnvironmentTag,
	}

	const secureServerConnections = true
	var networks []string
	icfg, err := instancecfg.NewInstanceConfig(
		machineTag.Id(), "yanonce", imagemetadata.ReleasedStream,
		series, secureServerConnections, networks, stateInfo, apiInfo,
	)
	c.Assert(err, jc.ErrorIsNil)

	var toolsVersion version.Binary
	toolsVersion.Number = version.MustParse("1.26.0")
	toolsVersion.Arch = arch.AMD64
	toolsVersion.Series = series

	return environs.StartInstanceParams{
		Tools: tools.List{{
			Version: toolsVersion,
			URL:     fmt.Sprintf("http://example.com/tools/juju-%s.tgz", toolsVersion),
			SHA256:  "1234567890abcdef",
			Size:    1024,
		}},
		InstanceConfig: icfg,
	}
}
