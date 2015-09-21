// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"runtime"
	"strings"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/cmd/envcmd"
	coretesting "github.com/juju/juju/testing"
	"github.com/juju/juju/worker/uniter/runner/debug"
)

var _ = gc.Suite(&DebugHooksSuite{})

type DebugHooksSuite struct {
	SSHCommonSuite
}

const debugHooksArgs = sshArgs
const debugHooksArgsNoProxy = sshArgsNoProxy

var debugHooksTests = []struct {
	info   string
	args   []string
	error  string
	proxy  bool
	result string
}{{
	args: []string{"mysql/0"},
	result: regexp.QuoteMeta(debugHooksArgsNoProxy +
		"ubuntu@mysql-0.public sudo /bin/bash -c 'F=$(mktemp); echo " +
		expectedDebugHooksScriptBase64("mysql/0", nil) +
		" | base64 -d > $F; . $F'\n",
	),
}, {
	args: []string{"mongodb/1"},
	result: regexp.QuoteMeta(debugHooksArgsNoProxy +
		"ubuntu@mongodb-1.public sudo /bin/bash -c 'F=$(mktemp); echo " +
		expectedDebugHooksScriptBase64("mongodb/1", nil) +
		" | base64 -d > $F; . $F'\n",
	),
}, {
	args:  []string{"mysql/0"},
	proxy: true,
	result: regexp.QuoteMeta(debugHooksArgs +
		"ubuntu@mysql-0.private sudo /bin/bash -c 'F=$(mktemp); echo " +
		expectedDebugHooksScriptBase64("mysql/0", nil) +
		" | base64 -d > $F; . $F'\n",
	),
}, {
	info:   `"*" is a valid hook name: it means hook everything`,
	args:   []string{"mysql/0", "*"},
	result: ".*\n",
}, {
	info:   `"*" mixed with named hooks is equivalent to "*"`,
	args:   []string{"mysql/0", "*", "relation-get"},
	result: ".*\n",
}, {
	info:   `multiple named hooks may be specified`,
	args:   []string{"mysql/0", "start", "stop"},
	result: ".*\n",
}, {
	info:   `relation hooks have the relation name prefixed`,
	args:   []string{"mysql/0", "juju-info-relation-joined"},
	result: ".*\n",
}, {
	info:  `invalid unit syntax`,
	args:  []string{"mysql"},
	error: `"mysql" is not a valid unit name`,
}, {
	info:  `invalid unit`,
	args:  []string{"nonexistent/123"},
	error: `unit "nonexistent/123" not found`,
}, {
	info:  `invalid hook`,
	args:  []string{"mysql/0", "invalid-hook"},
	error: `unit "mysql/0" does not contain hook "invalid-hook"`,
}}

func expectedDebugHooksScriptBase64(unitName string, hooks []string) string {
	ctx := debug.NewHooksContext(unitName)
	script := debug.ClientScript(ctx, hooks)
	return base64.StdEncoding.EncodeToString([]byte(script))
}

func (s *DebugHooksSuite) TestDebugHooksCommand(c *gc.C) {
	//TODO(bogdanteleaga): Fix once debughooks are supported on windows
	if runtime.GOOS == "windows" {
		c.Skip("bug 1403084: Skipping on windows for now")
	}

	s.mock.serviceCharmRelationsFunc = func(service string) ([]string, error) {
		c.Assert(service, gc.Equals, "mysql")
		return []string{"juju-info"}, nil
	}

	s.mock.privateAddressFunc = func(target string) (string, error) {
		if strings.HasPrefix(target, "nonexistent/") {
			return "", errors.NotFoundf("unit %q", target)
		}
		return strings.Replace(target, "/", "-", -1) + ".private", nil
	}

	for i, t := range debugHooksTests {
		c.Logf("test %d: %s\n\t%s\n", i, t.info, t.args)
		ctx := coretesting.Context(c)
		jujucmd := cmd.NewSuperCommand(cmd.SuperCommandParams{})

		debugHooksCmd := &DebugHooksCommand{}
		debugHooksCmd.proxy = true
		debugHooksCmd.apiClient = &s.mock
		debugHooksCmd.apiAddr = "localhost:1234"
		jujucmd.Register(envcmd.Wrap(debugHooksCmd))

		code := cmd.Main(jujucmd, ctx, append([]string{"debug-hooks"}, t.args...))
		//c.Check(ctx.Stderr.(*bytes.Buffer).String(), gc.Equals, "")
		//c.Check(strings.TrimRight(ctx.Stdout.(*bytes.Buffer).String(), "\r\n"), gc.Equals, t.result)

		/*
			err := envcmd.Wrap(debugHooksCmd).Init(t.args)
			if err == nil {
				err = debugHooksCmd.Run(ctx)
			}
			if t.error != "" {
				c.Check(err, gc.ErrorMatches, t.error)
				continue
			}
			if !c.Check(err, jc.ErrorIsNil) {
				continue
			}
		*/

		if t.error != "" {
			c.Check(code, gc.Not(gc.Equals), 0)
			stderr := ctx.Stderr.(*bytes.Buffer).String()
			c.Check(stderr, gc.Matches, "error: "+t.error)
			continue
		}

		c.Check(code, gc.Equals, 0)
		stdout := ctx.Stdout.(*bytes.Buffer).String()
		c.Check(stdout, gc.Matches, t.result)
	}
}
