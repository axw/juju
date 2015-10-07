// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package uniter_test

import (
	"github.com/juju/errors"
	"github.com/juju/names"
	"github.com/juju/testing"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	"gopkg.in/juju/charm.v6-unstable"
	"gopkg.in/juju/charm.v6-unstable/hooks"

	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/worker/uniter"
	uniteractions "github.com/juju/juju/worker/uniter/actions"
	"github.com/juju/juju/worker/uniter/hook"
	"github.com/juju/juju/worker/uniter/leadership"
	"github.com/juju/juju/worker/uniter/operation"
	"github.com/juju/juju/worker/uniter/relation"
	"github.com/juju/juju/worker/uniter/remotestate"
	"github.com/juju/juju/worker/uniter/resolver"
	"github.com/juju/juju/worker/uniter/storage"
)

type resolverSuite struct {
	stub        testing.Stub
	charmURL    *charm.URL
	remoteState remotestate.Snapshot
	opFactory   operation.Factory
	resolver    resolver.Resolver

	clearResolved   func() error
	reportHookError func(hook.Info) error
}

var _ = gc.Suite(&resolverSuite{})

func (s *resolverSuite) SetUpTest(c *gc.C) {
	s.stub = testing.Stub{}
	s.charmURL = charm.MustParseURL("cs:precise/mysql-2")
	s.remoteState = remotestate.Snapshot{
		CharmURL: s.charmURL,
	}
	s.opFactory = operation.NewFactory(operation.FactoryParams{})

	attachments, err := storage.NewAttachments(&dummyStorageAccessor{}, names.NewUnitTag("u/0"), c.MkDir(), nil)
	c.Assert(err, jc.ErrorIsNil)

	s.clearResolved = func() error {
		return errors.New("unexpected resolved")
	}

	s.reportHookError = func(hook.Info) error {
		return errors.New("unexpected report hook error")
	}

	s.resolver = uniter.NewUniterResolver(
		func() error { return s.clearResolved() },
		func(info hook.Info) error { return s.reportHookError(info) },
		func() error { return nil },
		func() { s.stub.AddCall("startRetryHookTimer") },
		func() { s.stub.AddCall("stopRetryHookTimer") },
		uniteractions.NewResolver(),
		leadership.NewResolver(),
		relation.NewRelationsResolver(&dummyRelations{}),
		storage.NewResolver(attachments),
	)
}

// TestStartedNotInstalled tests whether the Started flag overrides the
// Installed flag being unset, in the event of an unexpected inconsistency in
// local state.
func (s *resolverSuite) TestStartedNotInstalled(c *gc.C) {
	localState := resolver.LocalState{
		CharmURL: s.charmURL,
		State: operation.State{
			Kind:      operation.Continue,
			Installed: false,
			Started:   true,
		},
	}
	_, err := s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, gc.Equals, resolver.ErrNoOperation)
}

// TestNotStartedNotInstalled tests whether the next operation for an
// uninstalled local state is an install hook operation.
func (s *resolverSuite) TestNotStartedNotInstalled(c *gc.C) {
	localState := resolver.LocalState{
		CharmURL: s.charmURL,
		State: operation.State{
			Kind:      operation.Continue,
			Installed: false,
			Started:   false,
		},
	}
	op, err := s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(op.String(), gc.Equals, "run install hook")
}

func (s *resolverSuite) TestHookErrorStartRetryTimer(c *gc.C) {
	s.reportHookError = func(hook.Info) error { return nil }
	localState := resolver.LocalState{
		CharmURL: s.charmURL,
		State: operation.State{
			Kind:      operation.RunHook,
			Step:      operation.Pending,
			Installed: true,
			Started:   true,
			Hook: &hook.Info{
				Kind: hooks.ConfigChanged,
			},
		},
	}
	// Run the resolver twice; we should start the hook retry
	// timer on the first time through, no change on the second.
	_, err := s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, gc.Equals, resolver.ErrNoOperation)
	s.stub.CheckCallNames(c, "startRetryHookTimer")

	_, err = s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, gc.Equals, resolver.ErrNoOperation)
	s.stub.CheckCallNames(c, "startRetryHookTimer") // no change
}

func (s *resolverSuite) TestHookErrorStartRetryTimerAgain(c *gc.C) {
	s.reportHookError = func(hook.Info) error { return nil }
	localState := resolver.LocalState{
		CharmURL: s.charmURL,
		State: operation.State{
			Kind:      operation.RunHook,
			Step:      operation.Pending,
			Installed: true,
			Started:   true,
			Hook: &hook.Info{
				Kind: hooks.ConfigChanged,
			},
		},
	}

	_, err := s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, gc.Equals, resolver.ErrNoOperation)
	s.stub.CheckCallNames(c, "startRetryHookTimer")

	s.remoteState.RetryHookVersion = 1
	op, err := s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(op.String(), gc.Equals, "run config-changed hook")
	s.stub.CheckCallNames(c, "startRetryHookTimer") // no change
	localState.RetryHookVersion = 1

	_, err = s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, gc.Equals, resolver.ErrNoOperation)
	s.stub.CheckCallNames(c, "startRetryHookTimer", "startRetryHookTimer")
}

func (s *resolverSuite) TestResolvedRetryHooksStopRetryTimer(c *gc.C) {
	// Resolving a failed hook should stop the retry timer.
	s.testResolveHookErrorStopRetryTimer(c, params.ResolvedRetryHooks)
}

func (s *resolverSuite) TestResolvedNoHooksStopRetryTimer(c *gc.C) {
	// Resolving a failed hook should stop the retry timer.
	s.testResolveHookErrorStopRetryTimer(c, params.ResolvedNoHooks)
}

func (s *resolverSuite) testResolveHookErrorStopRetryTimer(c *gc.C, mode params.ResolvedMode) {
	s.stub.ResetCalls()
	s.clearResolved = func() error { return nil }
	s.reportHookError = func(hook.Info) error { return nil }
	localState := resolver.LocalState{
		CharmURL: s.charmURL,
		State: operation.State{
			Kind:      operation.RunHook,
			Step:      operation.Pending,
			Installed: true,
			Started:   true,
			Hook: &hook.Info{
				Kind: hooks.ConfigChanged,
			},
		},
	}

	_, err := s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, gc.Equals, resolver.ErrNoOperation)
	s.stub.CheckCallNames(c, "startRetryHookTimer")

	s.remoteState.ResolvedMode = mode
	_, err = s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, jc.ErrorIsNil)
	s.stub.CheckCallNames(c, "startRetryHookTimer", "stopRetryHookTimer")
}

func (s *resolverSuite) TestRunHookStopRetryTimer(c *gc.C) {
	s.reportHookError = func(hook.Info) error { return nil }
	localState := resolver.LocalState{
		CharmURL: s.charmURL,
		State: operation.State{
			Kind:      operation.RunHook,
			Step:      operation.Pending,
			Installed: true,
			Started:   true,
			Hook: &hook.Info{
				Kind: hooks.ConfigChanged,
			},
		},
	}

	_, err := s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, gc.Equals, resolver.ErrNoOperation)
	s.stub.CheckCallNames(c, "startRetryHookTimer")

	localState.Kind = operation.Continue
	_, err = s.resolver.NextOp(localState, s.remoteState, s.opFactory)
	c.Assert(err, gc.Equals, resolver.ErrNoOperation)
	s.stub.CheckCallNames(c, "startRetryHookTimer", "stopRetryHookTimer")
}
