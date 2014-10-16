// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package instance_test

import (
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/instance"
)

type StorageSuite struct{}

var _ = gc.Suite(&StorageSuite{})

func (s *StorageSuite) TestParseStorage(c *gc.C) {
	parseStorageTests := []struct {
		arg            string
		expectProvider string
		expectName     string
		expectCount    int
		expectSize     uint64
		expectOptions  string
		err            string
	}{{
		arg: "",
		err: `failed to parse storage ""`,
	}, {
		arg: ":",
		err: `failed to parse storage ":"`,
	}, {
		arg: "1M",
		err: "storage name missing",
	}, {
		arg: "name:1M",
		err: "storage provider missing",
	}, {
		arg:            "name:provider:1M",
		expectName:     "name",
		expectProvider: "provider",
		expectCount:    1,
		expectSize:     1,
	}, {
		arg:            "name:provider:1M:",
		expectName:     "name",
		expectProvider: "provider",
		expectCount:    1,
		expectSize:     1,
	}, {
		arg:            "name:provider:1M:whatever options that please me",
		expectName:     "name",
		expectProvider: "provider",
		expectCount:    1,
		expectSize:     1,
		expectOptions:  "whatever options that please me",
	}, {
		arg:            "n:p:1G",
		expectName:     "n",
		expectProvider: "p",
		expectCount:    1,
		expectSize:     1024,
	}, {
		arg:            "n:p:0.5T",
		expectName:     "n",
		expectProvider: "p",
		expectCount:    1,
		expectSize:     1024 * 512,
	}, {
		arg:            "n:p:3x0.125P",
		expectName:     "n",
		expectProvider: "p",
		expectCount:    3,
		expectSize:     1024 * 1024 * 128,
	}, {
		arg: "n:p:0x100M",
		err: "count must be a positive integer",
	}, {
		arg: "n:p:-1x100M",
		err: "count must be a positive integer",
	}, {
		arg: "n:p:-100M",
		err: "must be a non-negative float with optional M/G/T/P suffix",
	}}

	for i, t := range parseStorageTests {
		c.Logf("test %d: %s", i, t.arg)
		p, err := instance.ParseStorage(t.arg)
		if t.err != "" {
			c.Check(err, gc.ErrorMatches, t.err)
			c.Check(p, gc.IsNil)
		} else {
			if !c.Check(err, gc.IsNil) {
				continue
			}
			c.Check(p, gc.DeepEquals, &instance.Storage{
				Name:     t.expectName,
				Provider: t.expectProvider,
				Count:    t.expectCount,
				Size:     t.expectSize,
				Options:  t.expectOptions,
			})
		}
	}
}
