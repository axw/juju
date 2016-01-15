// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloud

import (
	"fmt"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"gopkg.in/juju/environschema.v1"
	"gopkg.in/juju/environschema.v1/form"

	jujucloud "github.com/juju/juju/cloud"
	"github.com/juju/juju/environs"
)

type addCredentialCommand struct {
	cmd.CommandBase
	out cmd.Output

	CloudName string
}

var addCredentialDoc = `
The add-credential command adds a new credential for the specified cloud.

Example:
   juju add-credential aws
`

// NewAddCredentialCommand returns a command to list cloud information.
func NewAddCredentialCommand() cmd.Command {
	return &addCredentialCommand{}
}

func (c *addCredentialCommand) Init(args []string) error {
	if len(args) == 0 {
		return errors.New("no cloud specified")
	}
	c.CloudName = args[0]
	return cmd.CheckEmpty(args[1:])
}

func (c *addCredentialCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "add-credential",
		Args:    "<cloudname>",
		Purpose: "add a credential for the specified cloud",
		Doc:     addCredentialDoc,
	}
}

func (c *addCredentialCommand) Run(ctxt *cmd.Context) error {
	publicClouds, _, err := jujucloud.PublicCloudMetadata(jujucloud.JujuPublicClouds())
	if err != nil {
		return err
	}
	cloud, ok := publicClouds.Clouds[c.CloudName]
	if !ok {
		return errors.NotFoundf("cloud %q", c.CloudName)
	}
	provider, err := environs.Provider(cloud.Type)
	if err != nil {
		return errors.Trace(err)
	}
	schemas := provider.CredentialSchemas()

	authTypes := make([]interface{}, 0, len(schemas))
	for authType := range schemas {
		authTypes = append(authTypes, string(authType))
	}

	baseFields := environschema.Fields{
		"credential-name": {
			Description: "credential name",
			Type:        environschema.Tstring,
			Group:       environschema.AccountGroup,
			Immutable:   true,
			Mandatory:   true,
		},
		"auth-type": {
			Description: "authentication type",
			Type:        environschema.Tstring,
			Group:       environschema.AccountGroup,
			Immutable:   true,
			Mandatory:   true,
			Secret:      true,
			Values:      authTypes,
		},
	}

	filler := form.IOFiller{
		In:               ctxt.Stdin,
		Out:              ctxt.Stdout,
		ShowDescriptions: true,
	}
	attrs, err := filler.Fill(form.Form{
		Title:  fmt.Sprintf("credential for %q", c.CloudName),
		Fields: baseFields,
	})
	if err != nil {
		return errors.Trace(err)
	}
	ctxt.Infof("credential: %v", attrs)
	return nil
}
