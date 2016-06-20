// Copyright 2011, 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package ec2

import (
	"fmt"

	"github.com/juju/errors"
	"github.com/juju/schema"
	"gopkg.in/amz.v3/aws"
	"gopkg.in/juju/environschema.v1"

	"github.com/juju/juju/cloud"
	"github.com/juju/juju/environs/config"
)

var configSchema = environschema.Fields{
	"vpc-id": {
		Description: "Use a specific AWS VPC ID (optional). When not specified, Juju requires a default VPC or EC2-Classic features to be available for the account/region.",
		Example:     "vpc-a1b2c3d4",
		Type:        environschema.Tstring,
		Group:       environschema.AccountGroup,
		Immutable:   true,
	},
	"vpc-id-force": {
		Description: "Force Juju to use the AWS VPC ID specified with vpc-id, when it fails the minimum validation criteria. Not accepted without vpc-id",
		Type:        environschema.Tbool,
		Group:       environschema.AccountGroup,
		Immutable:   true,
	},
}

var configFields = func() schema.Fields {
	fs, _, err := configSchema.ValidationSchema()
	if err != nil {
		panic(err)
	}
	return fs
}()

var configDefaults = schema.Defaults{
	"vpc-id":       "",
	"vpc-id-force": false,
}

type environConfig struct {
	*config.Config
	cloud config.CloudConfig
	cred  cloud.Credential
	attrs map[string]interface{}
}

func (c *environConfig) region() string {
	return c.cloud.Region
}

func (c *environConfig) accessKey() string {
	return c.cred.Attributes()["access-key"]
}

func (c *environConfig) secretKey() string {
	return c.cred.Attributes()["secret-key"]
}

func (c *environConfig) vpcID() string {
	return c.attrs["vpc-id"].(string)
}

func (c *environConfig) forceVPCID() bool {
	return c.attrs["vpc-id-force"].(bool)
}

func (p environProvider) newConfig(cfg *config.Config) (*environConfig, error) {
	valid, err := p.Validate(cfg, nil)
	if err != nil {
		return nil, err
	}
	credentials, _ := valid.Credentials()
	return &environConfig{
		valid,
		valid.Cloud(),
		credentials,
		valid.UnknownAttrs(),
	}, nil
}

// Schema returns the configuration schema for an environment.
func (environProvider) Schema() environschema.Fields {
	fields, err := config.Schema(configSchema)
	if err != nil {
		panic(err)
	}
	return fields
}

func validateConfig(cfg, old *config.Config) (*environConfig, error) {
	// Check for valid changes for the base config values.
	if err := config.Validate(cfg, old); err != nil {
		return nil, err
	}
	validated, err := cfg.ValidateUnknownAttrs(configFields, configDefaults)
	if err != nil {
		return nil, err
	}

	credentials, ok := cfg.Credentials()
	if !ok {
		return nil, errors.NewNotValid(nil, "missing cloud credentials")
	}
	if authType := credentials.AuthType(); authType != cloud.AccessKeyAuthType {
		return nil, errors.NotValidf("%q auth-type", authType)
	}

	ecfg := &environConfig{
		cfg,
		cfg.Cloud(),
		credentials,
		validated,
	}

	if _, ok := aws.Regions[ecfg.region()]; !ok {
		return nil, fmt.Errorf("invalid region name %q", ecfg.region())
	}

	if vpcID := ecfg.vpcID(); isVPCIDSetButInvalid(vpcID) {
		return nil, fmt.Errorf("vpc-id: %q is not a valid AWS VPC ID", vpcID)
	} else if !isVPCIDSet(vpcID) && ecfg.forceVPCID() {
		return nil, fmt.Errorf("cannot use vpc-id-force without specifying vpc-id as well")
	}

	if old != nil {
		attrs := old.UnknownAttrs()

		if vpcID, _ := attrs["vpc-id"].(string); vpcID != ecfg.vpcID() {
			return nil, fmt.Errorf("cannot change vpc-id from %q to %q", vpcID, ecfg.vpcID())
		}

		if forceVPCID, _ := attrs["vpc-id-force"].(bool); forceVPCID != ecfg.forceVPCID() {
			return nil, fmt.Errorf("cannot change vpc-id-force from %v to %v", forceVPCID, ecfg.forceVPCID())
		}
	}

	// ssl-hostname-verification cannot be disabled
	if !ecfg.SSLHostnameVerification() {
		return nil, fmt.Errorf("disabling ssh-hostname-verification is not supported")
	}
	return ecfg, nil
}
