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

	// RequestInspector will be used to inspect Azure requests
	// if it is non-nil.
	RequestInspector autorest.PrepareDecorator
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
//
// The result of RestrictedConfigAttributes is the names of attributes that
// will be copied across to a hosted environment's initial configuration.
func (prov *azureEnvironProvider) RestrictedConfigAttributes() []string {
	return []string{
		configAttrSubscriptionId,
		configAttrTenantId,
		configAttrClientId,
		configAttrClientKey,
		configAttrLocation,
		configAttrControllerResourceGroup,
		configAttrStorageAccountType,
	}
}

// PrepareForCreateEnvironment is specified in the EnvironProvider interface.
func (prov *azureEnvironProvider) PrepareForCreateEnvironment(cfg *config.Config) (*config.Config, error) {
	if _, ok := cfg.UUID(); !ok {
		// TODO(axw) PrepareForCreateEnvironment is called twice; once before
		// the UUID is set, and once after. It probably should just be called
		// after?
		return cfg, nil
	}
	env, err := newEnviron(prov, cfg)
	if err != nil {
		return nil, errors.Annotate(err, "opening environment")
	}
	return env.initResourceGroup()
}

// PrepareForBootstrap is specified in the EnvironProvider interface.
func (prov *azureEnvironProvider) PrepareForBootstrap(ctx environs.BootstrapContext, cfg *config.Config) (environs.Environ, error) {
	// Ensure that internal configuration is not specified, and then set
	// what we can now. We only need to do this during bootstrap. Validate
	// will check for changes later.
	unknownAttrs := cfg.UnknownAttrs()
	for _, key := range internalConfigAttributes {
		if _, ok := unknownAttrs[key]; ok {
			return nil, errors.Errorf(`internal config %q must not be specified`, key)
		}
	}

	// Record the UUID that will be used for the controller environment.
	cfg, err := cfg.Apply(map[string]interface{}{
		"controller-resource-group": resourceGroupName(cfg),
	})
	if err != nil {
		return nil, errors.Annotate(err, "recording controller-resource-group")
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
	e.mu.Lock()
	defer e.mu.Unlock()
	// TODO(axw) user-friendly error message
	return e.config.token.EnsureFresh()
}
