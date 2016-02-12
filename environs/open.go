// Copyright 2011, 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package environs

import (
	"crypto/rand"
	"fmt"
	"io"
	"time"

	"github.com/juju/errors"
	"github.com/juju/utils"

	"github.com/juju/juju/cert"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/jujuclient"
)

// New returns a new environment based on the provided configuration.
func New(config *config.Config) (Environ, error) {
	p, err := Provider(config.Type())
	if err != nil {
		return nil, errors.Trace(err)
	}
	return p.Open(config)
}

// Prepare prepares a new environment based on the provided configuration.
// It is an error to prepare a environment if there already exists an
// entry in the config store with that name.
//
// TODO(axw) this should be called PrepareController, or maybe
// PrepareControllerEnviron.
func Prepare(
	ctx BootstrapContext,
	clientStore jujuclient.ClientStore,
	controllerName string,
	args PrepareForBootstrapParams,
) (Environ, error) {

	if _, err := clientStore.ControllerByName(controllerName); err == nil {
		return nil, errors.AlreadyExistsf("controller %q", controllerName)
	} else if !errors.IsNotFound(err) {
		return nil, errors.Annotatef(err, "error getting controller %q details", controllerName)
	}

	p, err := Provider(args.Config.Type())
	if err != nil {
		return nil, errors.Trace(err)
	}
	env, err := prepare(ctx, p, args)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if err := decorateAndWriteInfo(clientStore, controllerName, env.Config()); err != nil {
		return nil, errors.Annotatef(err, "cannot writing controller %q details", controllerName)
	}
	return env, nil
}

// decorateAndWriteInfo writes the details in the given store for the
// newly prepared controller, account and admin model.
func decorateAndWriteInfo(
	store jujuclient.ClientStore,
	controllerName string,
	cfg *config.Config,
) error {

	// TODO(axw) we need to record input to PrepareForBootstrap
	// in the client, so we can force-destroy a broken controller.

	// These things are validated by the client store, so there's
	// no need to check here. Also, we just set them in prepare.
	caCert, _ := cfg.CACert()
	uuid, _ := cfg.UUID()

	controllerDetails := jujuclient.ControllerDetails{
		nil, // No servers yet
		uuid,
		nil, // No addresses yet
		caCert,
	}
	if err := store.UpdateController(controllerName, controllerDetails); err != nil {
		return errors.Annotate(err, "writing controller details")
	}

	accountDetails := jujuclient.AccountDetails{
		// TODO(axw) this should be captured in a constant somewhere.
		User:     "admin@local",
		Password: cfg.AdminSecret(),
	}
	if err := store.UpdateAccount(controllerName, accountDetails.User, accountDetails); err != nil {
		return errors.Annotate(err, "writing account details")
	}
	if err := store.SetCurrentAccount(controllerName, accountDetails.User); err != nil {
		return errors.Annotate(err, "setting current account")
	}

	modelDetails := jujuclient.ModelDetails{uuid}
	if err := store.UpdateModel(controllerName, cfg.Name(), modelDetails); err != nil {
		return errors.Annotate(err, "writing admin model details")
	}
	if err := store.SetCurrentModel(controllerName, cfg.Name()); err != nil {
		return errors.Annotate(err, "setting current mode")
	}
	return nil
}

func prepare(ctx BootstrapContext, p EnvironProvider, args PrepareForBootstrapParams) (Environ, error) {
	cfg, err := ensureAdminSecret(args.Config)
	if err != nil {
		return nil, errors.Annotate(err, "cannot generate admin-secret")
	}
	cfg, err = ensureCertificate(cfg)
	if err != nil {
		return nil, errors.Annotate(err, "cannot ensure CA certificate")
	}
	cfg, err = ensureUUID(cfg)
	if err != nil {
		return nil, errors.Annotate(err, "cannot ensure uuid")
	}
	args.Config = cfg
	return p.PrepareForBootstrap(ctx, args)
}

// ensureAdminSecret returns a config with a non-empty admin-secret.
func ensureAdminSecret(cfg *config.Config) (*config.Config, error) {
	if cfg.AdminSecret() != "" {
		return cfg, nil
	}
	return cfg.Apply(map[string]interface{}{
		"admin-secret": randomKey(),
	})
}

func randomKey() string {
	buf := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, buf)
	if err != nil {
		panic(fmt.Errorf("error from crypto rand: %v", err))
	}
	return fmt.Sprintf("%x", buf)
}

// ensureCertificate generates a new CA certificate and
// attaches it to the given controller configuration,
// unless the configuration already has one.
func ensureCertificate(cfg *config.Config) (*config.Config, error) {
	_, hasCACert := cfg.CACert()
	_, hasCAKey := cfg.CAPrivateKey()
	if hasCACert && hasCAKey {
		return cfg, nil
	}
	if hasCACert && !hasCAKey {
		return nil, fmt.Errorf("controller configuration with a certificate but no CA private key")
	}

	caCert, caKey, err := cert.NewCA(cfg.Name(), time.Now().UTC().AddDate(10, 0, 0))
	if err != nil {
		return nil, err
	}
	return cfg.Apply(map[string]interface{}{
		"ca-cert":        string(caCert),
		"ca-private-key": string(caKey),
	})
}

// ensureUUID generates a new uuid and attaches it to
// the given environment configuration, unless the
// configuration already has one.
func ensureUUID(cfg *config.Config) (*config.Config, error) {
	_, hasUUID := cfg.UUID()
	if hasUUID {
		return cfg, nil
	}
	uuid, err := utils.NewUUID()
	if err != nil {
		return nil, errors.Trace(err)
	}
	return cfg.Apply(map[string]interface{}{
		"uuid": uuid.String(),
	})
}

// Destroy destroys the controller and, if successful,
// removes it from the client.
//
// TODO(axw) this should be called DestroyController,
// or maybe DestroyControllerEnviron.
func Destroy(
	controllerName string,
	env Environ,
	controllerRemover jujuclient.ControllerRemover,
) error {
	if err := env.Destroy(); err != nil {
		return errors.Trace(err)
	}
	return errors.Annotate(
		controllerRemover.RemoveController(controllerName),
		"cannot remove controller details",
	)
}
