// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"strings"

	"gopkg.in/juju/environschema.v1"

	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/azure-sdk-for-go/arm/storage"
	"github.com/juju/errors"
	"github.com/juju/schema"

	"github.com/juju/juju/environs/config"
)

const (
	configAttrAppId              = "application-id"
	configAttrSubscriptionId     = "subscription-id"
	configAttrTenantId           = "tenant-id"
	configAttrAppKey             = "application-key"
	configAttrLocation           = "location"
	configAttrStorageAccountType = "storage-account-type"

	// The below bits are internal book-keeping things, rather than
	// configuration. Config is just what we have to work with.

	// configAttrStorageAccount is the name of the storage account. We
	// can't just use a well-defined name for the storage acocunt because
	// storage account names must be globally unique; each storage account
	// has an associated public DNS entry.
	configAttrStorageAccount = "storage-account"

	// configAttrStorageAccountKey is the primary key for the storage
	// account.
	configAttrStorageAccountKey = "storage-account-key"

	// configAttrControllerResourceGroup is the resource group
	// corresponding to the controller environment. Each environment needs
	// to know this because some resources are shared, and live in the
	// controller environment's resource group.
	configAttrControllerResourceGroup = "controller-resource-group"
)

var configSchema = environschema.Fields{
	// User-configurable attributes.
	configAttrLocation: {
		Description: "The Azure data center location",
		Type:        environschema.Tstring,
		Immutable:   true,
		Mandatory:   true,
		Example:     "West US",
		// TODO(axw) enumerate locations?
	},
	configAttrStorageAccountType: {
		Description: "The Azure storage account type",
		Type:        environschema.Tstring,
		Immutable:   true,
		Mandatory:   true,
		Example:     string(storage.StandardLRS),
		Values: []interface{}{
			string(storage.StandardLRS),
			string(storage.StandardGRS),
			string(storage.StandardRAGRS),
			string(storage.StandardZRS),
			string(storage.PremiumLRS),
		},
	},

	// Internal configuration.
	configAttrStorageAccount: {
		Description: "The environment's storage account name",
		Type:        environschema.Tstring,
		Group:       environschema.JujuGroup,
		Immutable:   true,
	},
	configAttrStorageAccountKey: {
		Description: "The environment's storage account key",
		Type:        environschema.Tstring,
		Group:       environschema.JujuGroup,
		Immutable:   true,
		Secret:      true,
	},
	configAttrControllerResourceGroup: {
		Description: "The name of the Juju controller environment's resource group",
		Type:        environschema.Tstring,
		Group:       environschema.JujuGroup,
		Immutable:   true,
	},

	// Credentials.
	configAttrAppId: {
		Description: "The application ID created in Azure Active Directory for Juju to use",
		Type:        environschema.Tstring,
		Group:       environschema.AccountGroup,
		Mandatory:   true,
		Secret:      true,
	},
	configAttrAppKey: {
		Description: "The password for the application created in Azure Active Directory",
		Type:        environschema.Tstring,
		Group:       environschema.AccountGroup,
		Mandatory:   true,
		Secret:      true,
	},
	configAttrSubscriptionId: {
		Description: "The ID of the account subscription to manage resources in",
		Type:        environschema.Tstring,
		Group:       environschema.AccountGroup,
		Immutable:   true,
		Mandatory:   true,
		Secret:      true,
	},
	configAttrTenantId: {
		Description: "The ID of the Azure tenant, which identifies the Azure Active Directory instance",
		Type:        environschema.Tstring,
		Group:       environschema.AccountGroup,
		Immutable:   true,
		Mandatory:   true,
		Secret:      true,
	},
}

var configFields, configDefaults = func() (schema.Fields, schema.Defaults) {
	fields, defaults, err := configSchema.ValidationSchema()
	if err != nil {
		panic(err)
	}
	defaults[configAttrStorageAccountType] = string(storage.StandardLRS)
	return fields, defaults
}()

var requiredConfigAttributes = []string{
	configAttrAppId,
	configAttrAppKey,
	configAttrSubscriptionId,
	configAttrTenantId,
	configAttrLocation,
	configAttrControllerResourceGroup,
}

var immutableConfigAttributes = []string{
	configAttrSubscriptionId,
	configAttrTenantId,
	configAttrControllerResourceGroup,
	configAttrStorageAccount,
	configAttrStorageAccountType,
}

var internalConfigAttributes = []string{
	configAttrStorageAccount,
	configAttrStorageAccountKey,
	configAttrControllerResourceGroup,
}

type azureEnvironConfig struct {
	*config.Config
	token                   *azure.ServicePrincipalToken
	subscriptionId          string
	location                string // canonicalized
	storageAccount          string
	storageAccountKey       string
	storageAccountType      storage.AccountType
	controllerResourceGroup string
}

func (prov *azureEnvironProvider) newConfig(cfg *config.Config) (*azureEnvironConfig, error) {
	azureConfig, err := validateConfig(cfg, nil)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return azureConfig, nil
}

// Validate ensures that the provided configuration is valid for this
// provider, and that changes between the old (if provided) and new
// configurations are valid.
func (*azureEnvironProvider) Validate(newCfg, oldCfg *config.Config) (*config.Config, error) {
	_, err := validateConfig(newCfg, oldCfg)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return newCfg, nil
}

func validateConfig(newCfg, oldCfg *config.Config) (*azureEnvironConfig, error) {
	err := config.Validate(newCfg, oldCfg)
	if err != nil {
		return nil, err
	}

	validated, err := newCfg.ValidateUnknownAttrs(configFields, configDefaults)
	if err != nil {
		return nil, err
	}

	// Ensure required configuration is provided.
	for _, key := range requiredConfigAttributes {
		if value, ok := validated[key].(string); !ok || value == "" {
			return nil, errors.Errorf("%q config not specified", key)
		}
	}
	if oldCfg != nil {
		// Ensure immutable configuration isn't changed.
		oldUnknownAttrs := oldCfg.UnknownAttrs()
		for _, key := range immutableConfigAttributes {
			oldValue, hadValue := oldUnknownAttrs[key].(string)
			if hadValue {
				newValue, haveValue := validated[key].(string)
				if !haveValue {
					return nil, errors.Errorf(
						"cannot remove immutable %q config", key,
					)
				}
				if newValue != oldValue {
					return nil, errors.Errorf(
						"cannot change immutable %q config (%v -> %v)",
						key, oldValue, newValue,
					)
				}
			}
			// It's valid to go from not having to having.
		}
		// TODO(axw) figure out how we intend to handle changing
		// secrets, such as application key
	}

	location := canonicalLocation(validated[configAttrLocation].(string))
	appId := validated[configAttrAppId].(string)
	subscriptionId := validated[configAttrSubscriptionId].(string)
	tenantId := validated[configAttrTenantId].(string)
	appKey := validated[configAttrAppKey].(string)
	storageAccount, _ := validated[configAttrStorageAccount].(string)
	storageAccountKey, _ := validated[configAttrStorageAccountKey].(string)
	storageAccountType := validated[configAttrStorageAccountType].(string)
	controllerResourceGroup := validated[configAttrControllerResourceGroup].(string)

	if newCfg.FirewallMode() == config.FwGlobal {
		// We do not currently support the "global" firewall mode.
		return nil, errNoFwGlobal
	}

	token, err := azure.NewServicePrincipalToken(
		appId, appKey, tenantId,
		azure.AzureResourceManagerScope,
	)
	if err != nil {
		return nil, errors.Annotate(err, "constructing service principal token")
	}

	azureConfig := &azureEnvironConfig{
		newCfg,
		token,
		subscriptionId,
		location,
		storageAccount,
		storageAccountKey,
		storage.AccountType(storageAccountType),
		controllerResourceGroup,
	}

	return azureConfig, nil
}

// canonicalLocation returns the canonicalized location string. This involves
// stripping whitespace, and lowercasing. The ARM APIs do not support embedded
// whitespace, whereas the old Service Management APIs used to; we allow the
// user to provide either, and canonicalize them to one form that ARM allows.
func canonicalLocation(s string) string {
	s = strings.Replace(s, " ", "", -1)
	return strings.ToLower(s)
}

// TODO(axw) update with prose re credentials
var boilerplateYAML = `
# https://juju.ubuntu.com/docs/config-azure.html
azure:
    type: azure

    # NOTE: below we refer to the "Azure CLI", which is a CLI for Azure
    # provided by Microsoft. Please see the documentation for this at:
    #   https://azure.microsoft.com/en-us/documentation/articles/xplat-cli/

    # application-id is the ID of an application you create in Azure Active
    # Directory for Juju to use. For instructions on how to do this, see:
    #   https://azure.microsoft.com/en-us/documentation/articles/resource-group-authenticate-service-principal
    application-id: 00000000-0000-0000-0000-000000000000

    # application-key is the password specified when creating the application
    # in Azure Active Directory.
    application-password: XXX

    # subscription-id defines the Azure account subscription ID to
    # manage resources in. You can list your account subscriptions
    # with the Azure CLI's "account list" action: "azure account list".
    # The ID associated with each account is the subscription ID.
    subscription-id: 00000000-0000-0000-0000-000000000000

    # tenant-id is the ID of the Azure tenant, which identifies the Azure
    # Active Directory instance. You can obtain this ID by using the Azure
    # CLI's "account show" action. First list your accounts with
    # "azure account list", and then feed the account ID to
    # "azure account show" to obtain the properties of the account, including
    # the tenant ID.
    tenant-id: 00000000-0000-0000-0000-000000000000

    # storage-account-type specifies the type of the storage account,
    # which defines the replication strategy and support for different
    # disk types.
    storage-account-type: Standard_LRS

    # location specifies the Azure data center ("location") where
    # instances will be started, for example: West US, North Europe.
    location: West US

    # image-stream chooses an stream from which to select OS images. This
    # can be "released" (default), or "daily".
    #
    # image-stream: "released"

    # agent-stream chooses a simplestreams stream from which to select tools,
    # for example released or proposed tools (or any other stream available
    # on simplestreams).
    #
    # agent-stream: "released"

    # Whether or not to refresh the list of available updates for an
    # OS. The default option of true is recommended for use in
    # production systems, but disabling this can speed up local
    # deployments for development or testing.
    #
    # enable-os-refresh-update: true

    # Whether or not to perform OS upgrades when machines are
    # provisioned. The default option of true is recommended for use
    # in production systems, but disabling this can speed up local
    # deployments for development or testing.
    #
    # enable-os-upgrade: true

`[1:]
