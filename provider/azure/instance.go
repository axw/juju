// Copyright 2011, 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"strings"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/to"
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"

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
		if pip.Properties.IPAddress == nil {
			continue
		}
		addresses = append(addresses, jujunetwork.NewScopedAddress(
			to.String(pip.Properties.IPAddress),
			jujunetwork.ScopePublic,
		))
	}
	return addresses, nil
}

// OpenPorts is specified in the Instance interface.
func (inst *azureInstance) OpenPorts(machineId string, ports []jujunetwork.PortRange) error {
	// TODO(axw)
	logger.Debugf("OpenPorts(%v, %+v)", machineId, ports)
	return nil
}

// ClosePorts is specified in the Instance interface.
func (inst *azureInstance) ClosePorts(machineId string, ports []jujunetwork.PortRange) error {
	// TODO(axw)
	logger.Debugf("ClosePorts(%v, %+v)", machineId, ports)
	return nil
}

// Ports is specified in the Instance interface.
func (inst *azureInstance) Ports(machineId string) (ports []jujunetwork.PortRange, err error) {
	inst.env.mu.Lock()
	nsgClient := network.SecurityGroupsClient{inst.env.network}
	resourceGroup := inst.env.resourceGroup
	inst.env.mu.Unlock()

	nsg, err := nsgClient.Get(resourceGroup, internalSecurityGroupName)
	if err != nil {
		return nil, errors.Annotate(err, "querying network security group")
	}
	if nsg.Properties.SecurityRules == nil {
		return nil, nil
	}

	vmName := resourceName(names.NewMachineTag(machineId))
	prefix := vmName + "-"
	for _, rule := range *nsg.Properties.SecurityRules {
		if rule.Properties.Direction != network.Inbound {
			continue
		}
		if to.Int(rule.Properties.Priority) <= securityRuleInternalMax {
			continue
		}
		if !strings.HasPrefix(to.String(rule.Name), prefix) {
			continue
		}

		var portRange jujunetwork.PortRange
		if *rule.Properties.DestinationPortRange == "*" {
			portRange.FromPort = 0
			portRange.ToPort = 65535
		} else {
			portRange, err = jujunetwork.ParsePortRange(
				*rule.Properties.DestinationPortRange,
			)
			if err != nil {
				return nil, errors.Annotatef(
					err, "parsing port range for security rule %q",
					to.String(rule.Name),
				)
			}
		}

		var protocols []string
		switch rule.Properties.Protocol {
		case network.SecurityRuleProtocolTCP:
			protocols = []string{"tcp"}
		case network.SecurityRuleProtocolUDP:
			protocols = []string{"udp"}
		default:
			protocols = []string{"tcp", "udp"}
		}
		for _, protocol := range protocols {
			portRange.Protocol = protocol
			ports = append(ports, portRange)
		}
	}
	return ports, nil
}
