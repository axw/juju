// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloud

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"launchpad.net/gnuflag"

	jujucloud "github.com/juju/juju/cloud"
)

type listCredentialsCommand struct {
	cmd.CommandBase
	out cmd.Output
}

var listCredentialsDoc = `
The list-credentials command lists the credentials for clouds on which Juju workloads
can be deployed. The credentials listed are those added with the add-credentials
command.

Example:
   # List all credentials.
   juju list-credentials

   # List credentials for the aws cloud only.
   juju list-credentials aws
`

// NewListCredentialsCommand returns a command to list cloud credentials.
func NewListCredentialsCommand() cmd.Command {
	return &listCredentialsCommand{}
}

func (c *listCredentialsCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "list-credentials",
		Purpose: "list credentials available to bootstrap Juju",
		Doc:     listCredentialsDoc,
	}
}

func (c *listCredentialsCommand) SetFlags(f *gnuflag.FlagSet) {
	c.out.AddFlags(f, "tabular", map[string]cmd.Formatter{
		"yaml":    cmd.FormatYaml,
		"json":    cmd.FormatJson,
		"tabular": formatCredentialsTabular,
	})
}

func (c *listCredentialsCommand) Run(ctxt *cmd.Context) error {
	var credentials *jujucloud.Credentials
	data, err := ioutil.ReadFile(jujucloud.JujuCredentials())
	if err == nil {
		credentials, err = jujucloud.ParseCredentials(data)
		if err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return c.out.Write(ctxt, credentials.Credentials)
}

// formatCredentialsTabular returns a tabular summary of cloud information.
func formatCredentialsTabular(value interface{}) ([]byte, error) {
	credentials, ok := value.(map[string]jujucloud.CloudCredential)
	if !ok {
		return nil, errors.Errorf("expected value of type %T, got %T", credentials, value)
	}

	// For tabular we'll sort alphabetically by cloud, and then by credential name.
	var cloudNames []string
	for name := range credentials {
		cloudNames = append(cloudNames, name)
	}
	sort.Strings(cloudNames)

	var out bytes.Buffer
	const (
		// To format things into columns.
		minwidth = 0
		tabwidth = 1
		padding  = 2
		padchar  = ' '
		flags    = 0
	)
	tw := tabwriter.NewWriter(&out, minwidth, tabwidth, padding, padchar, flags)
	p := func(values ...string) {
		text := strings.Join(values, "\t")
		fmt.Fprintln(tw, text)
	}
	p("CLOUD\tNAME\tTYPE\tATTRS")
	for _, cloudName := range cloudNames {
		credentials := credentials[cloudName]
		var credentialNames []string
		for name := range credentials.AuthCredentials {
			credentialNames = append(credentialNames, name)
		}
		sort.Strings(credentialNames)

		for _, credentialName := range credentialNames {
			credential := credentials.AuthCredentials[credentialName]
			if credentialName == credentials.DefaultCredential {
				credentialName += "*"
			}

			attrs := credential.Attributes()
			var attrNames []string
			for attrName := range attrs {
				attrNames = append(attrNames, attrName)
			}
			sort.Strings(attrNames)

			var kv []string
			for _, attrName := range attrNames {
				kv = append(kv, attrName+"="+attrs[attrName])
			}

			p(cloudName, credentialName, string(credential.AuthType()), strings.Join(kv, " "))
		}
	}
	tw.Flush()

	return out.Bytes(), nil
}
