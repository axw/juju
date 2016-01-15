// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloud

import (
	"strings"

	"github.com/juju/errors"
	"github.com/juju/schema"
	"gopkg.in/juju/environschema.v1"
	"gopkg.in/yaml.v1"

	"github.com/juju/juju/juju/osenv"
)

// Credentials is a struct containing cloud credential information.
type Credentials struct {
	// Credentials is a map of cloud credentials, keyed on cloud name.
	Credentials map[string]CloudCredential `yaml:"credentials"`
}

// CloudCredential contains attributes used to define credentials for a cloud.
type CloudCredential struct {
	// DefaultCredential is the named credential to use by default.
	DefaultCredential string `yaml:"default-credential,omitempty"`

	// DefaultRegion is the cloud region to use by default.
	DefaultRegion string `yaml:"default-region,omitempty"`

	// AuthCredentials is the credentials for a cloud, keyed on name.
	AuthCredentials map[string]Credential `yaml:",omitempty,inline"`
}

// Credential instances represent cloud credentials.
type Credential struct {
	authType   AuthType
	attributes map[string]string
}

// AuthType returns the authentication type.
func (c Credential) AuthType() AuthType {
	return c.authType
}

// Attributes returns the credential attributes.
func (c Credential) Attributes() map[string]string {
	m := make(map[string]string)
	for k, v := range c.attributes {
		m[k] = v
	}
	return m
}

type CredentialFields map[string]CredentialField

func ValidateCredential(credential Credential, schemas map[AuthType]CredentialFields) error {
	schema, ok := schemas[credential.authType]
	if !ok {
		return errors.NotSupportedf("%q auth-type", credential.authType)
	}
	return schema.Validate(credential.attributes)
}

func (s CredentialFields) Validate(attrs map[string]string) error {
	m := make(map[string]interface{})
	for k, v := range attrs {
		m[k] = v
	}
	fields := make(environschema.Fields)
	for name, field := range s {
		fields[name] = environschema.Attr{
			Description: field.Description,
			Type:        environschema.Tstring,
			Group:       environschema.AccountGroup,
			Immutable:   false,
			Mandatory:   true,
			Secret:      field.Secret,
			EnvVar:      field.EnvVar,
			EnvVars:     field.EnvVars,
		}
	}
	schemaFields, schemaDefaults, err := fields.ValidationSchema()
	if err != nil {
		return errors.Trace(err)
	}
	checker := schema.FieldMap(schemaFields, schemaDefaults)
	if _, err := checker.Coerce(m, nil); err != nil {
		return errors.Trace(err)
	}
	return nil
}

type CredentialField struct {
	Description string
	Secret      bool
	EnvVar      string
	EnvVars     []string
}

type cloudCredentialChecker struct{}

func (c cloudCredentialChecker) Coerce(v interface{}, path []string) (interface{}, error) {
	out := CloudCredential{
		AuthCredentials: make(map[string]Credential),
	}
	v, err := schema.StringMap(cloudCredentialValueChecker{}).Coerce(v, path)
	if err != nil {
		return nil, err
	}
	mapv := v.(map[string]interface{})
	for k, v := range mapv {
		switch k {
		case "default-region":
			out.DefaultRegion = v.(string)
		case "default-credential":
			out.DefaultCredential = v.(string)
		default:
			out.AuthCredentials[k] = v.(Credential)
		}
	}
	return out, nil
}

type cloudCredentialValueChecker struct{}

func (c cloudCredentialValueChecker) Coerce(v interface{}, path []string) (interface{}, error) {
	field := path[len(path)-1]
	switch field {
	case "default-region", "default-credential":
		return schema.String().Coerce(v, path)
	}
	v, err := schema.StringMap(schema.String()).Coerce(v, path)
	if err != nil {
		return nil, err
	}
	mapv := v.(map[string]interface{})

	authType, _ := mapv["auth-type"].(string)
	if authType == "" {
		return nil, errors.Errorf("%v: missing auth-type", strings.Join(path, ""))
	}

	attrs := make(map[string]string)
	delete(mapv, "auth-type")
	for k, v := range mapv {
		attrs[k] = v.(string)
	}
	return Credential{AuthType(authType), attrs}, nil
}

// JujuCredentials is the location where credentials are
// expected to be found. Requires JUJU_HOME to be set.
func JujuCredentials() string {
	return osenv.JujuHomePath("credentials.yaml")
}

// ParseCredentials parses the given yaml bytes into Credentials, but does
// not validate the credential attributes.
func ParseCredentials(data []byte) (*Credentials, error) {
	var credentialsYAML struct {
		Credentials map[string]interface{} `yaml:"credentials"`
	}
	err := yaml.Unmarshal(data, &credentialsYAML)
	if err != nil {
		return nil, errors.Annotate(err, "cannot unmarshal yaml credentials")
	}
	credentials := Credentials{make(map[string]CloudCredential)}
	for cloud, v := range credentialsYAML.Credentials {
		v, err := cloudCredentialChecker{}.Coerce(
			v, []string{"credentials." + cloud},
		)
		if err != nil {
			return nil, errors.Trace(err)
		}
		credentials.Credentials[cloud] = v.(CloudCredential)
	}
	return &credentials, nil
}

func EmptyCredential() Credential {
	return Credential{EmptyAuthType, nil}
}
