// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package leaseleadership

import (
	"time"

	"github.com/juju/errors"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/core/leadership"
	corelease "github.com/juju/juju/core/lease"
	"github.com/juju/juju/worker/lease"
)

// Secretary implements lease.Secretary; it checks that leases
// are application names, and holders are unit names.
type Secretary struct{}

// CheckLease is part of the lease.Secretary interface.
func (Secretary) CheckLease(name string) error {
	if !names.IsValidApplication(name) {
		return errors.NewNotValid(nil, "not an application name")
	}
	return nil
}

// CheckHolder is part of the lease.Secretary interface.
func (Secretary) CheckHolder(name string) error {
	if !names.IsValidUnit(name) {
		return errors.NewNotValid(nil, "not a unit name")
	}
	return nil
}

// CheckDuration is part of the lease.Secretary interface.
func (Secretary) CheckDuration(duration time.Duration) error {
	if duration <= 0 {
		return errors.NewNotValid(nil, "non-positive")
	}
	return nil
}

// Checker implements leadership.Checker by wrapping a lease.Manager.
type Checker struct {
	manager *lease.Manager
}

// LeadershipCheck is part of the leadership.Checker interface.
func (c Checker) LeadershipCheck(applicationname, unitName string) leadership.Token {
	token := c.manager.Token(applicationname, unitName)
	return Token{
		applicationname: applicationname,
		unitName:        unitName,
		token:           token,
	}
}

// Token implements leadership.Token by wrapping a corelease.Token.
type Token struct {
	applicationname string
	unitName        string
	token           corelease.Token
}

// Check is part of the leadership.Token interface.
func (t Token) Check(out interface{}) error {
	err := t.token.Check(out)
	if errors.Cause(err) == corelease.ErrNotHeld {
		return errors.Errorf("%q is not leader of %q", t.unitName, t.applicationname)
	}
	return errors.Trace(err)
}

// Claimer implements leadership.Claimer by wrappping a lease.Manager.
type Claimer struct {
	manager *lease.Manager
}

// ClaimLeadership is part of the leadership.Claimer interface.
func (c Claimer) ClaimLeadership(applicationname, unitName string, duration time.Duration) error {
	err := c.manager.Claim(applicationname, unitName, duration)
	if errors.Cause(err) == corelease.ErrClaimDenied {
		return leadership.ErrClaimDenied
	}
	return errors.Trace(err)
}

// BlockUntilLeadershipReleased is part of the leadership.Claimer interface.
func (c Claimer) BlockUntilLeadershipReleased(applicationname string) error {
	err := c.manager.WaitUntilExpired(applicationname)
	return errors.Trace(err)
}
