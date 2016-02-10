// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package jujuclient

// ControllerDetails holds the details needed to connect to a controller.
type ControllerDetails struct {
	// Servers contains the addresses of hosts that form the Juju controller
	// cluster.
	Servers []string `yaml:"servers,flow"`

	// ControllerUUID is the unique ID for the controller.
	ControllerUUID string `yaml:"uuid"`

	// APIEndpoints is the collection of API endpoints running in this controller.
	APIEndpoints []string `yaml:"api-endpoints,flow"`

	// CACert is a security certificate for this controller.
	CACert string `yaml:"ca-cert"`
}

// ModelDetails holds details of a model.
type ModelDetails struct {
	// ModelUUID holds the details of a model.
	ModelUUID string `yaml:"uuid"`
}

// ControllerUpdater stores controller details.
type ControllerUpdater interface {
	// UpdateController adds the given controller to the controller
	// collection.
	//
	// If the controller does not already exist, it will be added.
	// Otherwise, it will be overwritten with the new details.
	UpdateController(name string, one ControllerDetails) error
}

// ControllerRemover removes controllers.
type ControllerRemover interface {
	// RemoveController removes the controller with the given name from the
	// controllers collection.
	RemoveController(name string) error
}

// ControllerGetter gets controllers.
type ControllerGetter interface {
	// AllControllers gets all controllers.
	AllControllers() (map[string]ControllerDetails, error)

	// ControllerByName returns the controller with the specified name.
	// If there exists no controller with the specified name, an error
	// satisfying errors.IsNotFound will be returned.
	ControllerByName(name string) (*ControllerDetails, error)
}

// ModelUpdater stores model details.
type ModelUpdater interface {
	// UpdateModel adds the given model to the model collection.
	//
	// If the model does not already exist, it will be added.
	// Otherwise, it will be overwritten with the new details.
	UpdateModel(controllerName, modelName string, details ModelDetails) error

	// SetCurrentModel sets the name of the current model for
	// the specified controller. If there exists no model with
	// the specified names, an error satisfing errors.IsNotFound
	// will be returned.
	SetCurrentModel(controllerName, modelName string) error
}

// ModelRemover removes models.
type ModelRemover interface {
	// RemoveModel removes the model with the given controller and model
	// names from the models collection.
	RemoveModel(controllerName, modelName string) error
}

// ModelGetter gets models.
type ModelGetter interface {
	// AllModels gets all models for the specified controller.
	AllModels(controller string) (map[string]ModelDetails, error)

	// CurrentModel returns the name of the current model for
	// the specified controller. If there is no current model
	// for the controller, an error satisfying errors.IsNotFound
	// is returned.
	CurrentModel(controller string) (string, error)

	// ModelByName returns the model with the specified controller
	// and model names. If there exists no model with the specified
	// names, an error satisfying errors.IsNotFound will be returned.
	ModelByName(controllerName, modelName string) (*ModelDetails, error)
}

// ControllerStore is an amalgamation of ControllerUpdater, ControllerRemover,
// and ControllerGetter.
type ControllerStore interface {
	ControllerUpdater
	ControllerRemover
	ControllerGetter
}

// ModelStore is an amalgamation of ModelUpdater, ModelRemover, and ModelGetter.
type ModelStore interface {
	ModelUpdater
	ModelRemover
	ModelGetter
}

// ClientStore is an amalgamation of ControllerStore and ModelStore.
type ClientStore interface {
	ControllerStore
	ModelStore
}
