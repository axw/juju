// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package apiserver

import (
	"github.com/juju/errors"
	"github.com/juju/pubsub"
	"github.com/juju/utils/clock"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/apiserver/observer"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/state"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/dependency"
	"github.com/juju/juju/worker/httpserver"
)

// ManifoldConfig defines the static configuration, and names
// of the manifolds on which the apiserver Manifold will depend.
type ManifoldConfig struct {
	AgentName      string
	HTTPServerName string
	CentralHubName string

	Clock         clock.Clock
	ValidateLogin func(params.LoginRequest) error
	OpenState     func(agent.Config) (*state.State, error)
}

// Manifold returns a dependency manifold that runs an apiserver worker, using
// the resource names defined in the supplied config.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{
			config.AgentName,
			config.HTTPServerName,
			config.CentralHubName,
		},
		Start: func(context dependency.Context) (worker.Worker, error) {
			var a agent.Agent
			if err := context.Get(config.AgentName, &a); err != nil {
				return nil, errors.Trace(err)
			}
			agentConfig := a.CurrentConfig()

			var hub *pubsub.StructuredHub
			if err := context.Get(config.CentralHubName, &hub); err != nil {
				return nil, errors.Trace(err)
			}

			var registerHandler httpserver.RegisterHandlerFunc
			if err := context.Get(config.HTTPServerName, &registerHandler); err != nil {
				return nil, errors.Trace(err)
			}

			// TODO(axw) audit-logging observer
			newObserver := observer.None()

			// TODO(axw) introspection handler

			// Each time apiserver worker is restarted, we need a
			// fresh copy of state due to the fact that state holds
			// lease managers which are killed and need to be reset.
			st, err := config.OpenState(agentConfig)
			if err != nil {
				return nil, errors.Trace(err)
			}

			handlers, err := apiserver.NewHTTPHandlers(apiserver.ServerConfig{
				State:       st,
				StatePool:   statePool,
				Tag:         agentConfig.Tag(),
				DataDir:     agentConfig.DataDir(),
				LogDir:      agentConfig.LogDir(),
				Clock:       config.Clock,
				Validator:   config.ValidateLogin,
				Hub:         hub,
				NewObserver: newObserver,
			})
			if err != nil {
				st.Close()
				return nil, errors.Trace(err)
			}
			handlers.Register(registerHandler)
			return handlers, nil
		},
	}
}
