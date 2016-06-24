// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state"
)

// ControllerConfigAPI implements two common methods for use by various
// facades - eg Provisioner and ControllerConfig.
type ControllerConfigAPI struct {
	st state.ControllerAccessor
}

// NewControllerConfig returns a new NewControllerConfigAPI.
func NewControllerConfig(st state.ControllerAccessor) *ControllerConfigAPI {
	return &ControllerConfigAPI{
		st: st,
	}
}

// ControllerConfig returns the controller's configuration.
func (s *ControllerConfigAPI) ControllerConfig() (params.ControllerConfigResult, error) {
	result := params.ControllerConfigResult{}
	config, err := s.st.ControllerConfig()
	if err != nil {
		return result, err
	}
	result.Config = params.ControllerConfig{
		APIPort:              config.APIPort,
		StatePort:            config.StatePort,
		UUID:                 config.UUID,
		CACert:               config.CACert,
		IdentityURL:          config.IdentityURL,
		IdentityPublicKey:    config.IdentityPublicKey,
		SetNumaControlPolicy: config.SetNumaControlPolicy,
	}
	return result, nil
}
