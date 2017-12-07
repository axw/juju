// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package machinefirewaller

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"gopkg.in/juju/names.v2"
	"gopkg.in/juju/worker.v1"

	"github.com/juju/juju/network"
	"github.com/juju/juju/watcher"
	"github.com/juju/juju/worker/catacomb"
)

var logger = loggo.GetLogger("juju.worker.machinefirewaller")

// AddressGetter defines an interface for watching and getting the
// current addresses for a machine.
type AddressGetter interface {
	Addresses(machine string) ([]network.Address, error)
	WatchAddresses(machine string) (watcher.NotifyWatcher, error)
}

// IngressRuleGetter defines an interface for watching and getting the
// current ingress rules for a machine.
type IngressRuleGetter interface {
	IngressRules(machine string) ([]network.IngressRule, error)
	WatchIngressRules(machine string) (watcher.NotifyWatcher, error)
}

// IngressRuleEnsurer defines an interface for ensuring the desired
// (i.e. specified) ingress rules are applied locally to the machine,
// for the given public network addresses.
type IngressRuleEnsurer interface {
	// EnsureIngressRules ensures that the given ingress rules
	// are applied locally to the machine for the given public
	// network addresses.
	EnsureIngressRules(
		[]network.Address,
		[]network.IngressRule,
	) error
}

// Config holds configuration for running a machine firewaller.
type Config struct {
	AddressGetter      AddressGetter
	IngressRuleEnsurer IngressRuleEnsurer
	IngressRuleGetter  IngressRuleGetter
	Machine            string
}

// Validate validates the machine fiewaller configuration.
func (config Config) Validate() error {
	if config.AddressGetter == nil {
		return errors.NotValidf("nil AddressGetter")
	}
	if config.IngressRuleEnsurer == nil {
		return errors.NotValidf("nil IngressRuleEnsurer")
	}
	if config.IngressRuleGetter == nil {
		return errors.NotValidf("nil IngressRuleGetter")
	}
	if !names.IsValidMachine(config.Machine) {
		return errors.NotValidf("machine ID %q", config.Machine)
	}
	return nil
}

// Firewaller watches the machine's ingress rules, and ensures they
// are applied locally.
type Firewaller struct {
	catacomb catacomb.Catacomb
	config   Config
}

// NewFirewaller returns a new machine firewaller worker.
func NewFirewaller(config Config) (worker.Worker, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Trace(err)
	}
	fw := &Firewaller{config: config}
	err := catacomb.Invoke(catacomb.Plan{
		Site: &fw.catacomb,
		Work: fw.loop,
	})
	if err != nil {
		return nil, errors.Trace(err)
	}
	return fw, nil
}

// Kill is part of the worker.Worker interface.
func (fw *Firewaller) Kill() {
	fw.catacomb.Kill(nil)
}

// Wait is part of the worker.Worker interface.
func (fw *Firewaller) Wait() error {
	return fw.catacomb.Wait()
}

func (fw *Firewaller) loop() error {
	addressWatcher, err := fw.config.AddressGetter.WatchAddresses(
		fw.config.Machine,
	)
	if err != nil {
		return errors.Annotate(err, "watching addresses")
	}
	fw.catacomb.Add(addressWatcher)

	ingressRuleWatcher, err := fw.config.IngressRuleGetter.WatchIngressRules(
		fw.config.Machine,
	)
	if err != nil {
		return errors.Annotate(err, "watching ingress rules")
	}
	fw.catacomb.Add(ingressRuleWatcher)

	var publicAddresses []network.Address
	var rules []network.IngressRule
	var haveAddresses, haveRules bool

	for {
		select {
		case <-fw.catacomb.Dying():
			return fw.catacomb.ErrDying()

		case _, ok := <-addressWatcher.Changes():
			if !ok {
				return errors.New("address watcher closed")
			}
			allAddresses, err := fw.config.AddressGetter.Addresses(fw.config.Machine)
			if err != nil {
				return errors.Annotate(err, "getting addresses")
			}
			publicAddresses = make([]network.Address, 0, len(allAddresses))
			for _, addr := range allAddresses {
				if addr.Scope == network.ScopePublic {
					publicAddresses = append(publicAddresses, addr)
				}
			}
			haveAddresses = true

		case _, ok := <-ingressRuleWatcher.Changes():
			if !ok {
				return errors.New("ingress rule watcher closed")
			}
			rules, err = fw.config.IngressRuleGetter.IngressRules(fw.config.Machine)
			if err != nil {
				return errors.Annotate(err, "getting ingress rules")
			}
			haveRules = true
		}

		if !haveAddresses || !haveRules {
			continue
		}
		if err := fw.config.IngressRuleEnsurer.EnsureIngressRules(
			publicAddresses, rules,
		); err != nil {
			return errors.Annotate(err, "ensuring ingress rules are applied locally")
		}
	}
}
