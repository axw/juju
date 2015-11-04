// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure_test

import (
	"net/http"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/to"
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/instance"
	jujunetwork "github.com/juju/juju/network"
	"github.com/juju/juju/provider/azure/internal/azuretesting"
	"github.com/juju/juju/testing"
)

type instanceSuite struct {
	testing.BaseSuite

	storageClient              azuretesting.MockStorageClient
	provider                   environs.EnvironProvider
	requests                   []*http.Request
	sender                     azuretesting.Senders
	env                        environs.Environ
	virtualMachine             compute.VirtualMachine
	networkInterfaces          []network.Interface
	networkInterfaceReferences []compute.NetworkInterfaceReference
	publicIPAddresses          []network.PublicIPAddress
}

var _ = gc.Suite(&instanceSuite{})

func (s *instanceSuite) SetUpTest(c *gc.C) {
	s.BaseSuite.SetUpTest(c)
	s.storageClient = azuretesting.MockStorageClient{}
	s.provider, _ = newProviders(c, &s.sender, s.storageClient.NewClient, &s.requests)
	s.env = openEnviron(c, s.provider, &s.sender)
	s.sender = nil
	s.requests = nil
	s.networkInterfaces = nil
	s.networkInterfaceReferences = nil
	s.publicIPAddresses = nil
	s.virtualMachine = compute.VirtualMachine{
		Name: to.StringPtr("machine-0"),
		Properties: &compute.VirtualMachineProperties{
			NetworkProfile: &compute.NetworkProfile{
				NetworkInterfaces: &s.networkInterfaceReferences,
			},
			ProvisioningState: to.StringPtr("Successful"),
		},
	}
}

func (s *instanceSuite) getInstance(c *gc.C) instance.Instance {
	virtualMachines := []compute.VirtualMachine{s.virtualMachine}
	vmsSender := azuretesting.NewSenderWithValue(&compute.VirtualMachineListResult{
		Value: &virtualMachines,
	})
	vmsSender.PathPattern = ".*/virtualMachines"

	nicsSender := azuretesting.NewSenderWithValue(&network.InterfaceListResult{
		Value: &s.networkInterfaces,
	})
	nicsSender.PathPattern = ".*/networkInterfaces"

	pipsSender := azuretesting.NewSenderWithValue(&network.PublicIPAddressListResult{
		Value: &s.publicIPAddresses,
	})
	pipsSender.PathPattern = ".*/publicIPAddresses"

	s.sender = azuretesting.Senders{vmsSender, nicsSender, pipsSender}

	instances, err := s.env.AllInstances()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(instances, gc.HasLen, 1)
	return instances[0]
}

func (s *instanceSuite) TestInstanceStatus(c *gc.C) {
	inst := s.getInstance(c)
	c.Assert(inst.Status(), gc.Equals, "Successful")
}

func (s *instanceSuite) TestInstanceStatusNilProvisioningState(c *gc.C) {
	s.virtualMachine.Properties.ProvisioningState = nil
	inst := s.getInstance(c)
	c.Assert(inst.Status(), gc.Equals, "")
}

func (s *instanceSuite) TestInstanceAddressesEmpty(c *gc.C) {
	addresses, err := s.getInstance(c).Addresses()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(addresses, gc.HasLen, 0)
}

func (s *instanceSuite) TestInstanceAddressesDanglingNICs(c *gc.C) {
	// References for which there are no NICs
	s.networkInterfaceReferences = []compute.NetworkInterfaceReference{
		{ID: to.StringPtr("nic-0")}, {ID: to.StringPtr("nic-1")},
	}
	addresses, err := s.getInstance(c).Addresses()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(addresses, gc.HasLen, 0)
}

func (s *instanceSuite) TestInstanceAddresses(c *gc.C) {
	s.networkInterfaceReferences = []compute.NetworkInterfaceReference{
		{ID: to.StringPtr("nic-0")}, {ID: to.StringPtr("nic-1")},
	}
	nic0IPConfigurations := []network.InterfaceIPConfiguration{{
		Properties: &network.InterfaceIPConfigurationPropertiesFormat{
			PrivateIPAddress: to.StringPtr("10.0.0.4"),
		},
	}, {
		Properties: &network.InterfaceIPConfigurationPropertiesFormat{
			PrivateIPAddress: to.StringPtr("10.0.0.5"),
			PublicIPAddress: &network.SubResource{
				to.StringPtr("pip-1"),
			},
		},
	}}
	nic1IPConfigurations := []network.InterfaceIPConfiguration{{
		// No private IP or public IP.
		Properties: &network.InterfaceIPConfigurationPropertiesFormat{},
	}, {
		// Public IP only.
		Properties: &network.InterfaceIPConfigurationPropertiesFormat{
			PublicIPAddress: &network.SubResource{
				to.StringPtr("pip-0"),
			},
		},
	}}
	s.networkInterfaces = []network.Interface{{
		ID: to.StringPtr("nic-0"),
		Properties: &network.InterfacePropertiesFormat{
			IPConfigurations: &nic0IPConfigurations,
		},
	}, {
		ID: to.StringPtr("nic-1"),
		Properties: &network.InterfacePropertiesFormat{
			IPConfigurations: &nic1IPConfigurations,
		},
	}, {
		// unrelated NIC
		ID: to.StringPtr("nic-2"),
	}}
	s.publicIPAddresses = []network.PublicIPAddress{{
		ID: to.StringPtr("pip-0"),
		Properties: &network.PublicIPAddressPropertiesFormat{
			IPAddress: to.StringPtr("1.2.3.4"),
		},
	}, {
		ID: to.StringPtr("pip-1"),
		Properties: &network.PublicIPAddressPropertiesFormat{
			IPAddress: to.StringPtr("1.2.3.5"),
		},
	}, {
		// unrelated PIP
		ID: to.StringPtr("pip-2"),
		Properties: &network.PublicIPAddressPropertiesFormat{
			IPAddress: to.StringPtr("1.2.3.6"),
		},
	}}
	addresses, err := s.getInstance(c).Addresses()
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(addresses, jc.DeepEquals, jujunetwork.NewAddresses(
		"10.0.0.4", "10.0.0.5", "1.2.3.5", "1.2.3.4",
	))
}
