// Copyright 2011, 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environs

import (
	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/instance"
	"github.com/juju/juju/network"
	"github.com/juju/juju/state"
)

type ExternalEnviron struct {
}

func (e *ExternalEnviron) Bootstrap(ctx BootstrapContext, params BootstrapParams) (*BootstrapResult, error) {
}

// TODO(axw) InstanceBroker methods

func (e *ExternalEnviron) Config() *config.Config {
	// TODO(axw) just return config from Open/Bootstrap?
	// other calls need ability to update config...
}

	// EnvironCapability allows access to this environment's capabilities.
	state.EnvironCapability

	// ConstraintsValidator returns a Validator instance which
	// is used to validate and merge constraints.
	ConstraintsValidator() (constraints.Validator, error)

	// SetConfig updates the Environ's configuration.
	//
	// Calls to SetConfig do not affect the configuration of
	// values previously obtained from Storage.
	SetConfig(cfg *config.Config) error

	// Instances returns a slice of instances corresponding to the
	// given instance ids.  If no instances were found, but there
	// was no other error, it will return ErrNoInstances.  If
	// some but not all the instances were found, the returned slice
	// will have some nil slots, and an ErrPartialInstances error
	// will be returned.
	Instances(ids []instance.Id) ([]instance.Instance, error)

	// ControllerInstances returns the IDs of instances corresponding
	// to Juju controllers. If there are no controller instances,
	// ErrNoInstances is returned. If it can be determined that the
	// environment has not been bootstrapped, then ErrNotBootstrapped
	// should be returned instead.
	ControllerInstances() ([]instance.Id, error)

	// Destroy shuts down all known machines and destroys the
	// rest of the environment. Note that on some providers,
	// very recently started instances may not be destroyed
	// because they are not yet visible.
	//
	// If the Environ represents the controller model, then this
	// method should also destroy any resources relating to hosted
	// models. This ensures that "kill-controller" can clean up
	// hosted models when the Juju controller process is unavailable.
	//
	// When Destroy has been called, any Environ referring to the
	// same remote environment may become invalid.
	Destroy() error

	Firewaller

	// Provider returns the EnvironProvider that created this Environ.
	Provider() EnvironProvider

	state.Prechecker
}

// Firewaller exposes methods for managing network ports.
type Firewaller interface {
	// OpenPorts opens the given port ranges for the whole environment.
	// Must only be used if the environment was setup with the
	// FwGlobal firewall mode.
	OpenPorts(ports []network.PortRange) error

	// ClosePorts closes the given port ranges for the whole environment.
	// Must only be used if the environment was setup with the
	// FwGlobal firewall mode.
	ClosePorts(ports []network.PortRange) error

	// Ports returns the port ranges opened for the whole environment.
	// Must only be used if the environment was setup with the
	// FwGlobal firewall mode.
	Ports() ([]network.PortRange, error)
}
