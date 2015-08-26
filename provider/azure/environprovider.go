// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
)

// Logger for the Azure provider.
var logger = loggo.GetLogger("juju.provider.azure")

type azureEnvironProvider struct{}

// azureEnvironProvider implements EnvironProvider.
var _ environs.EnvironProvider = (*azureEnvironProvider)(nil)

// Open is specified in the EnvironProvider interface.
func (prov azureEnvironProvider) Open(cfg *config.Config) (environs.Environ, error) {
	logger.Debugf("opening environment %q.", cfg.Name())
	// We can't return NewEnviron(cfg) directly here because otherwise,
	// when err is not nil, we end up with a non-nil returned environ and
	// this breaks the loop in cmd/jujud/upgrade.go:run() (see
	// http://golang.org/doc/faq#nil_error for the gory details).
	environ, err := NewEnviron(cfg)
	if err != nil {
		return nil, err
	}
	return environ, nil
}

// RestrictedConfigAttributes is specified in the EnvironProvider interface.
func (prov azureEnvironProvider) RestrictedConfigAttributes() []string {
	return []string{"location"}
}

// PrepareForCreateEnvironment is specified in the EnvironProvider interface.
func (p azureEnvironProvider) PrepareForCreateEnvironment(cfg *config.Config) (*config.Config, error) {
	return cfg, nil
}

// PrepareForBootstrap is specified in the EnvironProvider interface.
func (prov azureEnvironProvider) PrepareForBootstrap(ctx environs.BootstrapContext, cfg *config.Config) (environs.Environ, error) {
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
func (prov azureEnvironProvider) BoilerplateConfig() string {
	return boilerplateYAML
}

// SecretAttrs is specified in the EnvironProvider interface.
func (prov azureEnvironProvider) SecretAttrs(cfg *config.Config) (map[string]string, error) {
	secretAttrs := make(map[string]string)
	azureCfg, err := prov.newConfig(cfg)
	if err != nil {
		return nil, err
	}
	secretAttrs["management-certificate"] = azureCfg.managementCertificate()
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
