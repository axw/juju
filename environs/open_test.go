// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environs_test

import (
	"github.com/juju/errors"
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/cert"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/bootstrap"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/environs/filestorage"
	sstesting "github.com/juju/juju/environs/simplestreams/testing"
	envtesting "github.com/juju/juju/environs/testing"
	envtools "github.com/juju/juju/environs/tools"
	"github.com/juju/juju/juju"
	"github.com/juju/juju/jujuclient"
	"github.com/juju/juju/jujuclient/jujuclienttesting"
	"github.com/juju/juju/provider/dummy"
	"github.com/juju/juju/testing"
	jujuversion "github.com/juju/juju/version"
)

type OpenSuite struct {
	testing.FakeJujuXDGDataHomeSuite
	envtesting.ToolsFixture
}

var _ = gc.Suite(&OpenSuite{})

func (s *OpenSuite) SetUpTest(c *gc.C) {
	s.FakeJujuXDGDataHomeSuite.SetUpTest(c)
	s.ToolsFixture.SetUpTest(c)
	s.PatchValue(&juju.JujuPublicKey, sstesting.SignedMetadataPublicKey)
}

func (s *OpenSuite) TearDownTest(c *gc.C) {
	dummy.Reset(c)
	s.ToolsFixture.TearDownTest(c)
	s.FakeJujuXDGDataHomeSuite.TearDownTest(c)
}

func (s *OpenSuite) TestNewDummyEnviron(c *gc.C) {
	s.PatchValue(&jujuversion.Current, testing.FakeVersionNumber)
	cfg := testing.Attrs{"controller": false}
	ctx := envtesting.BootstrapContext(c)
	cache := jujuclienttesting.NewMemStore()
	env, err := environs.Prepare(ctx, cache, environs.PrepareParams{
		ControllerName: "ctrl",
		BaseConfig:     cfg,
		CloudName:      "dummy",
		CloudConfig:    config.CloudConfig{Type: "dummy"},
	})
	c.Assert(err, jc.ErrorIsNil)

	storageDir := c.MkDir()
	s.PatchValue(&envtools.DefaultBaseURL, storageDir)
	stor, err := filestorage.NewFileStorageWriter(storageDir)
	c.Assert(err, jc.ErrorIsNil)
	envtesting.UploadFakeTools(c, stor, "released", "released")
	err = bootstrap.Bootstrap(ctx, env, bootstrap.BootstrapParams{
		ControllerUUID: controller.Config(env.Config().AllAttrs()).ControllerUUID(),
	})
	c.Assert(err, jc.ErrorIsNil)

	// New controller should have been added to collection.
	foundController, err := cache.ControllerByName("ctrl")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(foundController.ControllerUUID, gc.DeepEquals, env.Config().UUID())
}

func (s *OpenSuite) TestUpdateEnvInfo(c *gc.C) {
	store := jujuclienttesting.NewMemStore()
	ctx := envtesting.BootstrapContext(c)
	cfg := map[string]interface{}{}
	env, err := environs.Prepare(ctx, store, environs.PrepareParams{
		ControllerName: "controller-name",
		BaseConfig:     cfg,
		CloudName:      "dummy",
		CloudConfig:    config.CloudConfig{Type: "dummy"},
	})
	c.Assert(err, jc.ErrorIsNil)

	foundController, err := store.ControllerByName("controller-name")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(foundController.ControllerUUID, gc.Not(gc.Equals), "")
	c.Assert(foundController.CACert, gc.Not(gc.Equals), "")
	foundModel, err := store.ModelByName("controller-name", "admin@local", "controller")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(foundModel, jc.DeepEquals, &jujuclient.ModelDetails{
		ModelUUID: env.Config().UUID(),
	})
}

func (*OpenSuite) TestNewUnknownEnviron(c *gc.C) {
	cfg, err := config.New(config.NoDefaults, dummy.SampleConfig().Merge(
		testing.Attrs{
			"cloud": config.CloudConfig{Type: "wondercloud"}.Attributes(),
		},
	))
	c.Assert(err, jc.ErrorIsNil)
	env, err := environs.New(cfg)
	c.Assert(err, gc.ErrorMatches, "no registered provider for.*")
	c.Assert(env, gc.IsNil)
}

func (*OpenSuite) TestNew(c *gc.C) {
	cfg, err := config.New(config.NoDefaults, dummy.SampleConfig().Merge(
		testing.Attrs{
			"controller": false,
		},
	))
	c.Assert(err, jc.ErrorIsNil)
	e, err := environs.New(cfg)
	c.Assert(err, jc.ErrorIsNil)
	_, err = e.ControllerInstances("uuid")
	c.Assert(err, gc.ErrorMatches, "model is not prepared")
}

func (*OpenSuite) TestPrepare(c *gc.C) {
	baselineAttrs := testing.Attrs{
		"controller": false,
	}
	controllerStore := jujuclienttesting.NewMemStore()
	ctx := envtesting.BootstrapContext(c)
	env, err := environs.Prepare(ctx, controllerStore, environs.PrepareParams{
		ControllerName: "ctrl",
		BaseConfig:     baselineAttrs,
		CloudName:      "dummy",
		CloudConfig:    config.CloudConfig{Type: "dummy"},
	})
	c.Assert(err, jc.ErrorIsNil)

	// Check that an admin-secret was chosen.
	adminSecret := env.Config().AdminSecret()
	c.Assert(adminSecret, gc.HasLen, 32)
	c.Assert(adminSecret, gc.Matches, "^[0-9a-f]*$")

	// Check that the CA cert was generated.
	controllerCfg := controller.Config(env.Config().AllAttrs())
	cfgCertPEM, cfgCertOK := controllerCfg.CACert()
	cfgKeyPEM, cfgKeyOK := controllerCfg.CAPrivateKey()
	c.Assert(cfgCertOK, jc.IsTrue)
	c.Assert(cfgKeyOK, jc.IsTrue)

	// Check the common name of the generated cert
	caCert, _, err := cert.ParseCertAndKey(cfgCertPEM, cfgKeyPEM)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(caCert.Subject.CommonName, gc.Equals, `juju-generated CA for model "controller"`)

	// Check that controller was cached
	foundController, err := controllerStore.ControllerByName("ctrl")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(foundController.ControllerUUID, gc.DeepEquals, env.Config().UUID())
	c.Assert(foundController.Cloud, gc.Equals, "dummy")

	// Check we cannot call Prepare again.
	env, err = environs.Prepare(ctx, controllerStore, environs.PrepareParams{
		ControllerName: "ctrl",
		BaseConfig:     baselineAttrs,
		CloudName:      "dummy",
		CloudConfig:    config.CloudConfig{Type: "dummy"},
	})
	c.Assert(err, jc.Satisfies, errors.IsAlreadyExists)
	c.Assert(err, gc.ErrorMatches, `controller "ctrl" already exists`)
}

func (*OpenSuite) TestPrepareGeneratesDifferentAdminSecrets(c *gc.C) {
	baselineAttrs := testing.Attrs{
		"controller": false,
	}

	ctx := envtesting.BootstrapContext(c)
	env0, err := environs.Prepare(ctx, jujuclienttesting.NewMemStore(), environs.PrepareParams{
		ControllerName: "erewhemos",
		BaseConfig:     baselineAttrs,
		CloudName:      "dummy",
		CloudConfig:    config.CloudConfig{Type: "dummy"},
	})
	c.Assert(err, jc.ErrorIsNil)
	adminSecret0 := env0.Config().AdminSecret()
	c.Assert(adminSecret0, gc.HasLen, 32)
	c.Assert(adminSecret0, gc.Matches, "^[0-9a-f]*$")

	env1, err := environs.Prepare(ctx, jujuclienttesting.NewMemStore(), environs.PrepareParams{
		ControllerName: "erewhemos",
		BaseConfig:     baselineAttrs,
		CloudName:      "dummy",
		CloudConfig:    config.CloudConfig{Type: "dummy"},
	})
	c.Assert(err, jc.ErrorIsNil)
	adminSecret1 := env1.Config().AdminSecret()
	c.Assert(adminSecret1, gc.HasLen, 32)
	c.Assert(adminSecret1, gc.Matches, "^[0-9a-f]*$")

	c.Assert(adminSecret1, gc.Not(gc.Equals), adminSecret0)
}

func (*OpenSuite) TestPrepareWithMissingKey(c *gc.C) {
	cfg := testing.Attrs{
		"controller": false,
		"ca-cert":    string(testing.CACert),
	}
	controllerStore := jujuclienttesting.NewMemStore()
	env, err := environs.Prepare(envtesting.BootstrapContext(c), controllerStore, environs.PrepareParams{
		ControllerName: "ctrl",
		BaseConfig:     cfg,
		CloudName:      "dummy",
		CloudConfig:    config.CloudConfig{Type: "dummy"},
	})
	c.Assert(err, gc.ErrorMatches, "cannot ensure CA certificate: controller configuration with a certificate but no CA private key")
	c.Assert(env, gc.IsNil)
}

func (*OpenSuite) TestPrepareWithExistingKeyPair(c *gc.C) {
	cfg := testing.Attrs{
		"controller":     false,
		"ca-cert":        string(testing.CACert),
		"ca-private-key": string(testing.CAKey),
	}
	ctx := envtesting.BootstrapContext(c)
	env, err := environs.Prepare(ctx, jujuclienttesting.NewMemStore(), environs.PrepareParams{
		ControllerName: "ctrl",
		BaseConfig:     cfg,
		CloudName:      "dummy",
		CloudConfig:    config.CloudConfig{Type: "dummy"},
	})
	c.Assert(err, jc.ErrorIsNil)
	controllerCfg := controller.Config(env.Config().AllAttrs())
	cfgCertPEM, cfgCertOK := controllerCfg.CACert()
	cfgKeyPEM, cfgKeyOK := controllerCfg.CAPrivateKey()
	c.Assert(cfgCertOK, jc.IsTrue)
	c.Assert(cfgKeyOK, jc.IsTrue)
	c.Assert(string(cfgCertPEM), gc.DeepEquals, testing.CACert)
	c.Assert(string(cfgKeyPEM), gc.DeepEquals, testing.CAKey)
}

func (*OpenSuite) TestDestroy(c *gc.C) {
	store := jujuclienttesting.NewMemStore()
	// Prepare the environment and sanity-check that
	// the config storage info has been made.
	ctx := envtesting.BootstrapContext(c)
	e, err := environs.Prepare(ctx, store, environs.PrepareParams{
		ControllerName: "controller-name",
		CloudName:      "dummy",
		CloudConfig:    config.CloudConfig{Type: "dummy"},
	})
	c.Assert(err, jc.ErrorIsNil)
	_, err = store.ControllerByName("controller-name")
	c.Assert(err, jc.ErrorIsNil)

	err = environs.Destroy("controller-name", e, store)
	c.Assert(err, jc.ErrorIsNil)

	// Check that the environment has actually been destroyed
	// and that the controller details been removed too.
	_, err = e.ControllerInstances("not-used")
	c.Assert(err, gc.ErrorMatches, "model is not prepared")
	_, err = store.ControllerByName("controller-name")
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
}
