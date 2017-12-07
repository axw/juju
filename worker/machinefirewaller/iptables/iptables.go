package iptables

import "github.com/juju/juju/network"

type IngressRuleEnsurer struct{}

// EnsureIngressRules is part of the
// machinefirewaller.IngressRuleEnsurer
// interface.
func (IngressRuleEnsurer) EnsureIngressRules(
	addresses []network.Address,
	rules []network.IngressRule,
) error {
	return nil
}
