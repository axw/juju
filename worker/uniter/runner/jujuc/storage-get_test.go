// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package jujuc_test

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"

	"github.com/juju/cmd"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils/featureflag"
	gc "gopkg.in/check.v1"
	goyaml "gopkg.in/yaml.v1"

	"github.com/juju/juju/juju/osenv"
	"github.com/juju/juju/testing"
	"github.com/juju/juju/worker/uniter/runner/jujuc"
)

type storageGetSuite struct {
	ContextSuite
}

var _ = gc.Suite(&storageGetSuite{})

func (s *storageGetSuite) SetUpTest(c *gc.C) {
	s.ContextSuite.SetUpTest(c)
	s.PatchEnvironment(osenv.JujuFeatureFlagEnvKey, "storage")
	featureflag.SetFlagsFromEnvironment(osenv.JujuFeatureFlagEnvKey)
}

var (
	storageAttributes = map[string]interface{}{
		"location": "/dev/sda",
		"kind":     "block",
	}
)

var storageGetTests = []struct {
	args   []string
	format int
	out    interface{}
}{
	{[]string{"--format", "yaml"}, formatYaml, storageAttributes},
	{[]string{"--format", "json"}, formatJson, storageAttributes},
	{[]string{}, formatYaml, storageAttributes},
	{[]string{"location"}, -1, "/dev/sda\n"},
}

func (s *storageGetSuite) TestOutputFormatKey(c *gc.C) {
	for i, t := range storageGetTests {
		c.Logf("test %d: %#v", i, t.args)
		hctx := s.GetHookContext(c, -1, "")
		com, err := jujuc.NewCommand(hctx, cmdString("storage-get"))
		c.Assert(err, jc.ErrorIsNil)
		ctx := testing.Context(c)
		code := cmd.Main(com, ctx, t.args)
		c.Assert(code, gc.Equals, 0)
		c.Assert(bufferString(ctx.Stderr), gc.Equals, "")

		var out interface{}
		var outMap map[string]interface{}
		switch t.format {
		case formatYaml:
			c.Assert(goyaml.Unmarshal(bufferBytes(ctx.Stdout), &outMap), gc.IsNil)
			out = outMap
		case formatJson:
			c.Assert(json.Unmarshal(bufferBytes(ctx.Stdout), &outMap), gc.IsNil)
			out = outMap
		default:
			out = string(bufferBytes(ctx.Stdout))
		}
		c.Assert(out, gc.DeepEquals, t.out)
	}
}

func (s *storageGetSuite) TestHelp(c *gc.C) {
	hctx := s.GetHookContext(c, -1, "")
	com, err := jujuc.NewCommand(hctx, cmdString("storage-get"))
	c.Assert(err, jc.ErrorIsNil)
	ctx := testing.Context(c)
	code := cmd.Main(com, ctx, []string{"--help"})
	c.Assert(code, gc.Equals, 0)
	c.Assert(bufferString(ctx.Stdout), gc.Equals, `usage: storage-get [options] [<key>]
purpose: print information for storage instance with specified id

options:
--format  (= smart)
    specify output format (json|smart|yaml)
-o, --output (= "")
    specify an output file
-s  (= 1234)
    specify a storage instance by id

When no <key> is supplied, all keys values are printed.
`)
	c.Assert(bufferString(ctx.Stderr), gc.Equals, "")
}

//
func (s *storageGetSuite) TestOutputPath(c *gc.C) {
	hctx := s.GetHookContext(c, -1, "")
	com, err := jujuc.NewCommand(hctx, cmdString("storage-get"))
	c.Assert(err, jc.ErrorIsNil)
	ctx := testing.Context(c)
	code := cmd.Main(com, ctx, []string{"--format", "yaml", "--output", "some-file", "-s", "1234"})
	c.Assert(code, gc.Equals, 0)
	c.Assert(bufferString(ctx.Stderr), gc.Equals, "")
	c.Assert(bufferString(ctx.Stdout), gc.Equals, "")
	content, err := ioutil.ReadFile(filepath.Join(ctx.Dir, "some-file"))
	c.Assert(err, jc.ErrorIsNil)

	var out map[string]interface{}
	c.Assert(goyaml.Unmarshal(content, &out), gc.IsNil)
	c.Assert(out, gc.DeepEquals, storageAttributes)
}
