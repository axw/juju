// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package logforwarder

import (
	"github.com/juju/errors"
	worker "gopkg.in/juju/worker.v1"

	apiagent "github.com/juju/juju/api/agent"
	"github.com/juju/juju/api/base"
	"github.com/juju/juju/api/logstream"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/worker/dependency"
)

// EnvironManifoldConfig defines the names of the manifolds on which a
// EnvironManifold will depend.
type EnvironManifoldConfig struct {
	// These are the dependency resource names.
	EnvironName   string
	APICallerName string

	// OpenLogStream is the function that will be used to open the
	// log stream.
	OpenLogStream LogStreamFn

	// OpenLogForwarder opens each log forwarder that will be used.
	OpenLogForwarder func(OpenLogForwarderArgs) (*LogForwarder, error)
}

// EnvironManifold returns a dependency manifold that runs a log forwarding
// worker using the Environ's LogSink, if any, and using the resource names
// defined in the supplied config.
func EnvironManifold(config ManifoldConfig) dependency.Manifold {
	openLogStream := config.OpenLogStream
	if openLogStream == nil {
		openLogStream = func(caller base.APICaller, cfg params.LogStreamConfig, controllerUUID string) (LogStream, error) {
			return logstream.Open(caller, cfg, controllerUUID)
		}
	}

	openForwarder := config.OpenLogForwarder
	if openForwarder == nil {
		openForwarder = NewLogForwarder
	}

	return dependency.Manifold{
		Inputs: []string{
			config.EnvironName,
			config.APICallerName,
		},
		Start: func(context dependency.Context) (worker.Worker, error) {
			var environ environs.Environ
			if err := context.Get(cfg.EnvironName, &environ); err != nil {
				return nil, errors.Trace(err)
			}
			// TODO(axw) get Sender from environ.

			var apiCaller base.APICaller
			if err := context.Get(config.APICallerName, &apiCaller); err != nil {
				return nil, errors.Trace(err)
			}

			agentFacade := apiagent.NewState(apiCaller)
			controllerCfg, err := agentFacade.ControllerConfig()
			if err != nil {
				return nil, errors.Annotate(err, "cannot read controller config")
			}

			orchestrator, err := newOrchestratorForController(OrchestratorArgs{
				ControllerUUID:   controllerCfg.ControllerUUID(),
				LogForwardConfig: agentFacade,
				Caller:           apiCaller,
				Sinks:            config.Sinks,
				OpenLogStream:    openLogStream,
				OpenLogForwarder: openForwarder,
			})
			return orchestrator, errors.Annotate(err, "creating log forwarding orchestrator")
		},
	}
}
