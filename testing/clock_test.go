// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package testing_test

import (
	"time"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/testing"
)

var t0 time.Time

type ClockSuite struct {
	clock *testing.Clock
}

var _ = gc.Suite(&ClockSuite{})

func (s *ClockSuite) SetUpTest(c *gc.C) {
	s.clock = testing.NewClock(t0)
}

func (s *ClockSuite) TestNewTimer(c *gc.C) {
	timer := s.clock.NewTimer(time.Second)
	select {
	case <-timer.C():
		c.Fatal("unexpected event")
	default:
	}

	s.clock.Advance(time.Second + time.Millisecond)
	select {
	case t, ok := <-timer.C():
		c.Assert(ok, jc.IsTrue)
		c.Assert(t, gc.Equals, t0.Add(time.Second+time.Millisecond))
	default:
		c.Fatal("expected event")
	}
}

func (s *ClockSuite) TestNewTimerReset(c *gc.C) {
	timer := s.clock.NewTimer(time.Second)
	timer.Reset(2 * time.Second)

	s.clock.Advance(time.Second)
	select {
	case <-timer.C():
		c.Fatal("unexpected event")
	default:
	}

	s.clock.Advance(time.Second)
	select {
	case t, ok := <-timer.C():
		c.Assert(ok, jc.IsTrue)
		c.Assert(t, gc.Equals, t0.Add(2*time.Second))
	default:
		c.Fatal("expected event")
	}

	timer.Reset(time.Second)
	s.clock.Advance(time.Second)
	select {
	case t, ok := <-timer.C():
		c.Assert(ok, jc.IsTrue)
		c.Assert(t, gc.Equals, t0.Add(2*time.Second))
	default:
		c.Fatal("expected event")
	}
}
