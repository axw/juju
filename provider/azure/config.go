// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"encoding/pem"
	"fmt"

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
)

var configFields = schema.Fields{
	configAttrLocation:       schema.String(),
	configAttrClientId:       schema.String(),
	configAttrSubscriptionId: schema.String(),
	configAttrTenantId:       schema.String(),
	configAttrClientKey:      schema.String(),
}
var configDefaults = schema.Defaults{
	"location": "",
}

type azureEnvironConfig struct {
	*config.Config
	token          *azure.ServicePrincipalToken
	subscriptionId string
	location       string
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

	// TODO(axw) ensure default is set correctly, so omitting location
	// or setting it to "" will cause an error.
	location := validated[configAttrLocation]

	// TODO(axw) ensure the below must be set, and omitting will cause an error.
	clientId := validated[configAttrClientId]
	subscriptionId := validated[configAttrSubscriptionId]
	tenantId := validated[configAttrTenantId]
	clientKey := validated[configAttrClientKey]

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
	}

	return azureConfig, nil
}

// Validate ensures that config is a valid configuration for this
// provider like specified in the EnvironProvider interface.
func (prov azureEnvironProvider) Validate(cfg, oldCfg *config.Config) (*config.Config, error) {
	err := config.Validate(cfg, oldCfg)
	if err != nil {
		return nil, err
	}

	validated, err := cfg.ValidateUnknownAttrs(configFields, configDefaults)
	if err != nil {
		return nil, err
	}
	envCfg := new(azureEnvironConfig)
	envCfg.Config = cfg
	envCfg.attrs = validated

	cert := envCfg.managementCertificate()
	if cert == "" {
		certPath := envCfg.attrs["management-certificate-path"].(string)
		pemData, err := readPEMFile(certPath)
		if err != nil {
			return nil, fmt.Errorf("invalid management-certificate-path: %s", err)
		}
		envCfg.attrs["management-certificate"] = string(pemData)
	} else {
		if block, _ := pem.Decode([]byte(cert)); block == nil {
			return nil, fmt.Errorf("invalid management-certificate: not a PEM encoded certificate")
		}
	}
	delete(envCfg.attrs, "management-certificate-path")

	if envCfg.location() == "" {
		return nil, fmt.Errorf("environment has no location; you need to set one.  E.g. 'West US'")
	}
	return cfg.Apply(envCfg.attrs)
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
