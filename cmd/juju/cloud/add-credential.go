// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package cloud

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/errgo.v1"

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

func (c *addCredentialCommand) Run(ctx *cmd.Context) error {
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

	// Prompt for the credential name.
	credentialName, err := c.prompt(ctx, "credential name", false, nonEmptyValidator)
	if err != nil {
		return errors.Trace(err)
	}

	// Prompt for the auth-type. If there is only one possibility, just
	// automatically choose that without prompting.
	if len(cloud.AuthTypes) == 0 {
		return errors.Errorf("cloud %q does not specify any auth-types", c.CloudName)
	}
	authTypeStrings := make([]string, len(cloud.AuthTypes))
	for i, authType := range cloud.AuthTypes {
		authTypeStrings[i] = string(authType)
	}
	authTypesPrompt := fmt.Sprintf(
		"select auth type [%s]", strings.Join(authTypeStrings, ", "),
	)
	var authType string
	if len(authTypeStrings) == 1 {
		authType = string(cloud.AuthTypes[0])
		fmt.Fprintf(
			ctx.Stdout, "%s: (automatically selected %q)\n",
			authTypesPrompt, authType,
		)
	} else {
		authTypeValidator := newOneOfValidator(authTypeStrings)
		authType, err = c.prompt(ctx, authTypesPrompt, false, authTypeValidator)
		if err != nil {
			return errors.Trace(err)
		}
	}

	schema, ok := provider.CredentialSchemas()[jujucloud.AuthType(authType)]
	if !ok {
		return errors.NotFoundf("schema for auth-type %q", authType)
	}
	attrNames := make([]string, 0, len(schema))
	for attrName := range schema {
		attrNames = append(attrNames, attrName)
	}
	sort.Strings(attrNames)

	// Prompt for auth-type specific credentials.
	attrs := make(map[string]string)
	for _, attrName := range attrNames {
		attr := schema[attrName]
		attrValue, err := c.prompt(ctx, "enter "+attrName, attr.Secret, nonEmptyValidator)
		if err != nil {
			return errors.Trace(err)
		}
		attrs[attrName] = attrValue
	}
	if err := schema.Validate(attrs); err != nil {
		return errors.Trace(err)
	}

	fmt.Println(credentialName, authType, attrs)

	return nil
}

func (c *addCredentialCommand) prompt(
	ctx *cmd.Context,
	promptString string,
	secret bool,
	validate func(string) error,
) (string, error) {
	fmt.Fprintf(ctx.Stdout, "%s: ", promptString)
	line, err := c.readLine(ctx.Stdout, ctx.Stdin, secret)
	if err != nil {
		return "", errors.Trace(err)
	}
	if err := validate(line); err != nil {
		return "", errors.Trace(err)
	}
	return line, nil
}

// readLine reads a line from the given reader. If the reader is a terminal
// and the value is a secret, it will be read without echoing.
//
// This was cribbed from gopkg.in/juju/environschema.v1/form.
func (c *addCredentialCommand) readLine(w io.Writer, r io.Reader, secret bool) (string, error) {
	if f, ok := r.(*os.File); ok && secret && terminal.IsTerminal(int(f.Fd())) {
		defer w.Write([]byte{'\n'})
		line, err := terminal.ReadPassword(int(f.Fd()))
		return string(line), err
	}
	var input []byte
	for {
		var buf [1]byte
		n, err := r.Read(buf[:])
		if n == 1 {
			if buf[0] == '\n' {
				break
			}
			input = append(input, buf[0])
		}
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return "", errgo.Mask(err)
		}
	}
	return strings.TrimRight(string(input), "\r"), nil
}

func chainValidators(vs ...func(string) error) func(string) error {
	return func(s string) error {
		for _, v := range vs {
			if err := v(s); err != nil {
				return err
			}
		}
		return nil
	}
}

func nonEmptyValidator(s string) error {
	if s == "" {
		return errors.New("you must specify a non-empty value")
	}
	return nil
}

func newOneOfValidator(valid []string) func(string) error {
	return func(s string) error {
		for _, valid := range valid {
			if s == valid {
				return nil
			}
		}
		return errors.NewNotValid(nil, fmt.Sprintf(
			"invalid value %q, expected one of [%s]",
			s, strings.Join(valid, ", "),
		))
	}
}
