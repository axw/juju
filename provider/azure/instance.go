// Copyright 2011, 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/davecgh/go-spew/spew"

	"github.com/juju/errors"
	"github.com/juju/juju/instance"
	jujunetwork "github.com/juju/juju/network"
	"github.com/juju/names"
)

const AzureDomainName = "cloudapp.net"

type azureInstance struct {
	compute.VirtualMachine
	networkInterfaces []network.Interface
	publicIPAddresses []network.PublicIPAddress
	env               *azureEnviron
}

// azureInstance implements Instance.
var _ instance.Instance = (*azureInstance)(nil)

// Id is specified in the Instance interface.
func (inst *azureInstance) Id() instance.Id {
	// Note: we use Name and not Id, since all VM operations are in
	// terms of the VM name (qualified by resource group). The ID is
	// an internal detail.
	return instance.Id(to.String(inst.VirtualMachine.Name))
}

// Status is specified in the Instance interface.
func (inst *azureInstance) Status() string {
	// TODO(axw) is this the right thing to use?
	return to.String(inst.Properties.ProvisioningState)
}

func (inst *azureInstance) refreshAddresses() error {
	inst.env.mu.Lock()
	nicClient := network.InterfacesClient{inst.env.network}
	pipClient := network.PublicIPAddressesClient{inst.env.network}
	resourceGroup := inst.env.resourceGroup
	inst.env.mu.Unlock()

	// Can't use Get() with an Id, which is all we have in
	// the VirtualMachine. When we list generic resources
	// this will be a non-issue.
	nicsResult, err := nicClient.List(resourceGroup)
	if err != nil {
		return errors.Annotate(err, "listing network interfaces")
	}
	nicsById := make(map[string]network.Interface)
	pipsById := make(map[string]*network.PublicIPAddress)
	for _, nic := range *nicsResult.Value {
		nicsById[to.String(nic.ID)] = nic
	}
	if inst.Properties.NetworkProfile.NetworkInterfaces != nil {
		networkInterfaces := make([]network.Interface, 0, len(*inst.Properties.NetworkProfile.NetworkInterfaces))
		for _, nicRef := range *inst.Properties.NetworkProfile.NetworkInterfaces {
			nic, ok := nicsById[to.String(nicRef.ID)]
			if !ok {
				logger.Warningf("could not find NIC with ID %q", to.String(nicRef.ID))
				continue
			}
			if nic.Properties.IPConfigurations != nil {
				for _, ipConfiguration := range *nic.Properties.IPConfigurations {
					if ipConfiguration.Properties.PublicIPAddress == nil {
						continue
					}
					pipsById[to.String(ipConfiguration.Properties.PublicIPAddress.ID)] = nil
				}
			}
			networkInterfaces = append(networkInterfaces, nic)
		}
		inst.networkInterfaces = networkInterfaces
	}

	pipsResult, err := pipClient.List(resourceGroup)
	if err != nil {
		return errors.Annotate(err, "listing public IP addresses")
	}
	publicIPAddresses := make([]network.PublicIPAddress, 0, len(pipsById))
	if pipsResult.Value != nil {
		for _, pip := range *pipsResult.Value {
			if _, ok := pipsById[to.String(pip.ID)]; !ok {
				continue
			}
			publicIPAddresses = append(publicIPAddresses, pip)
		}
	}
	inst.publicIPAddresses = publicIPAddresses

	return nil
}

// Addresses is specified in the Instance interface.
func (inst *azureInstance) Addresses() ([]jujunetwork.Address, error) {
	addresses := make([]jujunetwork.Address, 0, len(inst.networkInterfaces)+len(inst.publicIPAddresses))
	for _, nic := range inst.networkInterfaces {
		if nic.Properties.IPConfigurations == nil {
			continue
		}
		for _, ipConfiguration := range *nic.Properties.IPConfigurations {
			privateIpAddress := ipConfiguration.Properties.PrivateIPAddress
			addresses = append(addresses, jujunetwork.NewScopedAddress(
				to.String(privateIpAddress),
				jujunetwork.ScopeCloudLocal,
			))
		}
	}
	for _, pip := range inst.publicIPAddresses {
		addresses = append(addresses, jujunetwork.NewScopedAddress(
			to.String(pip.Properties.IPAddress),
			jujunetwork.ScopePublic,
		))
	}
	return addresses, nil
}

// OpenPorts is specified in the Instance interface.
func (inst *azureInstance) OpenPorts(machineId string, portRange []jujunetwork.PortRange) error {
	// TODO(axw)
	return nil
}

// ClosePorts is specified in the Instance interface.
func (inst *azureInstance) ClosePorts(machineId string, ports []jujunetwork.PortRange) error {
	// TODO(axw)
	return nil
}

// Ports is specified in the Instance interface.
func (inst *azureInstance) Ports(machineId string) (ports []jujunetwork.PortRange, err error) {
	inst.env.mu.Lock()
	nsgClient := network.SecurityGroupsClient{inst.env.network}
	resourceGroup := inst.env.resourceGroup
	inst.env.mu.Unlock()

	nsg, err := nsgClient.Get(resourceGroup, machineSecurityGroupName(machineId))
	if err != nil {
		return nil, errors.Annotate(err, "getting network security group")
	}
	logger.Debugf("%v", spew.Sdump(nsg.Properties))

	// TODO(axw)
	return nil, nil
}

func machineSecurityGroupName(machineId string) string {
	return names.NewMachineTag(machineId).String()
}
