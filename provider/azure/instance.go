// Copyright 2011, 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"
	"github.com/davecgh/go-spew/spew"

	"github.com/juju/errors"
	"github.com/juju/juju/instance"
	jujunetwork "github.com/juju/juju/network"
	"github.com/juju/names"
)

const AzureDomainName = "cloudapp.net"

type azureInstance struct {
	compute.VirtualMachine
	networkInterfaces []network.NetworkInterface
	publicIpAddresses []network.PublicIpAddress
	env               *azureEnviron
}

// azureInstance implements Instance.
var _ instance.Instance = (*azureInstance)(nil)

// Id is specified in the Instance interface.
func (inst *azureInstance) Id() instance.Id {
	// Note: we use Name and not Id, since all VM operations are in
	// terms of the VM name (qualified by resource group). The ID is
	// an internal detail.
	return instance.Id(inst.VirtualMachine.Name)
}

// Status is specified in the Instance interface.
func (inst *azureInstance) Status() string {
	// TODO(axw) is this the right thing to use?
	return inst.Properties.ProvisioningState
}

// Refresh is specified in the Instance interface.
func (inst *azureInstance) Refresh() error {
	// TODO(axw) remove the Instance.Refresh method. Callers can just
	// use environs.Instances([]instance.Id{inst.Id()}) instead.
	instances, err := inst.env.Instances([]instance.Id{inst.Id()})
	if err != nil {
		return errors.Annotatef(err, "refreshing instance %q", inst.Id())
	}
	*inst = *instances[0].(*azureInstance)
	// TODO(axw) when querying instances, don't query VM directly,
	//           query resources with tags, use "type" field to
	//           decode into the right type.
	return errors.Trace(inst.refreshAddresses())
}

func (inst *azureInstance) refreshAddresses() error {
	inst.env.mu.Lock()
	nicClient := network.NetworkInterfacesClient{inst.env.network}
	pipClient := network.PublicIpAddressesClient{inst.env.network}
	resourceGroup := inst.env.resourceGroup
	inst.env.mu.Unlock()

	// Can't use Get() with an Id, which is all we have in
	// the VirtualMachine. When we list generic resources
	// this will be a non-issue.
	nicsResult, err := nicClient.List(resourceGroup)
	if err != nil {
		return errors.Annotate(err, "listing network interfaces")
	}
	nicsById := make(map[string]network.NetworkInterface)
	pipsById := make(map[string]*network.PublicIpAddress)
	for _, nic := range nicsResult.Value {
		nicsById[nic.Id] = nic
	}
	networkInterfaces := make([]network.NetworkInterface, 0, len(inst.Properties.NetworkProfile.NetworkInterfaces))
	for _, nicRef := range inst.Properties.NetworkProfile.NetworkInterfaces {
		nic, ok := nicsById[nicRef.Id]
		if !ok {
			logger.Warningf("could not find NIC with ID %q", nicRef.Id)
			continue
		}
		for _, ipConfiguration := range nic.Properties.IpConfigurations {
			if ipConfiguration.Properties.PublicIPAddress.Id == "" {
				continue
			}
			pipsById[ipConfiguration.Properties.PublicIPAddress.Id] = nil
		}
		networkInterfaces = append(networkInterfaces, nic)
	}
	inst.networkInterfaces = networkInterfaces

	pipsResult, err := pipClient.List(resourceGroup)
	if err != nil {
		return errors.Annotate(err, "listing public IP addresses")
	}
	publicIpAddresses := make([]network.PublicIpAddress, 0, len(pipsById))
	for _, pip := range pipsResult.Value {
		if _, ok := pipsById[pip.Id]; !ok {
			continue
		}
		publicIpAddresses = append(publicIpAddresses, pip)
	}
	inst.publicIpAddresses = publicIpAddresses

	return nil
}

// Addresses is specified in the Instance interface.
func (inst *azureInstance) Addresses() ([]jujunetwork.Address, error) {
	addresses := make([]jujunetwork.Address, 0, len(inst.networkInterfaces)+len(inst.publicIpAddresses))
	for _, nic := range inst.networkInterfaces {
		for _, ipConfiguration := range nic.Properties.IpConfigurations {
			privateIpAddress := ipConfiguration.Properties.PrivateIPAddress
			addresses = append(addresses,
				jujunetwork.NewScopedAddress(
					privateIpAddress, jujunetwork.ScopeCloudLocal,
				),
			)
		}
	}
	for _, pip := range inst.publicIpAddresses {
		addresses = append(addresses,
			jujunetwork.NewScopedAddress(
				pip.Properties.IpAddress, jujunetwork.ScopePublic,
			),
		)
	}
	logger.Debugf("addresses: %+v", addresses)
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
	nsgClient := network.NetworkSecurityGroupsClient{inst.env.network}
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
