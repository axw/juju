// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"github.com/Azure/azure-sdk-for-go/Godeps/_workspace/src/github.com/Azure/go-autorest/autorest"
	"github.com/juju/errors"
	"github.com/juju/loggo"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
)

// Logger for the Azure provider.
var logger = loggo.GetLogger("juju.provider.azure")

// EnvironProviderConfig contains configuration for the Azure provider.
type EnvironProviderConfig struct {
	// Sender is the autorest.Sender that will be used by Azure
	// clients. If sender is nil, the default HTTP client sender
	// will be used.
	Sender autorest.Sender
}

// Validate validates the Azure environ provider configuration.
func (EnvironProviderConfig) Validate() error {
	return nil
}

type azureEnvironProvider struct {
	config EnvironProviderConfig
}

// NewEnvironProvider returns a new EnvironProvider for Azure.
func NewEnvironProvider(config EnvironProviderConfig) (environs.EnvironProvider, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Annotate(err, "validating environ provider configuration")
	}
	return &azureEnvironProvider{config}, nil
}

// Open is specified in the EnvironProvider interface.
func (prov *azureEnvironProvider) Open(cfg *config.Config) (environs.Environ, error) {
	logger.Debugf("opening environment %q", cfg.Name())
	environ, err := newEnviron(prov, cfg)
	if err != nil {
		return nil, errors.Annotate(err, "opening environment")
	}
	return environ, nil
}

// RestrictedConfigAttributes is specified in the EnvironProvider interface.
func (prov *azureEnvironProvider) RestrictedConfigAttributes() []string {
	return []string{
		configAttrClientId,
		configAttrSubscriptionId,
		configAttrTenantId,
		configAttrClientKey,
		configAttrLocation,
	}
}

// PrepareForCreateEnvironment is specified in the EnvironProvider interface.
func (p *azureEnvironProvider) PrepareForCreateEnvironment(cfg *config.Config) (*config.Config, error) {
	return cfg, nil
}

// PrepareForBootstrap is specified in the EnvironProvider interface.
func (prov *azureEnvironProvider) PrepareForBootstrap(ctx environs.BootstrapContext, cfg *config.Config) (environs.Environ, error) {
	cfg, err := prov.PrepareForCreateEnvironment(cfg)
	if err != nil {
		return nil, errors.Trace(err)
	}
	env, err := prov.Open(cfg)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if ctx.ShouldVerifyCredentials() {
		if err := verifyCredentials(env.(*azureEnviron)); err != nil {
			return nil, errors.Trace(err)
		}
	}
	return env, nil
}

// BoilerplateProvider is specified in the EnvironProvider interface.
func (prov *azureEnvironProvider) BoilerplateConfig() string {
	return boilerplateYAML
}

// SecretAttrs is specified in the EnvironProvider interface.
func (prov *azureEnvironProvider) SecretAttrs(cfg *config.Config) (map[string]string, error) {
	secretAttrs := map[string]string{
		configAttrClientKey: cfg.UnknownAttrs()[configAttrClientKey].(string),
	}
	return secretAttrs, nil
}

// verifyCredentials issues a cheap, non-modifying request to Azure to
// verify the configured credentials. If verification fails, a user-friendly
// error will be returned, and the original error will be logged at debug
// level.
var verifyCredentials = func(e *azureEnviron) error {
	// TODO(axw) verify credentials
	return nil
}
