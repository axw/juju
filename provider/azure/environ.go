// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest"
	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/to"
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"
	"github.com/Azure/azure-sdk-for-go/arm/resources"
	"github.com/Azure/azure-sdk-for-go/arm/storage"
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"
	"github.com/juju/utils"
	"github.com/juju/utils/arch"
	"github.com/juju/utils/os"
	jujuseries "github.com/juju/utils/series"

	"github.com/juju/juju/cloudconfig/instancecfg"
	"github.com/juju/juju/cloudconfig/providerinit"
	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/environs/simplestreams"
	"github.com/juju/juju/environs/tags"
	"github.com/juju/juju/instance"
	jujunetwork "github.com/juju/juju/network"
	"github.com/juju/juju/provider/common"
	"github.com/juju/juju/state"
)

const (
	flatVirtualNetworkName         = "vnet-flat"
	flatVirtualNetworkPrefix       = "10.0.0.0/8"
	flatVirtualNetworkSubnetName   = "vnet-flat-subnet"
	flatVirtualNetworkSubnetPrefix = flatVirtualNetworkPrefix
)

type azureEnviron struct {
	resourceGroup string

	mu             sync.Mutex
	config         *azureEnvironConfig
	instanceTypes  map[string]instances.InstanceType
	subnets        map[string]*network.Subnet
	storageAccount *storage.Account
	// azure management clients
	compute   compute.ManagementClient
	resources resources.ManagementClient
	storage   storage.ManagementClient
	network   network.ManagementClient
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
	env.subnets = make(map[string]*network.Subnet)
	return &env, nil
}

// Bootstrap is specified in the Environ interface.
func (env *azureEnviron) Bootstrap(ctx environs.BootstrapContext, args environs.BootstrapParams) (arch, series string, _ environs.BootstrapFinalizer, _ error) {

	location := env.config.location
	tags, _ := env.config.ResourceTags()

	var err error
	resourceGroupsClient := resources.GroupsClient{env.resources}
	logger.Debugf("creating resource group %q", env.resourceGroup)
	_, err = resourceGroupsClient.CreateOrUpdate(env.resourceGroup, resources.Group{
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(tags),
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
	storageAccountsClient := storage.AccountsClient{env.storage}
	storageAccountName, err := createStorageAccount(
		storageAccountsClient, env.resourceGroup, location, tags,
	)
	if err != nil {
		return "", "", nil, errors.Annotate(err, "creating storage account")
	}

	// Create a flat virtual network for all VMs to connect to.
	virtualNetworksClient := network.VirtualNetworksClient{env.network}
	subnetsClient := network.SubnetsClient{env.network}
	vnet, subnet, err := createFlatVirtualNetwork(
		virtualNetworksClient, subnetsClient, env.resourceGroup, location, tags,
	)
	if err != nil {
		return "", "", nil, errors.Annotate(err, "creating virtual network")
	}
	env.subnets[to.String(vnet.Name)+":"+to.String(subnet.Name)] = subnet

	// TODO(axw) ensure user doesn't specify storage-account.
	// Update the environment's config with generated config.
	cfg, err := env.config.Config.Apply(map[string]interface{}{
		configAttrStorageAccount: storageAccountName,
	})
	if err != nil {
		return "", "", nil, errors.Trace(err)
	}
	if err := env.SetConfig(cfg); err != nil {
		return "", "", nil, errors.Trace(err)
	}

	// TODO(axw) create default availability set?
	return common.Bootstrap(ctx, env, args)
}

func createStorageAccount(
	client storage.AccountsClient,
	resourceGroup string,
	location string,
	tags map[string]string,
) (string, error) {
	const maxStorageAccountNameLen = 24
	const maxAttempts = 10
	validRunes := append([]rune(utils.LowerAlpha), []rune(utils.Digits)...)
	logger.Debugf("creating storage account (finding available name)")
	for remaining := maxAttempts; remaining > 0; remaining-- {
		accountName := utils.RandomString(maxStorageAccountNameLen, validRunes)
		logger.Debugf("- checking storage account name %q", accountName)
		result, err := client.CheckNameAvailability(
			storage.AccountCheckNameAvailabilityParameters{
				Name: to.StringPtr(accountName),
				// TODO(axw) do we need this?
				Type: to.StringPtr("Microsoft.Storage/storageAccounts"),
			},
		)
		if err != nil {
			return "", errors.Annotate(err, "checking account name availability")
		}
		if !to.Bool(result.NameAvailable) {
			logger.Debugf(
				"%q is not available (%v): %v",
				accountName, result.Reason, result.Message,
			)
			continue
		}
		createParams := storage.AccountCreateParameters{
			Location: to.StringPtr(location),
			Tags:     toTagsPtr(tags),
			Properties: &storage.AccountPropertiesCreateParameters{
				// TODO(axw) make storage account type configurable?
				AccountType: storage.StandardLRS,
			},
		}
		logger.Debugf("creating storage account %q", accountName)
		if _, err := client.Create(resourceGroup, accountName, createParams); err != nil {
			return "", errors.Trace(err)
		}
		return accountName, nil
	}
	return "", errors.New("could not find available storage account name")
}

func createFlatVirtualNetwork(
	vnetClient network.VirtualNetworksClient,
	subnetClient network.SubnetsClient,
	resourceGroup string,
	location string,
	tags map[string]string,
) (*network.VirtualNetwork, *network.Subnet, error) {
	// Vnet and subnet must be created separately. Vnet first.
	virtualNetworkParams := network.VirtualNetwork{
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(tags),
		Properties: &network.VirtualNetworkPropertiesFormat{
			AddressSpace: &network.AddressSpace{
				AddressPrefixes: toStringSlicePtr(flatVirtualNetworkPrefix),
			},
		},
	}
	logger.Debugf("creating virtual network %q", flatVirtualNetworkName)
	vnet, err := vnetClient.CreateOrUpdate(
		resourceGroup, flatVirtualNetworkName, virtualNetworkParams,
	)
	if err != nil {
		return nil, nil, errors.Annotatef(err, "creating virtual network %q", flatVirtualNetworkName)
	}

	// Now create a subnet with the same address prefix.
	subnetParams := network.Subnet{
		Properties: &network.SubnetPropertiesFormat{
			AddressPrefix: to.StringPtr(flatVirtualNetworkSubnetPrefix),
		},
	}
	// TODO(axw) security group?
	logger.Debugf("creating subnet %q", flatVirtualNetworkSubnetName)
	subnet, err := subnetClient.CreateOrUpdate(
		resourceGroup, flatVirtualNetworkName, flatVirtualNetworkSubnetName, subnetParams,
	)
	if err != nil {
		return nil, nil, errors.Annotatef(err, "creating subnet %q", flatVirtualNetworkSubnetName)
	}
	return &vnet, &subnet, nil
}

// StateServerInstances is specified in the Environ interface.
func (env *azureEnviron) StateServerInstances() ([]instance.Id, error) {
	// StateServerInstances may be called before bootstrapping, to
	// determine whether or not the environment is already bootstrapped.
	//
	// First check whether the resource group exists. Ideally we could
	// just call AllInstances and check the error, but the error from
	// the Azure SDK isn't well structured enough to support it nicely.
	env.mu.Lock()
	resourceGroupsClient := resources.GroupsClient{env.resources}
	env.mu.Unlock()
	if result, err := resourceGroupsClient.Get(env.resourceGroup); err != nil {
		if result.StatusCode == http.StatusNotFound {
			return nil, environs.ErrNoInstances
		}
		return nil, errors.Annotate(err, "querying resource group")
	}

	// State servers are tagged with tags.JujuStateServer, so just
	// list the instances and pick those ones out.
	instances, err := env.AllInstances()
	if err != nil {
		return nil, err
	}
	var ids []instance.Id
	for _, inst := range instances {
		azureInstance := inst.(*azureInstance)
		if toTags(azureInstance.Tags)[tags.JujuStateServer] == "true" {
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
	env.resources = resources.New(env.config.subscriptionId)
	env.storage = storage.New(env.config.subscriptionId)
	env.network = network.New(env.config.subscriptionId)
	clients := map[string]*autorest.Client{
		"azure.compute":   &env.compute.Client,
		"azure.resources": &env.resources.Client,
		"azure.storage":   &env.storage.Client,
		"azure.network":   &env.network.Client,
	}
	for id, client := range clients {
		client.Authorizer = env.config.token
		logger := loggo.GetLogger(id)
		client.RequestInspector = tracingPrepareDecorator(logger)
		client.ResponseInspector = tracingRespondDecorator(logger)
	}

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
	networkClient := env.network
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
	flatVirtualNetworkSubnet, err := env.getVirtualNetworkSubnetLocked(
		flatVirtualNetworkName, flatVirtualNetworkSubnetName,
	)
	if err != nil {
		env.mu.Unlock()
		return nil, errors.Trace(err)
	}
	env.mu.Unlock()

	// Identify the instance type and image to provision.
	instanceSpec, err := findInstanceSpec(env, instanceTypes, &instances.InstanceConstraint{
		Region:      regionFromLocation(location),
		Series:      args.Tools.OneSeries(),
		Arches:      args.Tools.Arches(),
		Constraints: args.Constraints,
	})
	if err != nil {
		return nil, err
	}

	machineTag := names.NewMachineTag(args.InstanceConfig.MachineId)
	vmName := resourceName(machineTag, envName)
	vmTags := make(map[string]string)
	for k, v := range args.InstanceConfig.Tags {
		vmTags[k] = v
	}
	// jujuMachineNameTag identifies the VM name, in which is encoded
	// the Juju machine name. We tag all resources related to the
	// machine with this.
	jujuMachineNameTag := tags.JujuTagPrefix + "machine-name"
	vmTags[jujuMachineNameTag] = vmName

	vm, err := createVirtualMachine(
		env.resourceGroup, location, vmName, vmTags,
		instanceSpec, args.InstanceConfig,
		flatVirtualNetworkSubnet.ID,
		storageAccount, networkClient, vmClient,
	)
	if err != nil {
		// TODO(axw) delete resources
		return nil, errors.Annotatef(err, "creating virtual machine %q", vmName)
	}

	// Note: the instance is initialised without addresses to keep the
	// API chatter down. We will refresh the instance if we need to know
	// the addresses.
	inst := &azureInstance{vm, nil, nil, env}
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

// createVirtualMachine creates a virtual machine and related resources.
//
// All resources created are tagged with the specified "vmTags", so if
// this function fails then all resources can be deleted by tag.
func createVirtualMachine(
	resourceGroup, location, vmName string,
	vmTags map[string]string,
	instanceSpec *instances.InstanceSpec,
	instanceConfig *instancecfg.InstanceConfig,
	flatVirtualNetworkSubnetID *string,
	storageAccount *storage.Account,
	networkClient network.ManagementClient,
	vmClient compute.VirtualMachinesClient,
) (compute.VirtualMachine, error) {

	storageProfile, err := newStorageProfile(
		vmName, instanceConfig.Series,
		instanceSpec, storageAccount,
	)
	if err != nil {
		return compute.VirtualMachine{}, errors.Annotate(err, "creating storage profile")
	}

	osProfile, err := newOSProfile(vmName, instanceConfig)
	if err != nil {
		return compute.VirtualMachine{}, errors.Annotate(err, "creating OS profile")
	}

	networkProfile, err := newNetworkProfile(
		networkClient, vmName,
		flatVirtualNetworkSubnetID,
		resourceGroup, location, vmTags,
	)
	if err != nil {
		return compute.VirtualMachine{}, errors.Annotate(err, "creating network profile")
	}

	vmArgs := compute.VirtualMachine{
		Name:     to.StringPtr(vmName),
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(vmTags),
		Properties: &compute.VirtualMachineProperties{
			HardwareProfile: &compute.HardwareProfile{
				VMSize: compute.VirtualMachineSizeTypes(
					instanceSpec.InstanceType.Name,
				),
			},
			StorageProfile: storageProfile,
			OsProfile:      osProfile,
			NetworkProfile: networkProfile,
			// TODO availability set
		},
	}
	return vmClient.CreateOrUpdate(resourceGroup, vmName, vmArgs)
}

// newStorageProfile creates the storage profile for a virtual machine,
// based on the series and chosen instance spec.
func newStorageProfile(
	vmName string,
	series string,
	instanceSpec *instances.InstanceSpec,
	storageAccount *storage.Account,
) (*compute.StorageProfile, error) {

	// TODO(axw) We should be using the image name from instanceSpec.
	// There is currently no way to specify the image name in VirtualMachine.
	seriesOS, err := jujuseries.GetOSFromSeries(series)
	if err != nil {
		return nil, errors.Trace(err)
	}

	var imageReference *compute.ImageReference
	switch seriesOS {
	case os.Ubuntu:
		imageReference = &compute.ImageReference{
			Publisher: to.StringPtr("Canonical"),
			Offer:     to.StringPtr("UbuntuServer"),
			Sku:       to.StringPtr("14.04.3-LTS"),
			Version:   to.StringPtr("latest"),
		}
	default:
		// TODO(axw) Windows, CentOS
		return nil, errors.NotSupportedf("%s", seriesOS)
	}

	vhdsRoot := to.String(storageAccount.Properties.PrimaryEndpoints.Blob) + "vhds/"
	osDiskName := vmName + "-osdisk"
	osDisk := &compute.OSDisk{
		Name:         to.StringPtr(osDiskName),
		CreateOption: compute.FromImage,
		Caching:      compute.ReadWrite,
		Vhd: &compute.VirtualHardDisk{
			URI: to.StringPtr(
				vhdsRoot + osDiskName + ".vhd",
			),
		},
	}
	return &compute.StorageProfile{
		ImageReference: imageReference,
		OsDisk:         osDisk,
	}, nil
}

func newOSProfile(vmName string, instanceConfig *instancecfg.InstanceConfig) (*compute.OSProfile, error) {
	customData, err := providerinit.ComposeUserData(instanceConfig, nil, AzureRenderer{})
	if err != nil {
		return nil, errors.Annotate(err, "composing user data")
	}

	osProfile := &compute.OSProfile{
		ComputerName: to.StringPtr(vmName),
		CustomData:   to.StringPtr(string(customData)),
	}

	seriesOS, err := jujuseries.GetOSFromSeries(instanceConfig.Series)
	if err != nil {
		return nil, errors.Trace(err)
	}
	switch seriesOS {
	case os.Ubuntu, os.CentOS, os.Arch:
		// SSH keys are handled by custom data, but must also be
		// specified in order to forego providing a password, and
		// disable password authentication.
		publicKeys := []compute.SSHPublicKey{{
			Path:    to.StringPtr("/home/ubuntu/.ssh/authorized_keys"),
			KeyData: to.StringPtr(instanceConfig.AuthorizedKeys),
		}}
		osProfile.AdminUsername = to.StringPtr("ubuntu")
		osProfile.LinuxConfiguration = &compute.LinuxConfiguration{
			DisablePasswordAuthentication: to.BoolPtr(true),
			SSH: &compute.SSHConfiguration{PublicKeys: &publicKeys}}
	default:
		// TODO(axw) support Windows
		return nil, errors.NotSupportedf("%s", seriesOS)
	}
	return osProfile, nil
}

func newNetworkProfile(
	client network.ManagementClient,
	vmName string,
	primarySubnetId *string,
	resourceGroup string,
	location string,
	tags map[string]string,
) (*compute.NetworkProfile, error) {
	// Create a public IP for the NIC.
	pipClient := network.PublicIPAddressesClient{client}
	publicIPAddressParams := network.PublicIPAddress{
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(tags),
		Properties: &network.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: network.Dynamic,
		},
	}
	publicIPAddress, err := pipClient.CreateOrUpdate(resourceGroup, vmName+"-public-ip", publicIPAddressParams)
	if err != nil {
		return nil, errors.Annotatef(err, "creating public IP address for %q", vmName)
	}

	// Create a primary NIC for the machine.
	nicClient := network.InterfacesClient{client}
	ipConfigurations := []network.InterfaceIPConfiguration{{
		Name: to.StringPtr("primary"),
		Properties: &network.InterfaceIPConfigurationPropertiesFormat{
			PrivateIPAllocationMethod: network.Dynamic,
			Subnet:          &network.SubResource{ID: primarySubnetId},
			PublicIPAddress: &network.SubResource{publicIPAddress.ID},
		},
	}}
	primaryNicName := vmName + "-primary"
	primaryNicParams := network.Interface{
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(tags),
		Properties: &network.InterfacePropertiesFormat{
			IPConfigurations: &ipConfigurations,
		},
	}
	primaryNic, err := nicClient.CreateOrUpdate(resourceGroup, primaryNicName, primaryNicParams)
	if err != nil {
		return nil, errors.Annotatef(err, "creating network interface for %q", vmName)
	}

	// For now we only attach a single, flat network to each machine.
	networkInterfaces := []compute.NetworkInterfaceReference{{
		ID: primaryNic.ID,
		Properties: &compute.NetworkInterfaceReferenceProperties{
			Primary: to.BoolPtr(true),
		},
	}}
	// TODO firewall?
	return &compute.NetworkProfile{&networkInterfaces}, nil
}

// StopInstances is specified in the InstanceBroker interface.
func (env *azureEnviron) StopInstances(ids ...instance.Id) error {
	vmClient := compute.VirtualMachinesClient{env.compute}
	for _, id := range ids {
		// TODO(axw) delete VMs in parallel.
		// TODO(axw) delete associated resources, e.g. NICs.
		_, err := vmClient.Delete(env.resourceGroup, string(id))
		if err != nil {
			return errors.Trace(err)
		}
	}
	return nil
}

// Instances is specified in the Environ interface.
func (env *azureEnviron) Instances(ids []instance.Id) ([]instance.Instance, error) {
	// TODO(axw) optimise the len(1) case.
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
	if result.Value == nil || len(*result.Value) == 0 {
		return nil, environs.ErrNoInstances
	}
	// TODO(axw) how to continue with result.NextLink?
	instances := make([]instance.Instance, len(*result.Value))
	for i, vm := range *result.Value {
		inst := &azureInstance{vm, nil, nil, env}
		if err := inst.refreshAddresses(); err != nil {
			return nil, errors.Trace(err)
		}
		instances[i] = inst
	}
	return instances, nil
}

// Destroy is specified in the Environ interface.
func (env *azureEnviron) Destroy() error {
	logger.Debugf("destroying environment %q", env.Config().Name())
	client := resources.GroupsClient{env.resources}
	if _, err := client.Delete(env.resourceGroup); err != nil {
		return errors.Annotatef(err, "deleting resource group %q", env.resourceGroup)
	}
	return nil
}

// OpenPorts is specified in the Environ interface. However, Azure does not
// support the global firewall mode.
func (env *azureEnviron) OpenPorts(ports []jujunetwork.PortRange) error {
	return nil
}

// ClosePorts is specified in the Environ interface. However, Azure does not
// support the global firewall mode.
func (env *azureEnviron) ClosePorts(ports []jujunetwork.PortRange) error {
	return nil
}

// Ports is specified in the Environ interface.
func (env *azureEnviron) Ports() ([]jujunetwork.PortRange, error) {
	// TODO: implement this.
	return []jujunetwork.PortRange{}, nil
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
		Region:   regionFromLocation(location),
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
	if result.Value != nil {
		for _, size := range *result.Value {
			instanceType := newInstanceType(size)
			instanceTypes[instanceType.Name] = instanceType
			// Create aliases for standard role sizes.
			if strings.HasPrefix(instanceType.Name, "Standard_") {
				instanceTypes[instanceType.Name[len("Standard_"):]] = instanceType
			}
		}
	}
	env.instanceTypes = instanceTypes
	return instanceTypes, nil
}

func (env *azureEnviron) getStorageAccountLocked() (*storage.Account, error) {
	if env.storageAccount != nil {
		return env.storageAccount, nil
	}

	client := storage.AccountsClient{env.storage}
	resourceGroup := env.resourceGroup
	accountName := env.config.storageAccount

	account, err := client.GetProperties(resourceGroup, accountName)
	if err != nil {
		return nil, errors.Annotate(err, "getting storage account")
	}
	// TODO(axw) ensure the storage account is fully provisioned,
	// retry until it is.

	env.storageAccount = &account
	return &account, nil
}

func (env *azureEnviron) getVirtualNetworkSubnetLocked(vnetName, subnetName string) (*network.Subnet, error) {
	subnetKey := vnetName + ":" + subnetName
	if subnet, ok := env.subnets[subnetKey]; ok {
		return subnet, nil
	}

	client := network.SubnetsClient{env.network}
	resourceGroup := env.resourceGroup

	subnet, err := client.Get(resourceGroup, vnetName, subnetName)
	if err != nil {
		return nil, errors.Annotate(err, "getting subnet")
	}

	env.subnets[subnetKey] = &subnet
	return &subnet, nil
}
