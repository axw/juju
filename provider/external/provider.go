package external

import (
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
)

type ExternalProvider struct {
}

func (p *ExternalProvider) RestrictedConfigAttributes() []string {
	// TODO(axw) call external process.
	return []string{}
}

func (p *ExternalProvider) PrepareForCreateEnvironment(cfg *config.Config) (*config.Config, error) {
}

func (p *ExternalProvider) PrepareForBootstrap(ctx environs.BootstrapContext, cfg *config.Config) (environs.Environ, error) {
}

func (p *ExternalProvider) BootstrapConfig(environs.BootstrapConfigParams) (*config.Config, error) {
}

func (p *ExternalProvider) Open(cfg *config.Config) (environs.Environ, error) {
}

func (p *ExternalProvider) Validate(cfg, old *config.Config) (valid *config.Config, err error) {
}

func (p *ExternalProvider) SecretAttrs(cfg *config.Config) (map[string]string, error) {
}

func (p *ExternalProvider) CredentialSchemas() map[cloud.AuthType]cloud.CredentialSchema {
}

func (p *ExternalProvider) DetectCredentials() (*cloud.CloudCredential, error) {
}
