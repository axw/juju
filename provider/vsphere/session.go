// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package vsphere

import (
	"context"

	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/provider/common"
	"github.com/juju/juju/storage"
)

// sessionEnviron implements common.ZonedEnviron. An instance of
// sessionEnviron is scoped to the context of a single exported
// method call.
//
// NOTE(axw) this type's methods are not safe for concurrent use.
// It is the responsibility of the environ type to ensure that
// there are no concurrent calls to sessionEnviron's methods.
type sessionEnviron struct {
	ctx    context.Context
	client Client

	// env is the environ that created this sessionEnviron.
	// Take care to synchronise access to env's attributes
	// and methods as necessary.
	env *environ

	// zones caches the results of AvailabilityZones to reduce
	// the number of API calls required by StartInstance.
	// We only cache per session, so there is no issue of
	// staleness.
	zones []common.AvailabilityZone
}

func (env *environ) withSession(f func(*sessionEnviron) error) error {
	ctx := context.Background()
	return env.withClient(ctx, func(client Client) error {
		return f(&sessionEnviron{
			ctx:    ctx,
			client: client,
			env:    env,
		})
	})
}

// NOTE(axw) trivial forwarding methods only should be found below.
// Please keep the business logic for other methods together, to
// make the cost and relationship between environ and sessionEnviron
// methods clear.

// Name is part of the environs.Environ interface.
func (env *sessionEnviron) Name() string {
	return env.env.Name()
}

// Provider is part of the environs.Environ interface.
func (env *sessionEnviron) Provider() environs.EnvironProvider {
	return env.env.Provider()
}

// SetConfig is part of the environs.Environ interface.
func (env *sessionEnviron) SetConfig(cfg *config.Config) error {
	return env.env.SetConfig(cfg)
}

// Config is part of the environs.Environ interface.
func (env *sessionEnviron) Config() *config.Config {
	return env.env.Config()
}

// PrepareForBootstrap is part of the environs.Environ interface.
func (env *sessionEnviron) PrepareForBootstrap(ctx environs.BootstrapContext) error {
	return env.env.PrepareForBootstrap(ctx)
}

// StorageProviderTypes is part of the storage.ProviderRegistry interface.
func (env *sessionEnviron) StorageProviderTypes() ([]storage.ProviderType, error) {
	return env.env.StorageProviderTypes()
}

// StorageProvider is part of the storage.ProviderRegistry interface.
func (env *sessionEnviron) StorageProvider(t storage.ProviderType) (storage.Provider, error) {
	return env.env.StorageProvider(t)
}

// InstanceTypes is part of the the environs.Environ interface.
func (env *sessionEnviron) InstanceTypes(c constraints.Value) (instances.InstanceTypesWithCostMetadata, error) {
	return env.env.InstanceTypes(c)
}

// ConstraintsValidator is part of the the environs.Environ interface.
func (env *sessionEnviron) ConstraintsValidator() (constraints.Validator, error) {
	return env.env.ConstraintsValidator()
}
