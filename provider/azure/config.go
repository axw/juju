// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/juju/schema"

	"github.com/juju/juju/environs/config"
)

var configFields = schema.Fields{
	"location":                    schema.String(),
	"management-subscription-id":  schema.String(),
	"management-certificate-path": schema.String(),
	"management-certificate":      schema.String(),
	"storage-account-name":        schema.String(),
}
var configDefaults = schema.Defaults{
	"location":                    "",
	"management-certificate":      "",
	"management-certificate-path": "",
}

type azureEnvironConfig struct {
	*config.Config
	attrs map[string]interface{}
}

func (cfg *azureEnvironConfig) location() string {
	return cfg.attrs["location"].(string)
}

func (cfg *azureEnvironConfig) managementSubscriptionId() string {
	return cfg.attrs["management-subscription-id"].(string)
}

func (cfg *azureEnvironConfig) managementCertificate() string {
	return cfg.attrs["management-certificate"].(string)
}

func (cfg *azureEnvironConfig) storageAccountName() string {
	return cfg.attrs["storage-account-name"].(string)
}

func (prov azureEnvironProvider) newConfig(cfg *config.Config) (*azureEnvironConfig, error) {
	validCfg, err := prov.Validate(cfg, nil)
	if err != nil {
		return nil, err
	}
	result := new(azureEnvironConfig)
	result.Config = validCfg
	result.attrs = validCfg.UnknownAttrs()
	return result, nil
}

// Validate ensures that config is a valid configuration for this
// provider like specified in the EnvironProvider interface.
func (prov azureEnvironProvider) Validate(cfg, oldCfg *config.Config) (*config.Config, error) {
	// Validate base configuration change before validating Azure specifics.
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

func readPEMFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// 640K ought to be enough for anybody.
	data, err := ioutil.ReadAll(io.LimitReader(f, 1024*640))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%q is not a PEM encoded certificate file", path)
	}
	return data, nil
}

var boilerplateYAML = `
# https://juju.ubuntu.com/docs/config-azure.html
azure:
    type: azure

    # location specifies the place where instances will be started,
    # for example: West US, North Europe.
    #
    location: West US

    # The following attributes specify Windows Azure Management
    # information. See:
    # http://msdn.microsoft.com/en-us/library/windowsazure
    # for details.
    #
    management-subscription-id: 00000000-0000-0000-0000-000000000000
    management-certificate-path: /home/me/azure.pem

    # storage-account-name holds Windows Azure Storage info.
    #
    storage-account-name: abcdefghijkl

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
