// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package machinefirewaller

import (
	"runtime"

	"github.com/juju/errors"
	"github.com/juju/names"
	"gopkg.in/juju/worker.v1"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/worker/dependency"
)

// ManifoldConfig describes the resources used by the machine firewaller worker.
type ManifoldConfig struct {
	APICallerName string

	Machine              string
	IngressRuleEnsurer   IngressRuleEnsurer
	NewAddressGetter     func(base.APICaller) (AddressGetter, error)
	NewIngressRuleGetter func(base.APICaller) (IngressRuleGetter, error)
	NewWorker            func(Config) (worker.Worker, error)
}

// Manifold returns a Manifold that encapsulates the firewaller worker.
func Manifold(config ManifoldConfig) dependency.Manifold {
	return dependency.Manifold{
		Inputs: []string{
			config.APICallerName,
		},
		Start: config.start,
	}
}

// Validate is called by start to check for bad configuration.
func (config ManifoldConfig) Validate() error {
	if !names.IsValidMachine(config.Machine) {
		return errors.NotValidf("machine ID %q", config.Machine)
	}
	if config.APICallerName == "" {
		return errors.NotValidf("empty APICallerName")
	}
	if config.IngressRuleEnsurer == nil {
		return errors.NotValidf("nil IngressRuleEnsurer")
	}
	if config.NewAddressGetter == nil {
		return errors.NotValidf("nil NewAddressGetter")
	}
	if config.NewIngressRuleGetter == nil {
		return errors.NotValidf("nil NewIngressRuleGetter")
	}
	if config.NewWorker == nil {
		return errors.NotValidf("nil NewWorker")
	}
	return nil
}

// start is a StartFunc for a Worker manifold.
func (config ManifoldConfig) start(context dependency.Context) (worker.Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}

	if runtime.GOOS != "linux" {
		// TODO(axw) implement local firewalling for Windows.
		logger.Debugf("machine firewaller not supported on %s", runtime.GOOS)
		return nil, dependency.ErrUninstall
	}

	var apiCaller base.APICaller
	if err := context.Get(config.APICallerName, &apiCaller); err != nil {
		return nil, errors.Trace(err)
	}

	addressGetter, err := config.NewAddressGetter(apiCaller)
	if err != nil {
		return nil, errors.Trace(err)
	}

	ingressRuleGetter, err := config.NewIngressRuleGetter(apiCaller)
	if err != nil {
		return nil, errors.Trace(err)
	}

	w, err := config.NewWorker(Config{
		AddressGetter:      addressGetter,
		IngressRuleEnsurer: config.IngressRuleEnsurer,
		IngressRuleGetter:  ingressRuleGetter,
		Machine:            config.Machine,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return w, nil
}
