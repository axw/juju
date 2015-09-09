// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/resources"
	"github.com/Azure/azure-sdk-for-go/arm/storage"
	"github.com/juju/errors"
	"github.com/juju/names"

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
	"github.com/juju/juju/version"
)

const (
	// Address space of the virtual network used by the nodes in this
	// environement, in CIDR notation. This is the network used for
	// machine-to-machine communication.
	networkDefinition = "10.0.0.0/8"
)

type azureEnviron struct {
	resourceGroup string

	mu             sync.Mutex
	config         *azureEnvironConfig
	instanceTypes  map[string]instances.InstanceType
	compute        compute.ComputeManagementClient
	resources      resources.ResourceManagementClient
	storage        storage.StorageManagementClient
	storageAccount *storage.StorageAccount
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
	env.resourceGroup = resourceGroupName(cfg)
	return &env, nil
}

// Bootstrap is specified in the Environ interface.
func (env *azureEnviron) Bootstrap(ctx environs.BootstrapContext, args environs.BootstrapParams) (arch, series string, _ environs.BootstrapFinalizer, _ error) {

	location := env.config.location
	tags, _ := env.config.ResourceTags()

	var err error
	resourceGroupsClient := resources.ResourceGroupsClient{env.resources}
	_, err = resourceGroupsClient.CreateOrUpdate(env.resourceGroup, resources.ResourceGroup{
		Name:     env.resourceGroup,
		Location: location,
		Tags:     tags,
	})
	if err != nil {
		return "", "", nil, errors.Annotate(err, "creating resource group")
	}

	arch, series, finalizer, err := env.bootstrapResourceGroup(ctx, args, location, tags)
	if err != nil {
		if _, err := resourceGroupsClient.Delete(env.resourceGroup); err != nil {
			logger.Errorf("failed to delete resource group %q: %v", env.resourceGroup, err)
		}
		return "", "", nil, errors.Trace(err)
	}
	return arch, series, finalizer, nil
}

func (env *azureEnviron) bootstrapResourceGroup(
	ctx environs.BootstrapContext,
	args environs.BootstrapParams,
	location string,
	tags map[string]string,
) (arch, series string, _ environs.BootstrapFinalizer, _ error) {

	// Create a storage account.
	storageAccountsClient := storage.StorageAccountsClient{env.storage}
	storageAccount, err := createStorageAccount(
		storageAccountsClient, env.resourceGroup, location, tags,
	)
	if err != nil {
		return "", "", nil, errors.Annotate(err, "creating storage account")
	}
	env.storageAccount = storageAccount

	// TODO(axw) ensure user doesn't specify storage-account.
	// Update the environment's config with generated config.
	cfg, err := env.config.Config.Apply(map[string]interface{}{
		configAttrStorageAccount: storageAccount.Name,
	})
	if err != nil {
		return "", "", nil, errors.Trace(err)
	}
	if err := env.SetConfig(cfg); err != nil {
		return "", "", nil, errors.Trace(err)
	}

	// TODO(axw) create default availability set?
	// TODO(axw) create vnet?
	return common.Bootstrap(ctx, env, args)
}

func createStorageAccount(
	client storage.StorageAccountsClient,
	resourceGroup string,
	location string,
	tags map[string]string,
) (*storage.StorageAccount, error) {
	const maxStorageAccountNameLen = 24
	const maxAttempts = 10
	validRunes := append([]rune(lowerAlpha), []rune(digits)...)
	for remaining := maxAttempts; remaining > 0; remaining-- {
		accountName := randomString(maxStorageAccountNameLen, validRunes)
		result, err := client.CheckNameAvailability(
			storage.StorageAccountCheckNameAvailabilityParameters{
				Name: accountName,
			},
		)
		if err != nil {
			return nil, errors.Annotate(err, "checking account name availability")
		}
		if !result.NameAvailable {
			logger.Debugf(
				"%q is not available (%v): %v",
				accountName, result.Reason, result.Message,
			)
			continue
		}
		createParams := storage.StorageAccountCreateParameters{
			Location: location,
			Tags:     tags,
		}
		// TODO(axw) make storage account type configurable?
		createParams.Properties.AccountType = storage.StandardLRS
		account, err := client.Create(resourceGroup, accountName, createParams)
		if err != nil {
			return nil, errors.Trace(err)
		}
		return &account, nil
	}
	return nil, errors.New("could not find available storage account name")
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
	return env.config.Config
}

// SetConfig is specified in the Environ interface.
func (env *azureEnviron) SetConfig(cfg *config.Config) error {
	env.mu.Lock()
	defer env.mu.Unlock()

	var old *config.Config
	if env.config != nil {
		old = env.config.Config
	}
	ecfg, err := validateConfig(cfg, old)
	if err != nil {
		return err
	}
	env.config = ecfg

	// Initialise clients.
	//
	// TODO(axw) we need to set the URI in each of the
	// SDK packages for the China locations.
	env.compute = compute.New(env.config.subscriptionId)
	env.compute.Authorizer = env.config.token
	env.resources = resources.New(env.config.subscriptionId)
	env.resources.Authorizer = env.config.token
	env.storage = storage.New(env.config.subscriptionId)
	env.storage.Authorizer = env.config.token

	// Invalidate instance types when the location changes.
	if old != nil {
		oldLocation := old.UnknownAttrs()["location"].(string)
		if env.config.location != oldLocation {
			env.instanceTypes = nil
		}
	}

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

	instanceTypes, err := env.getInstanceTypes()
	if err != nil {
		return nil, err
	}
	instTypeNames := make([]string, 0, len(instanceTypes))
	for instTypeName := range instanceTypes {
		instTypeNames = append(instTypeNames, instTypeName)
	}
	validator.RegisterVocabulary(constraints.InstanceType, instTypeNames)
	validator.RegisterConflicts(
		[]string{constraints.InstanceType},
		[]string{constraints.Mem, constraints.CpuCores, constraints.Arch, constraints.RootDisk},
	)
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
	instanceTypes, err := env.getInstanceTypes()
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

	// Get the required configuration and config-dependent information
	// required to create the instance. We take the lock just once, to
	// ensure we obtain all information based on the same configuration.
	env.mu.Lock()
	location := env.config.location
	envName := env.config.Name()
	vmClient := compute.VirtualMachinesClient{env.compute}
	instanceTypes, err := env.getInstanceTypesLocked()
	if err != nil {
		env.mu.Unlock()
		return nil, errors.Trace(err)
	}
	storageAccount, err := env.getStorageAccountLocked()
	if err != nil {
		env.mu.Unlock()
		return nil, errors.Trace(err)
	}
	env.mu.Unlock()

	// Identify the instance type and image to provision.
	instanceSpec, err := findInstanceSpec(env, instanceTypes, &instances.InstanceConstraint{
		Region:      location,
		Series:      args.Tools.OneSeries(),
		Arches:      args.Tools.Arches(),
		Constraints: args.Constraints,
	})
	if err != nil {
		return nil, err
	}

	// Prepare parameters for creating the instance.
	machineTag := names.NewMachineTag(args.InstanceConfig.MachineId)
	vmName := resourceName(machineTag, envName)
	vmArgs := compute.VirtualMachine{
		Location: location,
		Tags:     args.InstanceConfig.Tags,
	}
	vmArgs.Properties.HardwareProfile.VmSize = compute.VirtualMachineSizeTypes(instanceSpec.InstanceType.Name)
	if err := setVirtualMachineOsDisk(
		&vmArgs, vmName, args.InstanceConfig.Series,
		instanceSpec, storageAccount,
	); err != nil {
		return nil, errors.Trace(err)
	}
	if err := setVirtualMachineOsProfile(&vmArgs, vmName, args.InstanceConfig); err != nil {
		return nil, errors.Trace(err)
	}
	// TODO network
	// TODO availability set
	// TODO firewall?

	vm, err := vmClient.CreateOrUpdate(env.resourceGroup, vmName, vmArgs)
	// TODO(axw) check if autorest.Error is an interface type
	if err != nil {
		return nil, errors.Annotate(err, "creating virtual machine")
	}
	inst := &azureInstance{vm}

	amd64 := arch.AMD64
	hc := &instance.HardwareCharacteristics{
		Arch:     &amd64,
		Mem:      &instanceSpec.InstanceType.Mem,
		RootDisk: &instanceSpec.InstanceType.RootDisk,
		CpuCores: &instanceSpec.InstanceType.CpuCores,
	}
	return &environs.StartInstanceResult{
		Instance: inst,
		Hardware: hc,
	}, nil
}

// setVirtualMachineOsDisk sets the OS disk parameters for the
// virtual machine, base on the series and chosen instance spec.
func setVirtualMachineOsDisk(
	vm *compute.VirtualMachine,
	vmName string,
	series string,
	instanceSpec *instances.InstanceSpec,
	storageAccount *storage.StorageAccount,
) error {
	storageProfile := &vm.Properties.StorageProfile
	osDisk := &storageProfile.OsDisk

	os, err := version.GetOSFromSeries(series)
	if err != nil {
		return errors.Trace(err)
	}
	switch os {
	case version.Ubuntu, version.CentOS, version.Arch:
		osDisk.OsType = compute.Linux
	case version.Windows:
		osDisk.OsType = compute.Windows
	default:
		return errors.NotSupportedf("%s", os)
	}

	// TODO(axw) this should be using the image name from instanceSpec.
	// There is currently no way to specify the image name in VirtualMachine.

	switch os {
	case version.Ubuntu:
		storageProfile.ImageReference.Publisher = "Canonical"
		storageProfile.ImageReference.Offer = "UbuntuServer"
		storageProfile.ImageReference.Sku = "14.04.3-LTS"
		storageProfile.ImageReference.Version = "latest"
	default:
		// TODO(axw)
		return errors.NotImplementedf("%s", os)
	}

	osDisk.Name = vmName + "-osdisk"
	osDisk.CreateOption = compute.FromImage
	osDisk.Caching = compute.ReadWrite
	osDisk.Vhd.Uri = storageAccount.Properties.PrimaryEndpoints.Blob + "vhds/" + osDisk.Name + ".vhd"
	return nil
}

func setVirtualMachineOsProfile(
	vm *compute.VirtualMachine,
	vmName string,
	instanceConfig *instancecfg.InstanceConfig,
) error {
	osProfile := &vm.Properties.OsProfile
	osProfile.ComputerName = vmName

	customData, err := makeCustomData(instanceConfig)
	if err != nil {
		return errors.Annotate(err, "composing custom data")
	}
	osProfile.CustomData = customData

	os, err := version.GetOSFromSeries(instanceConfig.Series)
	if err != nil {
		return errors.Trace(err)
	}
	switch os {
	case version.Ubuntu, version.CentOS, version.Arch:
		// SSH keys are handled by custom data.
		osProfile.AdminUsername = "ubuntu"
		osProfile.LinuxConfiguration.DisablePasswordAuthentication = true
	default:
		// TODO(axw) support Windows
		return errors.NotSupportedf("%s", os)
	}
	return nil
}

// StopInstances is specified in the InstanceBroker interface.
func (env *azureEnviron) StopInstances(ids ...instance.Id) error {
	vmClient := compute.VirtualMachinesClient{env.compute}
	for _, id := range ids {
		// TODO(axw) delete VMs in parallel.
		_, err := vmClient.Delete(env.resourceGroup, string(id))
		if err != nil {
			return errors.Trace(err)
		}
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
	var found int
	matching := make([]instance.Instance, len(ids))
	for i, id := range ids {
		inst, ok := byId[id]
		if !ok {
			continue
		}
		matching[i] = inst
		found++
	}
	if found == 0 {
		return nil, environs.ErrNoInstances
	} else if found < len(ids) {
		return matching, environs.ErrPartialInstances
	}
	return matching, nil
}

// AllInstances is specified in the InstanceBroker interface.
func (env *azureEnviron) AllInstances() ([]instance.Instance, error) {
	env.mu.Lock()
	vmClient := compute.VirtualMachinesClient{env.compute}
	env.mu.Unlock()

	result, err := vmClient.List(env.resourceGroup)
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
	client := resources.ResourceGroupsClient{env.resources}
	if _, err := client.Delete(env.resourceGroup); err != nil {
		return errors.Annotatef(err, "deleting resource group %q", env.resourceGroup)
	}
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
	env.mu.Lock()
	location := env.config.location
	env.mu.Unlock()
	return simplestreams.CloudSpec{
		Region:   location,
		Endpoint: getEndpoint(location),
	}, nil
}

// SupportsUnitPlacement is specified in the state.EnvironCapability interface.
func (env *azureEnviron) SupportsUnitPlacement() error {
	return nil
}

// resourceGroupName returns the name of the environment's resource group.
func resourceGroupName(cfg *config.Config) string {
	uuid, _ := cfg.UUID()
	// UUID is always available for azure environments, since the (new)
	// provider was introduced after environment UUIDs.
	envTag := names.NewEnvironTag(uuid)
	return resourceName(envTag, cfg.Name())
}

// resourceName returns the string to use for a resource's Name tag,
// to help users identify Juju-managed resources in the AWS console.
func resourceName(tag names.Tag, envName string) string {
	return fmt.Sprintf("juju-%s-%s", envName, tag)
}

// getInstanceTypes gets the instance types available for the configured
// location, keyed by name.
func (env *azureEnviron) getInstanceTypes() (map[string]instances.InstanceType, error) {
	env.mu.Lock()
	defer env.mu.Unlock()
	instanceTypes, err := env.getInstanceTypesLocked()
	if err != nil {
		return nil, errors.Annotate(err, "getting instance types")
	}
	return instanceTypes, nil
}

func (env *azureEnviron) getInstanceTypesLocked() (map[string]instances.InstanceType, error) {
	if env.instanceTypes != nil {
		return env.instanceTypes, nil
	}

	location := env.config.location
	client := compute.VirtualMachineSizesClient{env.compute}

	result, err := client.List(location)
	if err != nil {
		return nil, errors.Trace(err)
	}
	instanceTypes := make(map[string]instances.InstanceType)
	for _, size := range result.Value {
		instanceType := newInstanceType(size)
		instanceTypes[instanceType.Name] = instanceType
		// Create aliases for standard role sizes.
		if strings.HasPrefix(instanceType.Name, "Standard_") {
			instanceTypes[instanceType.Name[len("Standard_"):]] = instanceType
		}
	}

	env.instanceTypes = instanceTypes
	return instanceTypes, nil
}

func (env *azureEnviron) getStorageAccountLocked() (*storage.StorageAccount, error) {
	if env.storageAccount != nil {
		return env.storageAccount, nil
	}

	client := storage.StorageAccountsClient{env.storage}
	resourceGroup := env.resourceGroup
	accountName := env.config.storageAccount

	account, err := client.GetProperties(resourceGroup, accountName)
	if err != nil {
		return nil, errors.Annotate(err, "getting storage account")
	}

	env.storageAccount = &account
	return &account, nil
}
