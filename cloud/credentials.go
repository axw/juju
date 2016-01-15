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

const (
	AttrKey    = "key"
	AttrSecret = "secret"
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

type CredentialSchema struct {
	Fields map[string]CredentialField
}

func ValidateCredential(credential Credential, schemas map[AuthType]CredentialSchema) error {
	schema, ok := schemas[credential.authType]
	if !ok {
		return errors.NotSupportedf("%q auth-type", credential.authType)
	}
	return schema.Validate(credential.attributes)
}

func (s CredentialSchema) Validate(attrs map[string]string) error {
	m := make(map[string]interface{})
	for k, v := range attrs {
		m[k] = v
	}
	fields := make(environschema.Fields)
	for name, field := range s.Fields {
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

/*
var _ Credential = (*AccessKeyCredentials)(nil)

// AccessKeyCredentials represent key/secret credentials.
type AccessKeyCredentials struct {
	// Key is the credential access key.
	Key string `yaml:"key,omitempty"`

	// Secret is the credential access secret.
	Secret string `yaml:"secret,omitempty"`
}

// AuthType is defined on Credentials interface.
func (c *AccessKeyCredentials) AuthType() AuthType {
	return AccessKeyAuthType
}

// Attributes is defined on Credentials interface.
func (c *AccessKeyCredentials) Attributes() map[string]string {
	return map[string]string{
		"key":    c.Key,
		"secret": c.Secret,
	}
}

var _ Credential = (*OpenstackAccessKeyCredentials)(nil)

// OpenstackAccessKeyCredentials are key/secret credentials for Openstack clouds.
type OpenstackAccessKeyCredentials struct {
	AccessKeyCredentials `yaml:",inline"`

	// Tenant is the openstack account tenant.
	Tenant string `yaml:"tenant-name,omitempty"`
}

// AuthType is defined on Credentials interface.
func (c *OpenstackAccessKeyCredentials) AuthType() AuthType {
	return AccessKeyAuthType
}

// Attributes is defined on Credentials interface.
func (c *OpenstackAccessKeyCredentials) Attributes() map[string]string {
	return map[string]string{
		"key":         c.Key,
		"secret":      c.Secret,
		"tenant-name": c.Tenant,
	}
}

// UserPassCredentials are username/password credentials.
type UserPassCredentials struct {
	// User is the credential user name.
	User string `yaml:"username,omitempty"`

	// Password is the credential password.
	Password string `yaml:"password,omitempty"`
}

var _ Credential = (*OpenstackUserPassCredentials)(nil)

// OpenstackUserPassCredentials are user/password credentials for Openstack clouds.
type OpenstackUserPassCredentials struct {
	UserPassCredentials `yaml:",inline"`

	// Tenant is the openstack account tenant.
	Tenant string `yaml:"tenant-name,omitempty"`
}

// AuthType is defined on Credentials interface.
func (c *OpenstackUserPassCredentials) AuthType() AuthType {
	return UserPassAuthType
}

// Attributes is defined on Credentials interface.
func (c *OpenstackUserPassCredentials) Attributes() map[string]string {
	return map[string]string{
		"username":    c.User,
		"password":    c.Password,
		"tenant-name": c.Tenant,
	}
}

var _ Credential = (*AzureUserPassCredentials)(nil)

// AzureUserPassCredentials are user/password credentials for Azure clouds.
type AzureUserPassCredentials struct {
	// Subscription Id is the Azure account subscription id.
	SubscriptionId string `yaml:"subscription-id,omitempty"`

	// TenantId is the Azure Active Directory tenant id.
	TenantId string `yaml:"tenant-id,omitempty"`

	// Application Id is the Azure account application id.
	ApplicationId string `yaml:"application-id,omitempty"`

	// Tenant is the Azure account account password.
	ApplicationPassword string `yaml:"application-password,omitempty"`
}

// AuthType is defined on Credentials interface.
func (c *AzureUserPassCredentials) AuthType() AuthType {
	return UserPassAuthType
}

// Attributes is defined on Credentials interface.
func (c *AzureUserPassCredentials) Attributes() map[string]string {
	return map[string]string{
		"application-id":       c.ApplicationId,
		"application-password": c.ApplicationPassword,
		"subscription-id":      c.SubscriptionId,
		"tenant-id":            c.TenantId,
	}
}

var _ Credential = (*OAuth1Credentials)(nil)

// OAuth1Credentials are oauth1 credentials.
type OAuth1Credentials struct {
	// ConsumerKey is the credential consumer key.
	ConsumerKey string `yaml:"consumer-key,omitempty"`

	// ConsumerSecret is the credential consumer secret.
	ConsumerSecret string `yaml:"consumer-secret,omitempty"`

	// AccessToken is the credential access token.
	AccessToken string `yaml:"access-token,omitempty"`

	// TokenSecret is the credential token secret.
	TokenSecret string `yaml:"token-secret,omitempty"`
}

// AuthType is defined on Credentials interface.
func (c *OAuth1Credentials) AuthType() AuthType {
	return OAuthAuth1Type
}

// Attributes is defined on Credentials interface.
func (c *OAuth1Credentials) Attributes() map[string]string {
	return map[string]string{
		"consumer-key":    c.ConsumerKey,
		"consumer-secret": c.ConsumerSecret,
		"access-token":    c.AccessToken,
		"token-secret":    c.TokenSecret,
	}
}

var _ Credential = (*OAuth2Credentials)(nil)

// OAuth2Credentials are oauth1 credentials.
type OAuth2Credentials struct {
	// Client Id is the credential client id.
	ClientId string `yaml:"client-id,omitempty"`

	// ClientEmail is the credential client email.
	ClientEmail string `yaml:"client-email,omitempty"`

	// PrivateKey is the credential private key.
	PrivateKey string `yaml:"private-key,omitempty"`
}

// AuthType is defined on Credentials interface.
func (c *OAuth2Credentials) AuthType() AuthType {
	return OAuthAuth2Type
}

// Attributes is defined on Credentials interface.
func (c *OAuth2Credentials) Attributes() map[string]string {
	return map[string]string{
		"client-id":    c.ClientId,
		"client-email": c.ClientEmail,
		"private-key":  c.PrivateKey,
	}
}

type EmptyCredentials struct{}

func (EmptyCredentials) AuthType() AuthType {
	return EmptyAuthType
}

func (EmptyCredentials) Attributes() map[string]string {
	return map[string]string{}
}
*/

func EmptyCredential() Credential {
	return Credential{EmptyAuthType, nil}
}
