// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"github.com/juju/juju/api/base"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/controller"
)

// ControllerConfigAPI provides common client-side API functions
// to call into apiserver.common.ControllerConfig.
type ControllerConfigAPI struct {
	facade base.FacadeCaller
}

// NewControllerConfig creates a ControllerConfig on the specified facade,
// and uses this name when calling through the caller.
func NewControllerConfig(facade base.FacadeCaller) *ControllerConfigAPI {
	return &ControllerConfigAPI{facade}
}

// ControllerConfig returns the current controller configuration.
func (e *ControllerConfigAPI) ControllerConfig() (controller.Config, error) {
	var result params.ControllerConfigResult
	err := e.facade.FacadeCall("ControllerConfig", nil, &result)
	if err != nil {
		return controller.Config{}, err
	}
	config := controller.Config{
		APIPort:              result.Config.APIPort,
		StatePort:            result.Config.StatePort,
		UUID:                 result.Config.UUID,
		CACert:               result.Config.CACert,
		IdentityURL:          result.Config.IdentityURL,
		IdentityPublicKey:    result.Config.IdentityPublicKey,
		SetNumaControlPolicy: result.Config.SetNumaControlPolicy,
	}
	return config, config.Validate()
}
