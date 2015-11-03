// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"fmt"
	"net"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest"
	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/to"
	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/azure-sdk-for-go/arm/network"
	"github.com/Azure/azure-sdk-for-go/arm/resources"
	"github.com/Azure/azure-sdk-for-go/arm/storage"
	azurestorage "github.com/Azure/azure-sdk-for-go/storage"
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/names"
	"github.com/juju/utils"
	"github.com/juju/utils/arch"
	"github.com/juju/utils/os"
	jujuseries "github.com/juju/utils/series"
	"github.com/juju/utils/set"

	"github.com/juju/juju/cloudconfig/instancecfg"
	"github.com/juju/juju/cloudconfig/providerinit"
	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/environs/tags"
	"github.com/juju/juju/instance"
	jujunetwork "github.com/juju/juju/network"
	internalazurestorage "github.com/juju/juju/provider/azure/internal/azurestorage"
	"github.com/juju/juju/provider/azure/internal/azureutils"
	"github.com/juju/juju/provider/common"
	"github.com/juju/juju/state"
	"github.com/juju/juju/state/multiwatcher"
)

const (
	// internalNetworkName is the name of the virtual network that all
	// Juju machines are connected to, so that they can communicate
	// with the controllers, and with each other.
	//
	// Each resource group is given its own subnet and network security
	// group to manage. The first resource group will be assigned the
	// subnet address prefix 10.0.0.0/16, the second 10.1.0.0/16, etc.,
	// which allows for up to 256 enviroments/resource groups. Azure
	// only supports 100, but this can be extended by contacting support;
	// we can make the address prefixes configurable if necessary.
	internalNetworkName = "juju-internal"

	// internalSecurityGroupName is the name of the network security
	// group that each machine's primary (internal network) NIC is
	// attached to.
	internalSecurityGroupName = "juju-internal"
)

const (
	// securityRuleInternalMin is the beginning of the range of
	// internal security group rules defined by Juju.
	securityRuleInternalMin = 100

	// securityRuleInternalMax is the end of the range of internal
	// security group rules defined by Juju.
	securityRuleInternalMax = 199

	// securityRuleInternalSSHInbound is the priority of the
	// security rule that allows inbound SSH access to all
	// machines.
	securityRuleInternalSSHInbound = securityRuleInternalMin + iota
)

var sshSecurityRule = network.SecurityRule{
	Name: to.StringPtr("SSHInbound"),
	Properties: &network.SecurityRulePropertiesFormat{
		Description:              to.StringPtr("Allow SSH access to all machines"),
		Protocol:                 network.SecurityRuleProtocolTCP,
		SourceAddressPrefix:      to.StringPtr("*"),
		SourcePortRange:          to.StringPtr("*"),
		DestinationAddressPrefix: to.StringPtr("*"),
		DestinationPortRange:     to.StringPtr("22"),
		Access:                   network.Allow,
		Priority:                 to.IntPtr(securityRuleInternalSSHInbound),
		Direction:                network.Inbound,
	},
}

type azureEnviron struct {
	provider                *azureEnvironProvider
	resourceGroup           string
	controllerResourceGroup string
	envName                 string

	mu            sync.Mutex
	config        *azureEnvironConfig
	instanceTypes map[string]instances.InstanceType
	// azure management clients
	compute       compute.ManagementClient
	resources     resources.ManagementClient
	storage       storage.ManagementClient
	network       network.ManagementClient
	storageClient azurestorage.Client
}

var _ environs.Environ = (*azureEnviron)(nil)
var _ state.Prechecker = (*azureEnviron)(nil)

// newEnviron creates a new azureEnviron.
func newEnviron(provider *azureEnvironProvider, cfg *config.Config) (*azureEnviron, error) {
	env := azureEnviron{provider: provider}
	err := env.SetConfig(cfg)
	if err != nil {
		return nil, err
	}
	env.resourceGroup = resourceGroupName(cfg)
	env.controllerResourceGroup = env.config.controllerResourceGroup
	env.envName = cfg.Name()
	return &env, nil
}

// Bootstrap is specified in the Environ interface.
func (env *azureEnviron) Bootstrap(
	ctx environs.BootstrapContext,
	args environs.BootstrapParams,
) (arch, series string, _ environs.BootstrapFinalizer, _ error) {

	cfg, err := env.initResourceGroup()
	if err != nil {
		return "", "", nil, errors.Annotate(err, "creating controller resource group")
	}
	if err := env.SetConfig(cfg); err != nil {
		return "", "", nil, errors.Annotate(err, "updating config")
	}

	arch, series, finalizer, err := common.Bootstrap(ctx, env, args)
	if err != nil {
		if err := env.Destroy(); err != nil {
			logger.Errorf(
				"failed to destroy environment: %v",
				env.controllerResourceGroup, err,
			)
		}
		return "", "", nil, errors.Trace(err)
	}
	return arch, series, finalizer, nil
}

// initResourceGroup creates and initialises a resource group for this
// environment. The resource group will have a storage account and a
// subnet associated with it (but not necessarily contained within:
// see subnet creation).
func (env *azureEnviron) initResourceGroup() (*config.Config, error) {
	location := env.config.location
	tags, _ := env.config.ResourceTags()
	resourceGroupsClient := resources.GroupsClient{env.resources}

	logger.Debugf("creating resource group %q", env.resourceGroup)
	_, err := resourceGroupsClient.CreateOrUpdate(env.resourceGroup, resources.Group{
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(tags),
	})
	if err != nil {
		return nil, errors.Annotate(err, "creating resource group")
	}

	var vnetPtr *network.VirtualNetwork
	if env.resourceGroup == env.controllerResourceGroup {
		// Create an internal network for all VMs to connect to.
		vnetPtr, err = createInternalVirtualNetwork(
			env.network, env.controllerResourceGroup, location, tags,
		)
		if err != nil {
			return nil, errors.Annotate(err, "creating virtual network")
		}
	} else {
		// We're creating a hosted environment, so we need to fetch
		// the virtual network to create a subnet below.
		vnetClient := network.VirtualNetworksClient{env.network}
		vnet, err := vnetClient.Get(env.controllerResourceGroup, internalNetworkName)
		if err != nil {
			return nil, errors.Annotate(err, "getting virtual network")
		}
		vnetPtr = &vnet
	}

	_, err = createInternalSubnet(
		env.network, env.resourceGroup, env.controllerResourceGroup,
		vnetPtr, location, tags,
	)
	if err != nil {
		return nil, errors.Annotate(err, "creating subnet")
	}

	// Create a storage account for the resource group.
	storageAccountsClient := storage.AccountsClient{env.storage}
	storageAccountName, err := createStorageAccount(
		storageAccountsClient, env.config.storageAccountType,
		env.resourceGroup, location, tags,
	)
	if err != nil {
		return nil, errors.Annotate(err, "creating storage account")
	}
	return env.config.Config.Apply(map[string]interface{}{
		configAttrStorageAccount: storageAccountName,
	})
}

func createStorageAccount(
	client storage.AccountsClient,
	accountType storage.AccountType,
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
				// Azure is a little inconsistent with when Type is
				// required. It's required here.
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
				AccountType: accountType,
			},
		}
		logger.Debugf("- creating %q storage account %q", accountType, accountName)
		if _, err := client.Create(resourceGroup, accountName, createParams); err != nil {
			return "", errors.Trace(err)
		}
		return accountName, nil
	}
	return "", errors.New("could not find available storage account name")
}

func createInternalVirtualNetwork(
	client network.ManagementClient,
	controllerResourceGroup string,
	location string,
	tags map[string]string,
) (*network.VirtualNetwork, error) {
	addressPrefixes := make([]string, 256)
	for i := range addressPrefixes {
		addressPrefixes[i] = fmt.Sprintf("10.%d.0.0/16", i)
	}
	virtualNetworkParams := network.VirtualNetwork{
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(tags),
		Properties: &network.VirtualNetworkPropertiesFormat{
			AddressSpace: &network.AddressSpace{&addressPrefixes},
		},
	}
	logger.Debugf("creating virtual network %q", internalNetworkName)
	vnetClient := network.VirtualNetworksClient{client}
	vnet, err := vnetClient.CreateOrUpdate(
		controllerResourceGroup, internalNetworkName, virtualNetworkParams,
	)
	if err != nil {
		return nil, errors.Annotatef(err, "creating virtual network %q", internalNetworkName)
	}
	return &vnet, nil
}

// createInternalSubnet creates an internal subnet for the specified resource group,
// within the specified virtual network.
//
// Subnets are tied to the resource group of the virtual network, so we must create
// them all in the controller resource group. We create the network security group
// for the subnet in the environment's resource group.
//
// NOTE(axw) this method expects an up-to-date VirtualNetwork, and expects that are
// no concurrent subnet additions to the virtual network. At the moment we have only
// three places where we modify subnets: at bootstrap, when a new environment is
// created, and when an environment is destroyed.
func createInternalSubnet(
	client network.ManagementClient,
	resourceGroup, controllerResourceGroup string,
	vnet *network.VirtualNetwork,
	location string,
	tags map[string]string,
) (*network.Subnet, error) {

	nextAddressPrefix := (*vnet.Properties.AddressSpace.AddressPrefixes)[0]
	if vnet.Properties.Subnets != nil {
		if len(*vnet.Properties.Subnets) == len(*vnet.Properties.AddressSpace.AddressPrefixes) {
			return nil, errors.Errorf(
				"no available address prefixes in vnet %q",
				to.String(vnet.Name),
			)
		}
		addressPrefixesInUse := make(set.Strings)
		for _, subnet := range *vnet.Properties.Subnets {
			addressPrefixesInUse.Add(to.String(subnet.Properties.AddressPrefix))
		}
		for _, addressPrefix := range *vnet.Properties.AddressSpace.AddressPrefixes {
			if !addressPrefixesInUse.Contains(addressPrefix) {
				nextAddressPrefix = addressPrefix
				break
			}
		}
	}

	// Create a network security group for the environment. There is only
	// one NSG per environment (there's a limit of 100 per subscription),
	// in which we manage rules for each exposed machine.
	securityRules := []network.SecurityRule{sshSecurityRule}
	securityGroupParams := network.SecurityGroup{
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(tags),
		Properties: &network.SecurityGroupPropertiesFormat{
			SecurityRules: &securityRules,
		},
	}
	securityGroupClient := network.SecurityGroupsClient{client}
	securityGroupName := internalSecurityGroupName
	logger.Debugf("creating security group %q", securityGroupName)
	_, err := securityGroupClient.CreateOrUpdate(
		resourceGroup, securityGroupName, securityGroupParams,
	)
	if err != nil {
		return nil, errors.Annotatef(err, "creating security group %q", securityGroupName)
	}

	// Now create a subnet with the next available address prefix. The
	// subnet must be created in the controller resource group, as it
	// must be co-located with the vnet.
	subnetName := resourceGroup
	subnetParams := network.Subnet{
		Properties: &network.SubnetPropertiesFormat{
			AddressPrefix: to.StringPtr(nextAddressPrefix),
			// NOTE(axw) we do NOT want to set the network security
			// group as default for the subnet, because that will
			// create a dependency from the controller resource
			// group to environment resource groups. Instead, we
			// set the NSG on NICs.
		},
	}
	logger.Debugf("creating subnet %q (%s)", subnetName, nextAddressPrefix)
	subnetClient := network.SubnetsClient{client}
	subnet, err := subnetClient.CreateOrUpdate(
		controllerResourceGroup, internalNetworkName, subnetName, subnetParams,
	)
	if err != nil {
		return nil, errors.Annotatef(err, "creating subnet %q", subnetName)
	}
	return &subnet, nil
}

// StateServerInstances is specified in the Environ interface.
func (env *azureEnviron) StateServerInstances() ([]instance.Id, error) {
	// State servers are tagged with tags.JujuStateServer, so just
	// list the instances in the controller resource group and pick
	// those ones out.
	instances, err := env.allInstances(env.controllerResourceGroup, true)
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
	baseURI := "https://management.azure.com"
	if strings.Contains(ecfg.location, "china") {
		baseURI = "https://management.chinacloudapi.cn"
	}
	env.compute = compute.NewWithBaseURI(baseURI, env.config.subscriptionId)
	env.resources = resources.NewWithBaseURI(baseURI, env.config.subscriptionId)
	env.storage = storage.NewWithBaseURI(baseURI, env.config.subscriptionId)
	env.network = network.NewWithBaseURI(baseURI, env.config.subscriptionId)
	clients := map[string]*autorest.Client{
		"azure.compute":   &env.compute.Client,
		"azure.resources": &env.resources.Client,
		"azure.storage":   &env.storage.Client,
		"azure.network":   &env.network.Client,
	}
	if env.provider.config.Sender != nil {
		env.config.token.SetSender(env.provider.config.Sender)
	}
	for id, client := range clients {
		client.Authorizer = env.config.token
		logger := loggo.GetLogger(id)
		if env.provider.config.Sender != nil {
			client.Sender = env.provider.config.Sender
		}
		client.ResponseInspector = tracingRespondDecorator(logger)
		client.RequestInspector = tracingPrepareDecorator(logger)
		if env.provider.config.RequestInspector != nil {
			tracer := client.RequestInspector
			inspector := env.provider.config.RequestInspector
			client.RequestInspector = func(p autorest.Preparer) autorest.Preparer {
				p = tracer(p)
				p = inspector(p)
				return p
			}
		}
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
	envTags, _ := env.config.ResourceTags()
	apiPort := env.config.APIPort()
	vmClient := compute.VirtualMachinesClient{env.compute}
	availabilitySetClient := compute.AvailabilitySetsClient{env.compute}
	networkClient := env.network
	vmImagesClient := compute.VirtualMachineImagesClient{env.compute}
	vmExtensionClient := compute.VirtualMachineExtensionsClient{env.compute}
	subscriptionId := env.config.subscriptionId
	imageStream := env.config.ImageStream()
	storageAccountName := env.config.storageAccount
	instanceTypes, err := env.getInstanceTypesLocked()
	if err != nil {
		env.mu.Unlock()
		return nil, errors.Trace(err)
	}
	internalNetworkSubnet, err := env.getInternalSubnetLocked()
	if err != nil {
		env.mu.Unlock()
		return nil, errors.Trace(err)
	}
	env.mu.Unlock()

	// Identify the instance type and image to provision.
	instanceSpec, err := findInstanceSpec(
		vmImagesClient,
		instanceTypes,
		&instances.InstanceConstraint{
			Region:      location,
			Series:      args.Tools.OneSeries(),
			Arches:      args.Tools.Arches(),
			Constraints: args.Constraints,
		},
		imageStream,
	)
	if err != nil {
		return nil, err
	}

	machineTag := names.NewMachineTag(args.InstanceConfig.MachineId)
	vmName := resourceName(machineTag)
	vmTags := make(map[string]string)
	for k, v := range args.InstanceConfig.Tags {
		vmTags[k] = v
	}
	// jujuMachineNameTag identifies the VM name, in which is encoded
	// the Juju machine name. We tag all resources related to the
	// machine with this.
	jujuMachineNameTag := tags.JujuTagPrefix + "machine-name"
	vmTags[jujuMachineNameTag] = vmName

	// If the machine will run a state server, then we need to open the
	// API port for it.
	var apiPortPtr *int
	if multiwatcher.AnyJobNeedsState(args.InstanceConfig.Jobs...) {
		apiPortPtr = &apiPort
	}

	// Construct the network security group ID for the environment.
	nsgID := path.Join(
		"/subscriptions", subscriptionId, "resourceGroups",
		env.resourceGroup, "providers", "Microsoft.Network",
		"networkSecurityGroups", internalSecurityGroupName,
	)

	vm, err := createVirtualMachine(
		env.resourceGroup, location, vmName,
		vmTags, envTags,
		instanceSpec, args.InstanceConfig,
		args.DistributionGroup,
		env.Instances,
		apiPortPtr, internalNetworkSubnet, nsgID,
		storageAccountName, networkClient,
		vmClient, availabilitySetClient,
		vmExtensionClient,
	)
	if err != nil {
		if err := env.destroyVirtualMachine(vmName); err != nil {
			logger.Errorf("could not destroy failed virtual machine: %v", err)
		}
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
	vmTags, envTags map[string]string,
	instanceSpec *instances.InstanceSpec,
	instanceConfig *instancecfg.InstanceConfig,
	distributionGroupFunc func() ([]instance.Id, error),
	instancesFunc func([]instance.Id) ([]instance.Instance, error),
	apiPort *int,
	internalNetworkSubnet *network.Subnet,
	nsgID, storageAccountName string,
	networkClient network.ManagementClient,
	vmClient compute.VirtualMachinesClient,
	availabilitySetClient compute.AvailabilitySetsClient,
	vmExtensionClient compute.VirtualMachineExtensionsClient,
) (compute.VirtualMachine, error) {

	storageProfile, err := newStorageProfile(
		vmName, instanceConfig.Series,
		instanceSpec, location, storageAccountName,
	)
	if err != nil {
		return compute.VirtualMachine{}, errors.Annotate(err, "creating storage profile")
	}

	osProfile, err := newOSProfile(vmName, instanceConfig)
	if err != nil {
		return compute.VirtualMachine{}, errors.Annotate(err, "creating OS profile")
	}

	networkProfile, err := newNetworkProfile(
		networkClient, vmName, apiPort,
		internalNetworkSubnet, nsgID,
		resourceGroup, location, vmTags,
	)
	if err != nil {
		return compute.VirtualMachine{}, errors.Annotate(err, "creating network profile")
	}

	availabilitySetId, err := createAvailabilitySet(
		availabilitySetClient,
		vmName, resourceGroup, location,
		vmTags, envTags,
		distributionGroupFunc, instancesFunc,
	)
	if err != nil {
		return compute.VirtualMachine{}, errors.Annotate(err, "creating availability set")
	}

	vmArgs := compute.VirtualMachine{
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
			AvailabilitySet: &compute.SubResource{
				ID: to.StringPtr(availabilitySetId),
			},
		},
	}
	vm, err := vmClient.CreateOrUpdate(resourceGroup, vmName, vmArgs)
	if err != nil {
		return compute.VirtualMachine{}, errors.Annotate(err, "creating virtual machine")
	}

	// On Windows, we must add the CustomScriptExtension VM extension
	// to run the CustomData script.
	if osProfile.WindowsConfiguration != nil {
		// TODO(axw) see if we can just put this straight in vmArgs.Resources.
		const extensionName = "JujuCustomScriptExtension"
		extensionSettings := map[string]*string{
			"commandToExecute": to.StringPtr(
				"powershell.exe -ExecutionPolicy Unrestricted -File C:\\AzureData\\CustomData.bin",
			),
		}
		_, err := vmExtensionClient.CreateOrUpdate(
			resourceGroup, vmName, extensionName,
			compute.VirtualMachineExtension{
				Location: to.StringPtr(location),
				Tags:     toTagsPtr(vmTags),
				Properties: &compute.VirtualMachineExtensionProperties{
					Publisher:               to.StringPtr("Microsoft.Compute"),
					Type:                    to.StringPtr("CustomScriptExtension"),
					TypeHandlerVersion:      to.StringPtr("1.4"),
					AutoUpgradeMinorVersion: to.BoolPtr(true),
					Settings:                &extensionSettings,
				},
			},
		)
		if err != nil {
			return compute.VirtualMachine{}, errors.Annotate(
				err, "creating CustomScript extension",
			)
		}
	}

	return vm, nil
}

// createAvailabilitySet creates the availability set for a machine to use
// if it doesn't already exist, and returns the availability set's ID. The
// algorithm used for choosing the availability set is:
//  - if there is a distribution group, use the same availability set as
//    the instances in that group. Instances in the group may be in
//    different availability sets (when multiple services colocated on a
//    machine), so we pick one arbitrarily
//  - if there is no distribution group, create an availability name with
//    a name based on the value of the tags.JujuUnitsDeployed tag in vmTags,
//    if it exists
//  - if there are no units assigned to the machine, then use the "juju"
//    availability set
func createAvailabilitySet(
	client compute.AvailabilitySetsClient,
	vmName, resourceGroup, location string,
	vmTags, envTags map[string]string,
	distributionGroupFunc func() ([]instance.Id, error),
	instancesFunc func([]instance.Id) ([]instance.Instance, error),
) (string, error) {
	logger.Debugf("selecting availability set for %q", vmName)

	// First we check if there's a distribution group, and if so,
	// use the availability set of the first instance we find in it.
	var instanceIds []instance.Id
	if distributionGroupFunc != nil {
		var err error
		instanceIds, err = distributionGroupFunc()
		if err != nil {
			return "", errors.Annotate(
				err, "querying distribution group",
			)
		}
	}
	instances, err := instancesFunc(instanceIds)
	switch err {
	case nil, environs.ErrPartialInstances, environs.ErrNoInstances:
	default:
		return "", errors.Annotate(
			err, "querying distribution group instances",
		)
	}
	for _, instance := range instances {
		if instance == nil {
			continue
		}
		instance := instance.(*azureInstance)
		availabilitySetSubResource := instance.Properties.AvailabilitySet
		if availabilitySetSubResource == nil || availabilitySetSubResource.ID == nil {
			continue
		}
		logger.Debugf("- selecting availability set of %q", instance.Name)
		return to.String(availabilitySetSubResource.ID), nil
	}

	// We'll have to create an availability set. Use the name of one of the
	// services assigned to the machine.
	availabilitySetName := "juju"
	if unitNames, ok := vmTags[tags.JujuUnitsDeployed]; ok {
		for _, unitName := range strings.Fields(unitNames) {
			if !names.IsValidUnit(unitName) {
				continue
			}
			serviceName, err := names.UnitService(unitName)
			if err != nil {
				return "", errors.Annotate(
					err, "getting service name",
				)
			}
			availabilitySetName = serviceName
			break
		}
	}

	logger.Debugf("- creating availability set %q", availabilitySetName)
	availabilitySet, err := client.CreateOrUpdate(
		resourceGroup, availabilitySetName, compute.AvailabilitySet{
			Location: to.StringPtr(location),
			// NOTE(axw) we do *not* want to use vmTags here,
			// because an availability set is shared by machines.
			Tags: toTagsPtr(envTags),
		},
	)
	if err != nil {
		return "", errors.Annotatef(
			err, "creating availability set %q", availabilitySetName,
		)
	}
	return to.String(availabilitySet.ID), nil
}

// newStorageProfile creates the storage profile for a virtual machine,
// based on the series and chosen instance spec.
func newStorageProfile(
	vmName string,
	series string,
	instanceSpec *instances.InstanceSpec,
	location, storageAccountName string,
) (*compute.StorageProfile, error) {
	logger.Debugf("creating storage profile for %q", vmName)

	urnParts := strings.SplitN(instanceSpec.Image.Id, ":", 4)
	if len(urnParts) != 4 {
		return nil, errors.Errorf("invalid image ID %q", instanceSpec.Image.Id)
	}
	publisher := urnParts[0]
	offer := urnParts[1]
	sku := urnParts[2]
	version := urnParts[3]

	osDisksRoot := osDiskVhdRoot(location, storageAccountName)
	osDiskName := vmName
	osDisk := &compute.OSDisk{
		Name:         to.StringPtr(osDiskName),
		CreateOption: compute.FromImage,
		Caching:      compute.ReadWrite,
		Vhd: &compute.VirtualHardDisk{
			URI: to.StringPtr(
				osDisksRoot + osDiskName + ".vhd",
			),
		},
	}
	return &compute.StorageProfile{
		ImageReference: &compute.ImageReference{
			Publisher: to.StringPtr(publisher),
			Offer:     to.StringPtr(offer),
			Sku:       to.StringPtr(sku),
			Version:   to.StringPtr(version),
		},
		OsDisk: osDisk,
	}, nil
}

func newOSProfile(vmName string, instanceConfig *instancecfg.InstanceConfig) (*compute.OSProfile, error) {
	logger.Debugf("creating OS profile for %q", vmName)

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
			SSH: &compute.SSHConfiguration{PublicKeys: &publicKeys},
		}
	case os.Windows:
		// Later we will add WinRM configuration here
		// Windows does not accept hostnames over 15 characters.
		osProfile.AdminUsername = to.StringPtr("JujuAdministrator")
		osProfile.WindowsConfiguration = &compute.WindowsConfiguration{
			ProvisionVMAgent:       to.BoolPtr(true),
			EnableAutomaticUpdates: to.BoolPtr(true),
		}
	default:
		return nil, errors.NotSupportedf("%s", seriesOS)
	}
	return osProfile, nil
}

func newNetworkProfile(
	client network.ManagementClient,
	vmName string,
	apiPort *int,
	internalSubnet *network.Subnet,
	nsgID string,
	resourceGroup string,
	location string,
	tags map[string]string,
) (*compute.NetworkProfile, error) {
	logger.Debugf("creating network profile for %q", vmName)

	// Create a public IP for the NIC. Public IP addresses are dynamic.
	logger.Debugf("- allocating public IP address")
	pipClient := network.PublicIPAddressesClient{client}
	publicIPAddressParams := network.PublicIPAddress{
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(tags),
		Properties: &network.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: network.Dynamic,
		},
	}
	publicIPAddressName := vmName + "-public-ip"
	publicIPAddress, err := pipClient.CreateOrUpdate(resourceGroup, publicIPAddressName, publicIPAddressParams)
	if err != nil {
		return nil, errors.Annotatef(err, "creating public IP address for %q", vmName)
	}

	// Determine the next available private IP address.
	nicClient := network.InterfacesClient{client}
	privateIPAddress, err := nextSubnetIPAddress(nicClient, resourceGroup, internalSubnet)
	if err != nil {
		return nil, errors.Annotatef(err, "querying private IP addresses")
	}

	// Create a primary NIC for the machine. This needs to be static, so
	// that we can create security rules that don't become invalid.
	logger.Debugf("- creating primary NIC")
	ipConfigurations := []network.InterfaceIPConfiguration{{
		Name: to.StringPtr("primary"),
		Properties: &network.InterfaceIPConfigurationPropertiesFormat{
			PrivateIPAddress:          to.StringPtr(privateIPAddress),
			PrivateIPAllocationMethod: network.Static,
			Subnet:          &network.SubResource{internalSubnet.ID},
			PublicIPAddress: &network.SubResource{publicIPAddress.ID},
		},
	}}
	primaryNicName := vmName + "-primary"
	primaryNicParams := network.Interface{
		Location: to.StringPtr(location),
		Tags:     toTagsPtr(tags),
		Properties: &network.InterfacePropertiesFormat{
			IPConfigurations: &ipConfigurations,
			// We set the network security group on the NIC, rather
			// than the subnet, to avoid having the controller
			// resource group dependent on the environment resource
			// group.
			NetworkSecurityGroup: &network.SubResource{to.StringPtr(nsgID)},
		},
	}
	primaryNic, err := nicClient.CreateOrUpdate(resourceGroup, primaryNicName, primaryNicParams)
	if err != nil {
		return nil, errors.Annotatef(err, "creating network interface for %q", vmName)
	}

	// Create a network security rule for the machine if we need to open
	// the API server port.
	if apiPort != nil {
		logger.Debugf("- querying network security group")
		securityGroupClient := network.SecurityGroupsClient{client}
		securityGroupName := internalSecurityGroupName
		securityGroup, err := securityGroupClient.Get(resourceGroup, securityGroupName)
		if err != nil {
			return nil, errors.Annotate(err, "querying network security group")
		}

		// NOTE(axw) this looks like TOCTTOU race territory, but it's
		// safe because we only allocate/deallocate rules in this
		// range during machine (de)provisioning, which is managed by
		// a single goroutine. Non-internal ports are managed by the
		// firewaller exclusively.
		nextPriority, err := nextSecurityRulePriority(
			securityGroup,
			securityRuleInternalSSHInbound+1,
			securityRuleInternalMax,
		)
		if err != nil {
			return nil, errors.Trace(err)
		}

		apiSecurityRuleName := fmt.Sprintf("%s-api", vmName)
		apiSecurityRule := network.SecurityRule{
			Name: to.StringPtr(apiSecurityRuleName),
			Properties: &network.SecurityRulePropertiesFormat{
				Description:              to.StringPtr("Allow API access to server machines"),
				Protocol:                 network.SecurityRuleProtocolTCP,
				SourceAddressPrefix:      to.StringPtr("*"),
				SourcePortRange:          to.StringPtr("*"),
				DestinationAddressPrefix: to.StringPtr(privateIPAddress),
				DestinationPortRange:     to.StringPtr(fmt.Sprint(*apiPort)),
				Access:                   network.Allow,
				Priority:                 to.IntPtr(nextPriority),
				Direction:                network.Inbound,
			},
		}
		logger.Debugf("- creating API network security rule")
		securityRuleClient := network.SecurityRulesClient{client}
		_, err = securityRuleClient.CreateOrUpdate(
			resourceGroup, securityGroupName, apiSecurityRuleName, apiSecurityRule,
		)
		if err != nil {
			return nil, errors.Annotate(err, "creating API network security rule")
		}
	}

	// For now we only attach a single, flat network to each machine.
	networkInterfaces := []compute.NetworkInterfaceReference{{
		ID: primaryNic.ID,
		Properties: &compute.NetworkInterfaceReferenceProperties{
			Primary: to.BoolPtr(true),
		},
	}}
	return &compute.NetworkProfile{&networkInterfaces}, nil
}

// StopInstances is specified in the InstanceBroker interface.
func (env *azureEnviron) StopInstances(ids ...instance.Id) error {
	for _, id := range ids {
		if err := env.destroyVirtualMachine(string(id)); err != nil {
			return errors.Annotatef(err, "stopping instance %q", id)
		}
	}
	return nil
}

func (env *azureEnviron) destroyVirtualMachine(vmName string) error {
	// TODO(axw) delete associated resources, e.g. NICs, network
	// security rules, OS disk blobs. This must be done before
	// deleting the machine, or we'll leak resources. Probably
	// have to deallocate the machine first.
	vmClient := compute.VirtualMachinesClient{env.compute}
	result, err := vmClient.Delete(env.resourceGroup, vmName)
	if err != nil && result.StatusCode != http.StatusNotFound {
		return errors.Trace(err)
	}
	return nil
}

// Instances is specified in the Environ interface.
func (env *azureEnviron) Instances(ids []instance.Id) ([]instance.Instance, error) {
	return env.instances(env.resourceGroup, ids, true /* refresh addresses */)
}

func (env *azureEnviron) instances(
	resourceGroup string,
	ids []instance.Id,
	refreshAddresses bool,
) ([]instance.Instance, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// TODO(axw) optimise the len(1) case.
	all, err := env.allInstances(resourceGroup, refreshAddresses)
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
	return env.allInstances(env.resourceGroup, true /* refresh addresses */)
}

// allInstances returns all of the instances in the given resource group,
// and optionally ensures that each instance's addresses are up-to-date.
func (env *azureEnviron) allInstances(
	resourceGroup string,
	refreshAddresses bool,
) ([]instance.Instance, error) {
	env.mu.Lock()
	vmClient := compute.VirtualMachinesClient{env.compute}
	env.mu.Unlock()

	result, err := vmClient.List(resourceGroup)
	if err != nil {
		if result.StatusCode == http.StatusNotFound {
			// This will occur if the resource group does not
			// exist, e.g. in a fresh hosted environment.
			return nil, nil
		}
		return nil, errors.Annotate(err, "listing virtual machines")
	}
	if result.Value == nil || len(*result.Value) == 0 {
		return nil, nil
	}
	instances := make([]instance.Instance, len(*result.Value))
	for i, vm := range *result.Value {
		inst := &azureInstance{vm, nil, nil, env}
		if refreshAddresses {
			if err := inst.refreshAddresses(); err != nil {
				return nil, errors.Trace(err)
			}
		}
		instances[i] = inst
	}
	return instances, nil
}

// Destroy is specified in the Environ interface.
func (env *azureEnviron) Destroy() error {
	logger.Debugf("destroying environment %q", env.envName)
	if err := env.deleteResourceGroup(); err != nil {
		return errors.Trace(err)
	}
	if env.resourceGroup == env.controllerResourceGroup {
		// This is the controller resource group; once it has been
		// deleted, there's nothing left.
		return nil
	}
	if err := env.deleteInternalSubnet(); err != nil {
		return errors.Trace(err)
	}
	return nil
}

func (env *azureEnviron) deleteResourceGroup() error {
	client := resources.GroupsClient{env.resources}
	result, err := client.Delete(env.resourceGroup)
	if err != nil && result.Response.StatusCode != http.StatusNotFound {
		return errors.Annotatef(err, "deleting resource group %q", env.resourceGroup)
	}
	return nil
}

func (env *azureEnviron) deleteInternalSubnet() error {
	client := network.SubnetsClient{env.network}
	subnetName := env.resourceGroup
	result, err := client.Delete(
		env.controllerResourceGroup, internalNetworkName, subnetName,
	)
	if err != nil && result.Response.StatusCode != http.StatusNotFound {
		return errors.Annotatef(err, "deleting subnet %q", subnetName)
	}
	return nil
}

var errNoFwGlobal = errors.New("global firewall mode is not supported")

// OpenPorts is specified in the Environ interface. However, Azure does not
// support the global firewall mode.
func (env *azureEnviron) OpenPorts(ports []jujunetwork.PortRange) error {
	return errNoFwGlobal
}

// ClosePorts is specified in the Environ interface. However, Azure does not
// support the global firewall mode.
func (env *azureEnviron) ClosePorts(ports []jujunetwork.PortRange) error {
	return errNoFwGlobal
}

// Ports is specified in the Environ interface.
func (env *azureEnviron) Ports() ([]jujunetwork.PortRange, error) {
	return nil, errNoFwGlobal
}

// Provider is specified in the Environ interface.
func (env *azureEnviron) Provider() environs.EnvironProvider {
	return env.provider
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
	return fmt.Sprintf(
		"juju-%s-%s", cfg.Name(),
		resourceName(envTag),
	)
}

// resourceName returns the string to use for a resource's Name tag,
// to help users identify Juju-managed resources in the Azure portal.
//
// Since resources are grouped under resource groups, we just use the
// tag.
func resourceName(tag names.Tag) string {
	return tag.String()
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

// getInstanceTypesLocked returns the instance types for Azure, by listing the
// role sizes available to the subscription.
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

// getInternalSubnetLocked queries the internal subnet for the environment.
func (env *azureEnviron) getInternalSubnetLocked() (*network.Subnet, error) {
	client := network.SubnetsClient{env.network}
	vnetName := internalNetworkName
	subnetName := env.resourceGroup
	subnet, err := client.Get(env.controllerResourceGroup, vnetName, subnetName)
	if err != nil {
		return nil, errors.Annotate(err, "getting internal subnet")
	}
	return &subnet, nil
}

// nextSecurityRulePriority returns the next available priority in the given
// security group within a specified range.
func nextSecurityRulePriority(group network.SecurityGroup, min, max int) (int, error) {
	if group.Properties.SecurityRules == nil {
		return min, nil
	}
	for p := min; p <= min; p++ {
		var found bool
		for _, rule := range *group.Properties.SecurityRules {
			if to.Int(rule.Properties.Priority) == p {
				found = true
				break
			}
		}
		if !found {
			return p, nil
		}
	}
	return -1, errors.Errorf(
		"no priorities available in the range [%d, %d]",
		securityRuleInternalMin,
		securityRuleInternalMax,
	)
}

// nextSubnetIPAddress returns the next available IP address in the given subnet.
func nextSubnetIPAddress(
	nicClient network.InterfacesClient,
	resourceGroup string,
	subnet *network.Subnet,
) (string, error) {
	_, ipnet, err := net.ParseCIDR(to.String(subnet.Properties.AddressPrefix))
	if err != nil {
		return "", errors.Annotate(err, "parsing subnet prefix")
	}
	results, err := nicClient.List(resourceGroup)
	if err != nil {
		return "", errors.Annotate(err, "listing NICs")
	}
	// Azure reserves the first 4 addresses in the subnet.
	var ipsInUse []net.IP
	if results.Value != nil {
		ipsInUse = make([]net.IP, 0, len(*results.Value))
		for _, item := range *results.Value {
			if item.Properties.IPConfigurations == nil {
				continue
			}
			for _, ipConfiguration := range *item.Properties.IPConfigurations {
				if to.String(ipConfiguration.Properties.Subnet.ID) != to.String(subnet.ID) {
					continue
				}
				ip := net.ParseIP(to.String(ipConfiguration.Properties.PrivateIPAddress))
				if ip != nil {
					ipsInUse = append(ipsInUse, ip)
				}
			}
		}
	}
	ip, err := azureutils.NextSubnetIP(ipnet, ipsInUse)
	if err != nil {
		return "", errors.Trace(err)
	}
	return ip.String(), nil
}

// getStorageClient queries the storage account key, and uses it to construct
// a new storage client.
func (env *azureEnviron) getStorageClient() (internalazurestorage.Client, error) {
	env.mu.Lock()
	defer env.mu.Unlock()
	client, err := getStorageClient(env.provider.config.NewStorageClient, env.config)
	if err != nil {
		return nil, errors.Annotate(err, "getting storage client")
	}
	return client, nil
}
