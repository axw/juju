package apiserver

import (
	"sync"

	"github.com/juju/errors"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/core/leadership"
)

// ModelResourcesRegistry is a registry containing resources
// corresponding to models being managed by the controller.
//
// ModelResources will be registered when they are available
// for use, and unregistered when they are no longer available.
type ModelResourcesRegistry struct {
	mu     sync.Mutex
	models map[string]ModelResources
}

// NewModelResourcesRegistry returns a new ModelResourcesRegistry.
func NewModelResourcesRegistry() *ModelResourcesRegistry {
	return &ModelResourcesRegistry{
		models: make(map[string]ModelResources),
	}
}

// RegisterModelResources registers the given ModelResources,
// corresponding to the model with the given UUID, into the
// registry.
//
// RegisterModelResources will fail if the UUID or resources
// are invalid, or if there already exists an entry for the
// given UUID.
func (r *ModelResourcesRegistry) RegisterModelResources(modelUUID string, resources ModelResources) error {
	if !names.IsValidModel(modelUUID) {
		return errors.NotValidf("model UUID %q", modelUUID)
	}
	if err := resources.Validate(); err != nil {
		return errors.Trace(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.models[modelUUID]; ok {
		return errors.Errorf("model %q resources already registered", modelUUID)
	}
	r.models[modelUUID] = resources
	return nil
}

// UnregisterModelResources unregisters the resources previously
// registered for the model with the given UUID.
func (r *ModelResourcesRegistry) UnregisterModelResources(modelUUID string) error {
	if !names.IsValidModel(modelUUID) {
		return errors.NotValidf("model UUID %q", modelUUID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.models[modelUUID]; ok {
		delete(r.models, modelUUID)
		return nil
	}
	return errors.NotFoundf("model %q resources", modelUUID)
}

// ModelResources contains the resources for a model, for use
// by the apiserver.
type ModelResources struct {
	LeadershipClaimer leadership.Claimer
	LeadershipChecker leadership.Checker
}

// Validate checks that the ModelResources is valid for registration.
func (r ModelResources) Validate() error {
	if r.LeadershipClaimer == nil {
		return errors.NotValidf("nil LeadershipClaimer")
	}
	if r.LeadershipChecker == nil {
		return errors.NotValidf("nil LeadershipChecker")
	}
	return nil
}
