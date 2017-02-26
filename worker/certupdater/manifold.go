// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package certupdater

import (
	"github.com/juju/errors"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/agent"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/worker"
	"github.com/juju/juju/worker/dependency"
	"github.com/juju/juju/worker/state"
)

// ManifoldConfig defines the static configuration, and names
// of the manifolds on which the certupgrader Manifold will depend.
type ManifoldConfig struct {
	AgentName string
	StateName string
}

// Manifold returns a dependency manifold that runs a certupdater worker, using
// the resource names defined in the supplied config.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{
			config.AgentName,
			config.StateName,
		},
		Start: func(context dependency.Context) (worker.Worker, error) {
			var a agent.Agent
			if err := context.Get(config.AgentName, &a); err != nil {
				return nil, errors.Trace(err)
			}

			var stateTracker state.StateTracker
			if err := context.Get(config.StateName, &stateTracker); err != nil {
				return nil, errors.Trace(err)
			}

			agentConfig := a.CurrentConfig()
			tag := agentConfig.Tag()
			if tag.Kind() != names.MachineTagKind {
				return nil, errors.NotValidf("non-machine tag %q", tag.String())
			}

			st, err := stateTracker.Use()
			if err != nil {
				return nil, errors.Trace(err)
			}
			m, err := st.Machine(tag.Id())
			if err != nil {
				return nil, errors.Trace(err)
			}
			// TODO(axw) call stateTracker.Done() when the worker exits.

			// certChangedChan is shared by multiple workers it's up
			// to the agent to close it rather than any one of the
			// workers.  It is possible that multiple cert changes
			// come in before the apiserver is up to receive them.
			// Specify a bigger buffer to prevent deadlock when
			// the apiserver isn't up yet.  Use a size of 10 since we
			// allow up to 7 controllers, and might also update the
			// addresses of the local machine (127.0.0.1, ::1, etc).
			//
			// TODO(axw) certUpdaterWorker should close the channel
			// when it exits.
			ch := make(chan params.StateServingInfo, 10)
			stateServingInfoSetter := func(info params.StateServingInfo, done <-chan struct{}) error {
				return a.ChangeConfig(func(config agent.ConfigSetter) error {
					config.SetStateServingInfo(info)
					select {
					case ch <- info:
						return nil
					case <-done:
						return nil
					}
				})
			}
			w := NewCertificateUpdater(m, agentConfig, st, st, stateServingInfoSetter)
			return &certUpdaterWorker{w, ch}, nil
		},
		Output: func(in worker.Worker, out interface{}) error {
			w, ok := in.(*certUpdaterWorker)
			if !ok {
				return errors.Errorf("in should be a %T; got %T", w, in)
			}
			chp, ok := out.(*<-chan params.StateServingInfo)
			if ok {
				*chp = w.ch
				return nil
			}
			return errors.Errorf("out should be %T; got %T", chp, out)
		},
	}
}

type certUpdaterWorker struct {
	worker.Worker
	ch chan params.StateServingInfo
}
