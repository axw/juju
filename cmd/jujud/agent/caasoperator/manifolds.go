// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package caasoperator

import (
	"github.com/juju/utils/clock"
	"github.com/prometheus/client_golang/prometheus"

	coreagent "github.com/juju/juju/agent"
	"github.com/juju/juju/api"
	"github.com/juju/juju/worker/agent"
	"github.com/juju/juju/worker/apicaller"
	"github.com/juju/juju/worker/caasoperator"
	"github.com/juju/juju/worker/dependency"
	"github.com/juju/juju/worker/logsender"
)

// ManifoldsConfig allows specialisation of the result of Manifolds.
type ManifoldsConfig struct {

	// Agent contains the agent that will be wrapped and made available to
	// its dependencies via a dependency.Engine.
	Agent coreagent.Agent

	// AgentDir is the location of the jujud binary.
	AgentDir string

	// LogSource will be read from by the logsender component.
	LogSource logsender.LogRecordCh

	// PrometheusRegisterer is a prometheus.Registerer that may be used
	// by workers to register Prometheus metric collectors.
	PrometheusRegisterer prometheus.Registerer
}

// Manifolds returns a set of co-configured manifolds covering the various
// responsibilities of a caasoperator agent. It also accepts the logSource
// argument because we haven't figured out how to thread all the logging bits
// through a dependency engine yet.
//
// Thou Shalt Not Use String Literals In This Function. Or Else.
func Manifolds(config ManifoldsConfig) dependency.Manifolds {

	return dependency.Manifolds{

		// The agent manifold references the enclosing agent, and is the
		// foundation stone on which most other manifolds ultimately depend.
		// (Currently, that is "all manifolds", but consider a shared clock.)
		agentName: agent.Manifold(config.Agent),

		apiCallerName: apicaller.Manifold(apicaller.ManifoldConfig{
			AgentName:     agentName,
			APIOpen:       api.Open,
			NewConnection: apicaller.OnlyConnect,
		}),

		// The operator installs and deploys charm containers;
		// manages the unit's presence in its relations;
		// creates suboordinate units; runs all the hooks;
		// sends metrics; etc etc etc.

		operatorName: caasoperator.Manifold(caasoperator.ManifoldConfig{
			AgentName:            agentName,
			APICallerName:        apiCallerName,
			Clock:                clock.WallClock,
			TranslateResolverErr: caasoperator.TranslateFortressErrors,
			AgentDir:             config.AgentDir,
		}),
	}
}

const (
	agentName     = "agent"
	apiCallerName = "api-caller"
	operatorName  = "operator"
)