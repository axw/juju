package environs

import (
	"fmt"
	"launchpad.net/juju-core/environs/config"
)

// Open creates a new Environ using the environment configuration with the
// given name. If name is empty, the default environment will be used.
func (envs *Environs) Open(name string) (Environ, error) {
	if name == "" {
		name = envs.Default
		if name == "" {
			return nil, fmt.Errorf("no default environment found")
		}
	}
	e, ok := envs.environs[name]
	if !ok {
		return nil, fmt.Errorf("unknown environment %q", name)
	}
	if e.err != nil {
		return nil, e.err
	}
	return New(e.config)
}

// New returns a new environment based on the provided configuration.
// The configuration is validated for the respective provider before
// the environment is instantiated.
func New(config *config.Config) (Environ, error) {
	p, ok := providers[config.Type()]
	if !ok {
		return nil, fmt.Errorf("no registered provider for %q", config.Type())
	}
	return p.Open(config)
}

// New returns a new environment based on the provided configuration
// attributes. The configuration is validated for the respective provider
// before the environment is instantiated.
func NewFromAttrs(attrs map[string]interface{}) (Environ, error) {
	cfg, err := config.New(attrs)
	if err != nil {
		return nil, err
	}
	return New(cfg)
}
