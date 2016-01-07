// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloud_test

import (
	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/juju/juju/cloud"
)

type credentialsSuite struct{}

var _ = gc.Suite(&credentialsSuite{})

func (s *credentialsSuite) TestMarshallAccessKey(c *gc.C) {
	creds := cloud.Credentials{
		Credentials: map[string]cloud.CloudCredential{
			"aws": {
				DefaultCredential: "default-cred",
				DefaultRegion:     "us-west-2",
				AuthCredentials: map[string]cloud.Credential{
					"peter": &cloud.AccessKeyCredentials{
						Key:    "key",
						Secret: "secret",
					},
					// TODO(wallyworld) - add anther credential once goyaml.v2 supports inline MapSlice.
					//"paul": &cloud.AccessKeyCredentials{
					//	Key: "paulkey",
					//	Secret: "paulsecret",
					//},
				},
			},
		},
	}
	out, err := yaml.Marshal(creds)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(out), gc.Equals, `
credentials:
  aws:
    default-credential: default-cred
    default-region: us-west-2
    peter:
      key: key
      secret: secret
`[1:])
}

func (s *credentialsSuite) TestMarshallOpenstackAccessKey(c *gc.C) {
	creds := cloud.Credentials{
		Credentials: map[string]cloud.CloudCredential{
			"openstack": {
				DefaultCredential: "default-cred",
				DefaultRegion:     "region-a",
				AuthCredentials: map[string]cloud.Credential{
					"peter": &cloud.OpenstackAccessKeyCredentials{
						AccessKeyCredentials: cloud.AccessKeyCredentials{
							Key:    "key",
							Secret: "secret",
						},
						Tenant: "tenant",
					},
				},
			},
		},
	}
	out, err := yaml.Marshal(creds)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(out), gc.Equals, `
credentials:
  openstack:
    default-credential: default-cred
    default-region: region-a
    peter:
      key: key
      secret: secret
      tenant-name: tenant
`[1:])
}

func (s *credentialsSuite) TestMarshallOpenstackUserPass(c *gc.C) {
	creds := cloud.Credentials{
		Credentials: map[string]cloud.CloudCredential{
			"openstack": {
				DefaultCredential: "default-cred",
				DefaultRegion:     "region-a",
				AuthCredentials: map[string]cloud.Credential{
					"peter": &cloud.OpenstackUserPassCredentials{
						UserPassCredentials: cloud.UserPassCredentials{
							User:     "user",
							Password: "secret",
						},
						Tenant: "tenant",
					},
				},
			},
		},
	}
	out, err := yaml.Marshal(creds)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(out), gc.Equals, `
credentials:
  openstack:
    default-credential: default-cred
    default-region: region-a
    peter:
      username: user
      password: secret
      tenant-name: tenant
`[1:])
}

func (s *credentialsSuite) TestMarshallAzureCredntials(c *gc.C) {
	creds := cloud.Credentials{
		Credentials: map[string]cloud.CloudCredential{
			"azure": {
				DefaultCredential: "default-cred",
				DefaultRegion:     "Central US",
				AuthCredentials: map[string]cloud.Credential{
					"peter": &cloud.AzureUserPassCredentials{
						ApplicationId:       "app-id",
						ApplicationPassword: "app-secret",
						SubscriptionId:      "subscription-id",
						TenantId:            "tenant-id",
					},
				},
			},
		},
	}
	out, err := yaml.Marshal(creds)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(out), gc.Equals, `
credentials:
  azure:
    default-credential: default-cred
    default-region: Central US
    peter:
      subscription-id: subscription-id
      tenant-id: tenant-id
      application-id: app-id
      application-password: app-secret
`[1:])
}

func (s *credentialsSuite) TestMarshallOAuth1(c *gc.C) {
	creds := cloud.Credentials{
		Credentials: map[string]cloud.CloudCredential{
			"maas": {
				DefaultCredential: "default-cred",
				DefaultRegion:     "region-default",
				AuthCredentials: map[string]cloud.Credential{
					"peter": &cloud.OAuth1Credentials{
						ConsumerKey:    "consumer-key",
						ConsumerSecret: "consumer-secret",
						AccessToken:    "access-token",
						TokenSecret:    "token-secret",
					},
				},
			},
		},
	}
	out, err := yaml.Marshal(creds)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(out), gc.Equals, `
credentials:
  maas:
    default-credential: default-cred
    default-region: region-default
    peter:
      consumer-key: consumer-key
      consumer-secret: consumer-secret
      access-token: access-token
      token-secret: token-secret
`[1:])
}

func (s *credentialsSuite) TestMarshallOAuth2(c *gc.C) {
	creds := cloud.Credentials{
		Credentials: map[string]cloud.CloudCredential{
			"google": {
				DefaultCredential: "default-cred",
				DefaultRegion:     "West US",
				AuthCredentials: map[string]cloud.Credential{
					"peter": &cloud.OAuth2Credentials{
						ClientId:    "client-id",
						ClientEmail: "client-email",
						PrivateKey:  "secret",
					},
				},
			},
		},
	}
	out, err := yaml.Marshal(creds)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(string(out), gc.Equals, `
credentials:
  google:
    default-credential: default-cred
    default-region: West US
    peter:
      client-id: client-id
      client-email: client-email
      private-key: secret
`[1:])
}

func (s *credentialsSuite) TestParseCredentials(c *gc.C) {
	s.testParseCredentials(c, []byte(`
credentials:
  aws:
    default-credential: peter
    default-region: us-east-2
    peter:
      auth-type: access-key
      key: key
      secret: secret
  aws-china:
    default-credential: zhu8jie
    zhu8jie:
      auth-type: access-key
      key: key
      secret: secret
    sun5kong:
      auth-type: access-key
      key: quay
      secret: sekrit
  aws-gov:
    default-region: us-gov-west-1
    supersekrit:
      auth-type: access-key
      key: super
      secret: sekrit
`[1:]), &cloud.Credentials{
		Credentials: map[string]cloud.CloudCredential{
			"aws": cloud.CloudCredential{
				DefaultCredential: "peter",
				DefaultRegion:     "us-east-2",
				AuthCredentials: map[string]cloud.Credential{
					"peter": &cloud.AccessKeyCredentials{
						Key:    "key",
						Secret: "secret",
					},
				},
			},
			"aws-china": cloud.CloudCredential{
				DefaultCredential: "zhu8jie",
				AuthCredentials: map[string]cloud.Credential{
					"zhu8jie": &cloud.AccessKeyCredentials{
						Key:    "key",
						Secret: "secret",
					},
					"sun5kong": &cloud.AccessKeyCredentials{
						Key:    "quay",
						Secret: "sekrit",
					},
				},
			},
			"aws-gov": cloud.CloudCredential{
				DefaultRegion: "us-gov-west-1",
				AuthCredentials: map[string]cloud.Credential{
					"supersekrit": &cloud.AccessKeyCredentials{
						Key:    "super",
						Secret: "sekrit",
					},
				},
			},
		},
	})
}

func (s *credentialsSuite) testParseCredentials(c *gc.C, input []byte, expect *cloud.Credentials) {
	output, err := cloud.ParseCredentials(input)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(output, jc.DeepEquals, expect)
}

func (s *credentialsSuite) TestParseCredentialsMissingAuthType(c *gc.C) {
	s.testParseCredentialsError(c, []byte(`
credentials:
  cloud-name:
    credential-name:
      doesnt: really-matter
`[1:]), "credentials.cloud-name.credential-name: missing auth-type")
}

func (s *credentialsSuite) TestParseCredentialsUnknownAuthType(c *gc.C) {
	s.testParseCredentialsError(c, []byte(`
credentials:
  cloud-name:
    credential-name:
      auth-type: woop
`[1:]), `credentials\.cloud-name\.credential-name: woop auth-type not supported`)
}

func (s *credentialsSuite) TestParseCredentialsNonStringValue(c *gc.C) {
	s.testParseCredentialsError(c, []byte(`
credentials:
  cloud-name:
    credential-name:
      non-string-value: 123
`[1:]), `credentials\.cloud-name\.credential-name\.non-string-value: expected string, got int\(123\)`)
}

func (s *credentialsSuite) testParseCredentialsError(c *gc.C, input []byte, expect string) {
	_, err := cloud.ParseCredentials(input)
	c.Assert(err, gc.ErrorMatches, expect)
}
