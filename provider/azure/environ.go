// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"fmt"
	"sync"

	"github.com/azure/azure-sdk-for-go/arm/compute"
	"github.com/juju/errors"
	"github.com/juju/utils/set"

	"github.com/juju/juju/cloudconfig/instancecfg"
	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/environs/simplestreams"
	"github.com/juju/juju/environs/tags"
	"github.com/juju/juju/instance"
	"github.com/juju/juju/juju/arch"
	"github.com/juju/juju/network"
	"github.com/juju/juju/provider/common"
	"github.com/juju/juju/state"
)

const (
	// Address space of the virtual network used by the nodes in this
	// environement, in CIDR notation. This is the network used for
	// machine-to-machine communication.
	networkDefinition = "10.0.0.0/8"

	// stateServerLabel is the label applied to the cloud service created
	// for state servers.
	stateServerLabel = "juju-state-server"
)

type azureEnviron struct {
	mu                 sync.Mutex
	ecfg               *azureEnvironConfig
	storageAccountKey  string
	availableRoleSizes set.Strings
	compute            compute.ComputeManagementClient
}

// azureEnviron implements Environ and HasRegion.
var _ environs.Environ = (*azureEnviron)(nil)
var _ simplestreams.HasRegion = (*azureEnviron)(nil)
var _ state.Prechecker = (*azureEnviron)(nil)

// NewEnviron creates a new azureEnviron.
func NewEnviron(cfg *config.Config) (*azureEnviron, error) {
	var env azureEnviron
	err := env.SetConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &env, nil
}

// Bootstrap is specified in the Environ interface.
func (env *azureEnviron) Bootstrap(ctx environs.BootstrapContext, args environs.BootstrapParams) (arch, series string, _ environs.BootstrapFinalizer, err error) {
	// TODO(axw) create resource group?
	// TODO(axw) create affinity group?
	// TODO(axw) create vnet
	return common.Bootstrap(ctx, env, args)
}

// StateServerInstances is specified in the Environ interface.
func (env *azureEnviron) StateServerInstances() ([]instance.Id, error) {
	instances, err := env.AllInstances()
	if err != nil {
		return nil, err
	}
	var ids []instance.Id
	for _, inst := range instances {
		azureInstance := inst.(*azureInstance)
		if azureInstance.Tags[tags.JujuStateServer] == "true" {
			ids = append(ids, inst.Id())
		}
	}
	if len(ids) == 0 {
		return nil, environs.ErrNoInstances
	}
	return ids, nil
}

// Config is specified in the Environ interface.
func (env *azureEnviron) Config() *config.Config {
	env.mu.Lock()
	defer env.mu.Unlock()
	return env.ecfg.Config
}

// SetConfig is specified in the Environ interface.
func (env *azureEnviron) SetConfig(cfg *config.Config) error {
	env.Lock()
	defer env.Unlock()

	var old *config.Config
	if env.cfg != nil {
		old = env.ecfg.Config
	}
	_, err = azureEnvironProvider{}.Validate(cfg, old)
	if err != nil {
		return err
	}

	ecfg, err := azureEnvironProvider{}.newConfig(cfg)
	if err != nil {
		return err
	}
	env.ecfg = ecfg

	subscription := ecfg.managementSubscriptionId()
	certKeyPEM := []byte(ecfg.managementCertificate())
	location := ecfg.location()

	// TODO(axw) initialise client, oauth token?
	// TODO(axw) load available role sizes

	return nil
}

// SupportedArchitectures is specified on the EnvironCapability interface.
func (env *azureEnviron) SupportedArchitectures() ([]string, error) {
	return []string{arch.AMD64}, nil
}

var unsupportedConstraints = []string{
	constraints.CpuPower,
	constraints.Tags,
}

// ConstraintsValidator is defined on the Environs interface.
func (env *azureEnviron) ConstraintsValidator() (constraints.Validator, error) {
	validator := constraints.NewValidator()
	validator.RegisterUnsupported(unsupportedConstraints)
	supportedArches, err := env.SupportedArchitectures()
	if err != nil {
		return nil, err
	}
	validator.RegisterVocabulary(constraints.Arch, supportedArches)

	instanceTypes, err := listInstanceTypes(env)
	if err != nil {
		return nil, err
	}
	instTypeNames := make([]string, len(instanceTypes))
	for i, instanceType := range instanceTypes {
		instTypeNames[i] = instanceType.Name
	}
	validator.RegisterVocabulary(constraints.InstanceType, instTypeNames)
	validator.RegisterConflicts(
		[]string{constraints.InstanceType},
		[]string{constraints.Mem, constraints.CpuCores, constraints.Arch, constraints.RootDisk})

	return validator, nil
}

// PrecheckInstance is defined on the state.Prechecker interface.
func (env *azureEnviron) PrecheckInstance(series string, cons constraints.Value, placement string) error {
	if placement != "" {
		return fmt.Errorf("unknown placement directive: %s", placement)
	}
	if !cons.HasInstanceType() {
		return nil
	}
	// Constraint has an instance-type constraint so let's see if it is valid.
	instanceTypes, err := listInstanceTypes(env)
	if err != nil {
		return err
	}
	for _, instanceType := range instanceTypes {
		if instanceType.Name == *cons.InstanceType {
			return nil
		}
	}
	return fmt.Errorf("invalid instance type %q", *cons.InstanceType)
}

// MaintainInstance is specified in the InstanceBroker interface.
func (*azureEnviron) MaintainInstance(args environs.StartInstanceParams) error {
	return nil
}

// StartInstance is specified in the InstanceBroker interface.
func (env *azureEnviron) StartInstance(args environs.StartInstanceParams) (*environs.StartInstanceResult, error) {
	if args.InstanceConfig.HasNetworks() {
		return nil, errors.New("starting instances with networks is not supported yet")
	}

	err := instancecfg.FinishInstanceConfig(args.InstanceConfig, env.Config())
	if err != nil {
		return nil, err
	}

	// Pick envtools.  Needed for the custom data (which is what we normally
	// call userdata).
	args.InstanceConfig.Tools = args.Tools[0]
	logger.Infof("picked tools %q", args.InstanceConfig.Tools)

	// Compose userdata.
	customData, err := makeCustomData(args.InstanceConfig)
	if err != nil {
		return nil, errors.Annotate(err, "cannot compose user data")
	}

	// TODO(axw) take a snapshot like we used to?
	env.mu.Lock()
	defer env.mu.Unlock()

	location := env.ecfg.location()
	instanceType, sourceImageName, err := env.selectInstanceTypeAndImage(&instances.InstanceConstraint{
		Region:      location,
		Series:      args.Tools.OneSeries(),
		Arches:      args.Tools.Arches(),
		Constraints: args.Constraints,
	})
	if err != nil {
		return nil, err
	}

	vmArgs := compute.VirtualMachine{
		Type:     "", // TODO
		Location: location,
		Tags:     args.InstanceConfig.Tags,
	}
	vmArgs.Properties.HardwareProfile.VmSize = string(instanceType)
	// TODO OS disk (using sourceImageName)
	// TODO availability set
	vmArgs.Properties.OsProfile.ComputerName = computerName
	vmArgs.Properties.OsProfile.CustomData = customData
	// TODO network
	// TODO ssh? is it handled by custom data?
	// TODO firewall

	vmClient := compute.VirtualMachinesClient{env.compute}
	vm, err := vmClient.CreateOrUpdate(resourceGroup, vmName, vmArgs)
	// TODO(axw) check if autorest.Error is an interface type
	if err != nil {
		return nil, errors.Annotate(err, "creating virtual machine")
	}
	inst := &azureEnviron{vm}

	amd64 := arch.AMD64
	hc := &instance.HardwareCharacteristics{
		Arch:     &amd64,
		Mem:      &instanceType.Mem,
		RootDisk: &instanceType.RootDisk,
		CpuCores: &instanceType.CpuCores,
	}
	return &environs.StartInstanceResult{
		Instance: inst,
		Hardware: hc,
	}, nil
}

/*
// newOSDisk creates a gwacl.OSVirtualHardDisk object suitable for an
// Azure Virtual Machine.
func (env *azureEnviron) newOSDisk(sourceImageName string) *gwacl.OSVirtualHardDisk {
	vhdName := gwacl.MakeRandomDiskName("juju")
	vhdPath := fmt.Sprintf("vhds/%s", vhdName)
	snap := env.getSnapshot()
	storageAccount := snap.ecfg.storageAccountName()
	mediaLink := gwacl.CreateVirtualHardDiskMediaLink(storageAccount, vhdPath)
	// The disk label is optional and the disk name can be omitted if
	// mediaLink is provided.
	return gwacl.NewOSVirtualHardDisk("", "", "", mediaLink, sourceImageName, "Linux")
}
*/

// StopInstances is specified in the InstanceBroker interface.
func (env *azureEnviron) StopInstances(ids ...instance.Id) error {
	vmClient := compute.VirtualMachinesClient{env.compute}
	for _, id := range ids {
		_, err := vmClient.Delete(resourceGroup, string(id))
	}
	return nil
}

// Instances is specified in the Environ interface.
func (env *azureEnviron) Instances(ids []instance.Id) ([]instance.Instance, error) {
	all, err := env.AllInstances()
	if err != nil {
		return nil, errors.Trace(err)
	}
	byId := make(map[instance.Id]instance.Instance)
	for _, inst := range all {
		byId[inst.Id()] = inst
	}
	found := true
	matching := make([]instance.Id, len(ids))
	for i, id := range ids {
		inst, ok := byId[id]
		if !ok {
			continue
		}
		matching[i] = inst
		found = false
	}
	if found == 0 {
		return nil, environs.ErrNoInstances
	} else if found < len(ids) {
		return matching, environs.ErrNoInstances
	}
	return matching, nil
}

// AllInstances is specified in the InstanceBroker interface.
func (env *azureEnviron) AllInstances() ([]instance.Instance, error) {
	vmClient := compute.VirtualMachinesClient{env.compute}
	result, err := vmClient.List(resourceGroup)
	if err != nil {
		return nil, errors.Annotate(err, "listing virtual machines")
	}
	if len(result.Value) == 0 {
		return nil, environs.ErrNoInstances
	}
	// TODO(axw) how to continue with result.NextLink?
	instances := make([]instance.Instance, len(result.Value))
	for i, vm := range result.Value {
		instances[i] = &azureInstance{vm}
	}
	return instances, nil
}

// Destroy is specified in the Environ interface.
func (env *azureEnviron) Destroy() error {
	logger.Debugf("destroying environment %q", env.Config().Name())
	// TODO(axw) delete resource group. Is that all there is to it?
	return nil
}

// OpenPorts is specified in the Environ interface. However, Azure does not
// support the global firewall mode.
func (env *azureEnviron) OpenPorts(ports []network.PortRange) error {
	return nil
}

// ClosePorts is specified in the Environ interface. However, Azure does not
// support the global firewall mode.
func (env *azureEnviron) ClosePorts(ports []network.PortRange) error {
	return nil
}

// Ports is specified in the Environ interface.
func (env *azureEnviron) Ports() ([]network.PortRange, error) {
	// TODO: implement this.
	return []network.PortRange{}, nil
}

// Provider is specified in the Environ interface.
func (env *azureEnviron) Provider() environs.EnvironProvider {
	return azureEnvironProvider{}
}

// TODO(ericsnow) lp-1398055
// Implement the ZonedEnviron interface.

// Region is specified in the HasRegion interface.
func (env *azureEnviron) Region() (simplestreams.CloudSpec, error) {
	/*
		ecfg := env.getSnapshot().ecfg
		return simplestreams.CloudSpec{
			Region:   ecfg.location(),
			Endpoint: string(gwacl.GetEndpoint(ecfg.location())),
		}, nil
	*/
	panic("TODO")
}

// SupportsUnitPlacement is specified in the state.EnvironCapability interface.
func (env *azureEnviron) SupportsUnitPlacement() error {
	return nil
}
