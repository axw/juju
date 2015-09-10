// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"strings"

	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/juju/errors"
	"github.com/juju/schema"

	"github.com/juju/juju/environs/config"
)

const (
	configAttrClientId       = "client-id"
	configAttrSubscriptionId = "subscription-id"
	configAttrTenantId       = "tenant-id"
	configAttrClientKey      = "client-key"
	configAttrLocation       = "location"
	configAttrStorageAccount = "storage-account"
)

var configFields = schema.Fields{
	configAttrLocation:       schema.String(),
	configAttrClientId:       schema.String(),
	configAttrSubscriptionId: schema.String(),
	configAttrTenantId:       schema.String(),
	configAttrClientKey:      schema.String(),
	configAttrStorageAccount: schema.String(),
}
var configDefaults = schema.Defaults{
	configAttrStorageAccount: schema.Omit,
}

type azureEnvironConfig struct {
	*config.Config
	token          *azure.ServicePrincipalToken
	subscriptionId string
	location       string
	storageAccount string
}

func (prov azureEnvironProvider) newConfig(cfg *config.Config) (*azureEnvironConfig, error) {
	azureConfig, err := validateConfig(cfg, nil)
	if err != nil {
		return nil, errors.Trace(err)
	}
	return azureConfig, nil
}

// Validate ensures that the provided configuration is valid for this
// provider, and that changes between the old (if provided) and new
// configurations are valid.
func (azureEnvironProvider) Validate(newCfg, oldCfg *config.Config) (*config.Config, error) {
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

	location := canonicalLocation(validated[configAttrLocation].(string))
	clientId := validated[configAttrClientId].(string)
	subscriptionId := validated[configAttrSubscriptionId].(string)
	tenantId := validated[configAttrTenantId].(string)
	clientKey := validated[configAttrClientKey].(string)
	storageAccount, haveStorageAccount := validated[configAttrStorageAccount].(string)

	if oldCfg != nil {
		oldUnknownAttrs := oldCfg.UnknownAttrs()
		if haveStorageAccount {
			oldStorageAccount, ok := oldUnknownAttrs[configAttrStorageAccount]
			if ok && storageAccount != oldStorageAccount {
				return nil, errors.Errorf("cannot change storage account")
			}
		}
	}

	token, err := azure.NewServicePrincipalToken(
		clientId,
		clientKey,
		tenantId,
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

    # Credentials
    client-id: 00000000-0000-0000-0000-000000000000
    tenant-id: 00000000-0000-0000-0000-000000000000
    client-key: XXX

    subscription-id: 00000000-0000-0000-0000-000000000000

    # location specifies the place where instances will be started,
    # for example: West US, North Europe.
    #
    location: West US

    # image-stream chooses a simplestreams stream from which to select
    # OS images, for example daily or released images (or any other stream
    # available on simplestreams).
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
