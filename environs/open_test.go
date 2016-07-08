// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environs_test

import (
	"github.com/juju/errors"
	jc "github.com/juju/testing/checkers"
	"github.com/juju/utils"
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
	"github.com/juju/juju/juju/keys"
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
	s.PatchValue(&keys.JujuPublicKey, sstesting.SignedMetadataPublicKey)
}

func (s *OpenSuite) TearDownTest(c *gc.C) {
	dummy.Reset(c)
	s.ToolsFixture.TearDownTest(c)
	s.FakeJujuXDGDataHomeSuite.TearDownTest(c)
}

func (s *OpenSuite) TestNewDummyEnviron(c *gc.C) {
	s.PatchValue(&jujuversion.Current, testing.FakeVersionNumber)
	// matches *Settings.Map()
	cfg, err := config.New(config.NoDefaults, dummySampleConfig())
	c.Assert(err, jc.ErrorIsNil)
	ctx := envtesting.BootstrapContext(c)
	cache := jujuclienttesting.NewMemStore()
	controllerCfg := testing.FakeControllerConfig()
	env, err := environs.Prepare(ctx, cache, environs.PrepareParams{
		ControllerConfig: controllerCfg,
		ControllerName:   cfg.Name(),
		BaseConfig:       cfg.AllAttrs(),
		CloudName:        "dummy",
	})
	c.Assert(err, jc.ErrorIsNil)

	storageDir := c.MkDir()
	s.PatchValue(&envtools.DefaultBaseURL, storageDir)
	stor, err := filestorage.NewFileStorageWriter(storageDir)
	c.Assert(err, jc.ErrorIsNil)
	envtesting.UploadFakeTools(c, stor, cfg.AgentStream(), cfg.AgentStream())
	err = bootstrap.Bootstrap(ctx, env, bootstrap.BootstrapParams{
		ControllerConfig: controllerCfg,
	})
	c.Assert(err, jc.ErrorIsNil)

	// New controller should have been added to collection.
	foundController, err := cache.ControllerByName(cfg.Name())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(foundController.ControllerUUID, gc.DeepEquals, cfg.UUID())
}

func (s *OpenSuite) TestUpdateEnvInfo(c *gc.C) {
	store := jujuclienttesting.NewMemStore()
	ctx := envtesting.BootstrapContext(c)
	uuid := utils.MustNewUUID().String()
	cfg, err := config.New(config.UseDefaults, map[string]interface{}{
		"type": "dummy",
		"name": "admin-model",
		"uuid": uuid,
	})
	c.Assert(err, jc.ErrorIsNil)
	controllerCfg := testing.FakeControllerConfig()
	controllerCfg["controller-uuid"] = uuid
	_, err = environs.Prepare(ctx, store, environs.PrepareParams{
		ControllerConfig: controllerCfg,
		ControllerName:   "controller-name",
		BaseConfig:       cfg.AllAttrs(),
		CloudName:        "dummy",
	})
	c.Assert(err, jc.ErrorIsNil)

	foundController, err := store.ControllerByName("controller-name")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(foundController.ControllerUUID, gc.Not(gc.Equals), "")
	c.Assert(foundController.CACert, gc.Not(gc.Equals), "")
	foundModel, err := store.ModelByName("controller-name", "admin@local", "admin-model")
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(foundModel, jc.DeepEquals, &jujuclient.ModelDetails{
		ModelUUID: cfg.UUID(),
	})
}

func (*OpenSuite) TestNewUnknownEnviron(c *gc.C) {
	cfg, err := config.New(config.NoDefaults, dummy.SampleConfig().Merge(
		testing.Attrs{
			"type": "wondercloud",
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
			"name":       "erewhemos",
		},
	))
	c.Assert(err, jc.ErrorIsNil)
	e, err := environs.New(cfg)
	c.Assert(err, jc.ErrorIsNil)
	_, err = e.ControllerInstances("uuid")
	c.Assert(err, gc.ErrorMatches, "model is not prepared")
}

func (*OpenSuite) TestPrepare(c *gc.C) {
	baselineAttrs := dummy.SampleConfig().Merge(testing.Attrs{
		"controller": false,
		"name":       "erewhemos",
	}).Delete(
		"admin-secret",
	)
	cfg, err := config.New(config.NoDefaults, baselineAttrs)
	c.Assert(err, jc.ErrorIsNil)
	controllerStore := jujuclienttesting.NewMemStore()
	ctx := envtesting.BootstrapContext(c)
	controllerCfg := controller.Config{controller.ControllerUUIDKey: testing.ModelTag.Id()}
	env, err := environs.Prepare(ctx, controllerStore, environs.PrepareParams{
		ControllerConfig: controllerCfg,
		ControllerName:   cfg.Name(),
		BaseConfig:       cfg.AllAttrs(),
		CloudName:        "dummy",
	})
	c.Assert(err, jc.ErrorIsNil)

	// Check that an admin-secret was chosen.
	adminSecret := env.Config().AdminSecret()
	c.Assert(adminSecret, gc.HasLen, 32)
	c.Assert(adminSecret, gc.Matches, "^[0-9a-f]*$")

	// Check that the CA cert was generated.
	cfgCertPEM, cfgCertOK := controllerCfg.CACert()
	cfgKeyPEM, cfgKeyOK := controllerCfg.CAPrivateKey()
	c.Assert(cfgCertOK, jc.IsTrue)
	c.Assert(cfgKeyOK, jc.IsTrue)

	// Check the common name of the generated cert
	caCert, _, err := cert.ParseCertAndKey(cfgCertPEM, cfgKeyPEM)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(caCert.Subject.CommonName, gc.Equals, `juju-generated CA for model "`+testing.SampleModelName+`"`)

	// Check that controller was cached
	foundController, err := controllerStore.ControllerByName(cfg.Name())
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(foundController.ControllerUUID, gc.DeepEquals, cfg.UUID())
	c.Assert(foundController.Cloud, gc.Equals, "dummy")

	// Check we cannot call Prepare again.
	env, err = environs.Prepare(ctx, controllerStore, environs.PrepareParams{
		ControllerConfig: controllerCfg,
		ControllerName:   cfg.Name(),
		BaseConfig:       cfg.AllAttrs(),
		CloudName:        "dummy",
	})
	c.Assert(err, jc.Satisfies, errors.IsAlreadyExists)
	c.Assert(err, gc.ErrorMatches, `controller "erewhemos" already exists`)
}

func (*OpenSuite) TestPrepareGeneratesDifferentAdminSecrets(c *gc.C) {
	baselineAttrs := dummy.SampleConfig().Merge(testing.Attrs{
		"controller": false,
		"name":       "erewhemos",
	}).Delete(
		"admin-secret",
	)

	ctx := envtesting.BootstrapContext(c)
	env0, err := environs.Prepare(ctx, jujuclienttesting.NewMemStore(), environs.PrepareParams{
		ControllerConfig: testing.FakeControllerConfig(),
		ControllerName:   "erewhemos",
		BaseConfig:       baselineAttrs,
		CloudName:        "dummy",
	})
	c.Assert(err, jc.ErrorIsNil)
	adminSecret0 := env0.Config().AdminSecret()
	c.Assert(adminSecret0, gc.HasLen, 32)
	c.Assert(adminSecret0, gc.Matches, "^[0-9a-f]*$")

	// Allocate a new UUID, or we'll end up with the same config.
	newUUID := utils.MustNewUUID()
	baselineAttrs[config.UUIDKey] = newUUID.String()
	controllerCfg := testing.FakeControllerConfig()
	controllerCfg[controller.ControllerUUIDKey] = newUUID.String()

	env1, err := environs.Prepare(ctx, jujuclienttesting.NewMemStore(), environs.PrepareParams{
		ControllerConfig: controllerCfg,
		ControllerName:   "erewhemos",
		BaseConfig:       baselineAttrs,
		CloudName:        "dummy",
	})
	c.Assert(err, jc.ErrorIsNil)
	adminSecret1 := env1.Config().AdminSecret()
	c.Assert(adminSecret1, gc.HasLen, 32)
	c.Assert(adminSecret1, gc.Matches, "^[0-9a-f]*$")

	c.Assert(adminSecret1, gc.Not(gc.Equals), adminSecret0)
}

func (*OpenSuite) TestPrepareWithMissingKey(c *gc.C) {
	cfg, err := config.New(config.NoDefaults, dummy.SampleConfig().Merge(
		testing.Attrs{
			"controller": false,
			"name":       "erewhemos",
		},
	))
	c.Assert(err, jc.ErrorIsNil)
	controllerStore := jujuclienttesting.NewMemStore()
	controllerCfg := testing.FakeControllerConfig()
	delete(controllerCfg, "ca-private-key")
	env, err := environs.Prepare(envtesting.BootstrapContext(c), controllerStore, environs.PrepareParams{
		ControllerConfig: controllerCfg,
		ControllerName:   cfg.Name(),
		BaseConfig:       cfg.AllAttrs(),
		CloudName:        "dummy",
	})
	c.Assert(err, gc.ErrorMatches, "cannot ensure CA certificate: controller configuration with a certificate but no CA private key")
	c.Assert(env, gc.IsNil)
}

func (*OpenSuite) TestPrepareWithExistingKeyPair(c *gc.C) {
	cfg, err := config.New(config.NoDefaults, dummy.SampleConfig().Merge(
		testing.Attrs{
			"controller": false,
			"name":       "erewhemos",
		},
	))
	c.Assert(err, jc.ErrorIsNil)
	ctx := envtesting.BootstrapContext(c)
	controllerCfg := testing.FakeControllerConfig()
	_, err = environs.Prepare(ctx, jujuclienttesting.NewMemStore(), environs.PrepareParams{
		ControllerConfig: controllerCfg,
		ControllerName:   cfg.Name(),
		BaseConfig:       cfg.AllAttrs(),
		CloudName:        "dummy",
	})
	c.Assert(err, jc.ErrorIsNil)
	cfgCertPEM, cfgCertOK := controllerCfg.CACert()
	cfgKeyPEM, cfgKeyOK := controllerCfg.CAPrivateKey()
	c.Assert(cfgCertOK, jc.IsTrue)
	c.Assert(cfgKeyOK, jc.IsTrue)
	c.Assert(string(cfgCertPEM), gc.DeepEquals, testing.CACert)
	c.Assert(string(cfgKeyPEM), gc.DeepEquals, testing.CAKey)
}

func (*OpenSuite) TestDestroy(c *gc.C) {
	cfg, err := config.New(config.NoDefaults, dummy.SampleConfig().Merge(
		testing.Attrs{
			"name": "erewhemos",
		},
	))
	c.Assert(err, jc.ErrorIsNil)

	store := jujuclienttesting.NewMemStore()
	// Prepare the environment and sanity-check that
	// the config storage info has been made.
	controllerCfg := testing.FakeControllerConfig()
	ctx := envtesting.BootstrapContext(c)
	e, err := environs.Prepare(ctx, store, environs.PrepareParams{
		ControllerConfig: controllerCfg,
		ControllerName:   "controller-name",
		BaseConfig:       cfg.AllAttrs(),
		CloudName:        "dummy",
	})
	c.Assert(err, jc.ErrorIsNil)
	_, err = store.ControllerByName("controller-name")
	c.Assert(err, jc.ErrorIsNil)

	err = environs.Destroy("controller-name", e, store)
	c.Assert(err, jc.ErrorIsNil)

	// Check that the environment has actually been destroyed
	// and that the controller details been removed too.
	_, err = e.ControllerInstances(controllerCfg.ControllerUUID())
	c.Assert(err, gc.ErrorMatches, "model is not prepared")
	_, err = store.ControllerByName("controller-name")
	c.Assert(err, jc.Satisfies, errors.IsNotFound)
}
