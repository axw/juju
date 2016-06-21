// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package dummy_test

import (
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	envtesting "github.com/juju/juju/environs/testing"
	"github.com/juju/juju/jujuclient/jujuclienttesting"
	"github.com/juju/juju/provider/dummy"
	"github.com/juju/juju/testing"
)

var _ = gc.Suite(&ConfigSuite{})

type ConfigSuite struct {
	testing.BaseSuite
}

func (s *ConfigSuite) TearDownTest(c *gc.C) {
	s.BaseSuite.TearDownTest(c)
	dummy.Reset(c)
}

func (*ConfigSuite) TestSecretAttrs(c *gc.C) {
	attrs := dummy.SampleConfig().Delete("secret")
	cfg, err := config.New(config.NoDefaults, attrs)
	c.Assert(err, jc.ErrorIsNil)

	dummyProvider, err := environs.Provider("dummy")
	c.Assert(err, jc.ErrorIsNil)
	actual, err := dummyProvider.SecretAttrs(cfg)
	c.Assert(err, jc.ErrorIsNil)

	c.Assert(actual, gc.DeepEquals, map[string]string{
		"secret": "pork",
	})
}

var firewallModeTests = []struct {
	configFirewallMode string
	firewallMode       string
	errorMsg           string
}{
	{
		// Empty value leads to default value.
		firewallMode: config.FwInstance,
	}, {
		// Explicit default value.
		configFirewallMode: "",
		firewallMode:       config.FwInstance,
	}, {
		// Instance mode.
		configFirewallMode: "instance",
		firewallMode:       config.FwInstance,
	}, {
		// Global mode.
		configFirewallMode: "global",
		firewallMode:       config.FwGlobal,
	}, {
		// Invalid mode.
		configFirewallMode: "invalid",
		errorMsg:           `firewall-mode: expected one of \[instance global none ], got "invalid"`,
	},
}

func (s *ConfigSuite) TestFirewallMode(c *gc.C) {
	for i, test := range firewallModeTests {
		c.Logf("test %d: %s", i, test.configFirewallMode)
		attrs := testing.Attrs{
			"firewall-mode":  config.FwInstance,
			"ca-cert":        testing.CACert,
			"ca-private-key": testing.CAKey,
		}
		if test.configFirewallMode != "" {
			attrs = attrs.Merge(testing.Attrs{
				"firewall-mode": test.configFirewallMode,
			})
		}
		ctx := envtesting.BootstrapContext(c)
		env, err := environs.Prepare(
			ctx, jujuclienttesting.NewMemStore(),
			environs.PrepareParams{
				ControllerName: "ctrl",
				BaseConfig:     attrs,
				CloudName:      "dummy",
				CloudConfig: config.CloudConfig{
					Type: "dummy",
				},
			},
		)
		if test.errorMsg != "" {
			c.Assert(err, gc.ErrorMatches, test.errorMsg)
			continue
		}
		c.Assert(err, jc.ErrorIsNil)
		defer env.Destroy()

		firewallMode := env.Config().FirewallMode()
		c.Assert(firewallMode, gc.Equals, test.firewallMode)

		s.TearDownTest(c)
	}
}
