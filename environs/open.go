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
	"github.com/juju/juju/cloud"
	"github.com/juju/juju/controller"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/jujuclient"
)

// ControllerModelName is the name of the admin model in each controller.
const ControllerModelName = "controller"

// AdminUser is the initial admin user created for all controllers.
const AdminUser = "admin@local"

// New returns a new environment based on the provided configuration.
func New(config *config.Config) (Environ, error) {
	p, err := Provider(config.Type())
	if err != nil {
		return nil, errors.Trace(err)
	}
	return p.Open(config)
}

// PrepareParams contains the parameters for preparing a controller Environ
// for bootstrapping.
type PrepareParams struct {
	// Base Config contains the user-supplied configuration for the
	// controller and model. This does not include UUIDs, cloud type,
	// or model name.
	BaseConfig map[string]interface{}

	// ControllerName is the name of the controller being prepared.
	ControllerName string

	// CloudName is the name of the cloud that the controller is being
	// prepared for.
	CloudName string

	// CloudConfig contains the cloud configuration that will be used
	// for the controller model.
	CloudConfig config.CloudConfig

	// Credential is the credential to use to bootstrap.
	Credential cloud.Credential

	// CredentialName is the name of the credential to use to bootstrap.
	// This will be empty for auto-detected credentials.
	CredentialName string
}

// Prepare prepares a new controller based on the provided configuration.
// It is an error to prepare a controller if there already exists an
// entry in the client store with the same name.
//
// Upon success, Prepare will update the ClientStore with the details of
// the controller, admin account, and admin model.
func Prepare(
	ctx BootstrapContext,
	store jujuclient.ClientStore,
	args PrepareParams,
) (Environ, error) {

	_, err := store.ControllerByName(args.ControllerName)
	if err == nil {
		return nil, errors.AlreadyExistsf("controller %q", args.ControllerName)
	} else if !errors.IsNotFound(err) {
		return nil, errors.Annotatef(err, "error reading controller %q info", args.ControllerName)
	}

	p, err := Provider(args.CloudConfig.Type)
	if err != nil {
		return nil, errors.Trace(err)
	}

	env, details, err := prepare(ctx, p, args)
	if err != nil {
		return nil, errors.Trace(err)
	}

	if err := decorateAndWriteInfo(
		store, details, args.ControllerName, ControllerModelName,
	); err != nil {
		return nil, errors.Annotatef(err, "cannot create controller %q info", args.ControllerName)
	}
	return env, nil
}

// decorateAndWriteInfo decorates the info struct with information
// from the given cfg, and the writes that out to the filesystem.
func decorateAndWriteInfo(
	store jujuclient.ClientStore,
	details prepareDetails,
	controllerName, modelName string,
) error {
	accountName := details.User
	if err := store.UpdateController(controllerName, details.ControllerDetails); err != nil {
		return errors.Trace(err)
	}
	if err := store.UpdateBootstrapConfig(controllerName, details.BootstrapConfig); err != nil {
		return errors.Trace(err)
	}
	if err := store.UpdateAccount(controllerName, accountName, details.AccountDetails); err != nil {
		return errors.Trace(err)
	}
	if err := store.SetCurrentAccount(controllerName, accountName); err != nil {
		return errors.Trace(err)
	}
	if err := store.UpdateModel(controllerName, accountName, modelName, details.ModelDetails); err != nil {
		return errors.Trace(err)
	}
	if err := store.SetCurrentModel(controllerName, accountName, modelName); err != nil {
		return errors.Trace(err)
	}
	return nil
}

func prepare(
	ctx BootstrapContext,
	p EnvironProvider,
	args PrepareParams,
) (Environ, prepareDetails, error) {
	var details prepareDetails

	controllerUUID, err := utils.NewUUID()
	if err != nil {
		return nil, details, errors.Trace(err)
	}
	attrs := map[string]interface{}{
		config.NameKey:               ControllerModelName,
		config.UUIDKey:               controllerUUID.String(),
		controller.ControllerUUIDKey: controllerUUID.String(), // TODO delete? need to pass through another way
		config.CloudKey:              args.CloudConfig.Attributes(),
		config.TypeKey:               args.CloudConfig.Type, // TODO(axw) delete
	}
	if args.Credential.AuthType() != "" {
		attrs[config.CredentialsKey] = config.CredentialAttributes(args.Credential)
	}
	for k, v := range args.BaseConfig {
		if _, ok := attrs[k]; ok {
			return nil, details, errors.Errorf("%q may not be specified in config", k)
		}
		attrs[k] = v
	}

	cfg, err := config.New(config.UseDefaults, attrs)
	if err != nil {
		return nil, details, errors.Trace(err)
	}
	cfg, adminSecret, err := ensureAdminSecret(cfg)
	if err != nil {
		return nil, details, errors.Annotate(err, "cannot generate admin-secret")
	}
	cfg, caCert, err := ensureCertificate(cfg)
	if err != nil {
		return nil, details, errors.Annotate(err, "cannot ensure CA certificate")
	}

	cfg, err = p.BootstrapConfig(BootstrapConfigParams{
		cfg, args.Credential,
		args.CloudConfig.Region,
		args.CloudConfig.Endpoint,
		args.CloudConfig.StorageEndpoint,
	})
	if err != nil {
		return nil, details, errors.Trace(err)
	}
	env, err := p.PrepareForBootstrap(ctx, cfg)
	if err != nil {
		return nil, details, errors.Trace(err)
	}

	// We store the base configuration only; we don't want the
	// default attributes, generated secrets/certificates, or
	// UUIDs stored in the bootstrap config. Make a copy, so
	// we don't disturb the caller's config map.
	details.Config = make(map[string]interface{})
	for k, v := range args.BaseConfig {
		details.Config[k] = v
	}

	details.CACert = caCert
	details.ControllerUUID = controllerUUID.String()
	details.User = AdminUser
	details.Password = adminSecret
	details.ModelUUID = controllerUUID.String()
	details.ControllerDetails.Cloud = args.CloudName
	details.ControllerDetails.CloudRegion = args.CloudConfig.Region
	details.BootstrapConfig.Cloud = args.CloudName
	details.BootstrapConfig.CloudRegion = args.CloudConfig.Region
	details.CloudEndpoint = args.CloudConfig.Endpoint
	details.CloudStorageEndpoint = args.CloudConfig.StorageEndpoint
	details.Credential = args.CredentialName

	return env, details, nil
}

type prepareDetails struct {
	jujuclient.ControllerDetails
	jujuclient.BootstrapConfig
	jujuclient.AccountDetails
	jujuclient.ModelDetails
}

// ensureAdminSecret returns a config with a non-empty admin-secret.
func ensureAdminSecret(cfg *config.Config) (*config.Config, string, error) {
	if secret := cfg.AdminSecret(); secret != "" {
		return cfg, secret, nil
	}

	// Generate a random string.
	buf := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return nil, "", errors.Annotate(err, "generating random secret")
	}
	secret := fmt.Sprintf("%x", buf)

	cfg, err := cfg.Apply(map[string]interface{}{"admin-secret": secret})
	if err != nil {
		return nil, "", errors.Trace(err)
	}
	return cfg, secret, nil
}

// ensureCertificate generates a new CA certificate and
// attaches it to the given controller configuration,
// unless the configuration already has one.
func ensureCertificate(cfg *config.Config) (*config.Config, string, error) {
	controllerCfg := controller.ControllerConfig(cfg.AllAttrs())
	caCert, hasCACert := controllerCfg.CACert()
	_, hasCAKey := controllerCfg.CAPrivateKey()
	if hasCACert && hasCAKey {
		return cfg, caCert, nil
	}
	if hasCACert && !hasCAKey {
		return nil, "", errors.Errorf("controller configuration with a certificate but no CA private key")
	}

	// TODO(perrito666) 2016-05-02 lp:1558657
	caCert, caKey, err := cert.NewCA(ControllerModelName, cfg.UUID(), time.Now().UTC().AddDate(10, 0, 0))
	if err != nil {
		return nil, "", errors.Trace(err)
	}
	cfg, err = cfg.Apply(map[string]interface{}{
		controller.CACertKey:    string(caCert),
		controller.CAPrivateKey: string(caKey),
	})
	if err != nil {
		return nil, "", errors.Trace(err)
	}
	return cfg, string(caCert), nil
}

// Destroy destroys the controller and, if successful,
// its associated configuration data from the given store.
func Destroy(
	controllerName string,
	env Environ,
	store jujuclient.ControllerStore,
) error {
	details, err := store.ControllerByName(controllerName)
	if err == nil {
		if err := env.DestroyController(details.ControllerUUID); err != nil {
			return errors.Trace(err)
		}
		if err := store.RemoveController(controllerName); err != nil {
			return errors.Trace(err)
		}
	} else if !errors.IsNotFound(err) {
		return errors.Trace(err)
	}
	return nil
}
