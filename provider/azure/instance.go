// Copyright 2011, 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"github.com/Azure/azure-sdk-for-go/arm/compute"

	"github.com/juju/juju/instance"
	"github.com/juju/juju/network"
)

const AzureDomainName = "cloudapp.net"

type azureInstance struct {
	compute.VirtualMachine
	//maskStateServerPorts bool
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
	// TODO(axw) remove the Instance.Refresh method.
	return nil
}

// Addresses is specified in the Instance interface.
func (inst *azureInstance) Addresses() ([]network.Address, error) {
	// TODO(axw) have to query VM and then network interfaces.
	return nil, nil
}

// OpenPorts is specified in the Instance interface.
func (inst *azureInstance) OpenPorts(machineId string, portRange []network.PortRange) error {
	// TODO(axw)
	return nil
}

// ClosePorts is specified in the Instance interface.
func (inst *azureInstance) ClosePorts(machineId string, ports []network.PortRange) error {
	// TODO(axw)
	return nil
}

// Ports is specified in the Instance interface.
func (inst *azureInstance) Ports(machineId string) (ports []network.PortRange, err error) {
	// TODO(axw)
	return nil, nil
}
