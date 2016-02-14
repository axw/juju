// Copyright 2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package user

import (
	"fmt"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/names"
	"github.com/juju/utils"
	"github.com/juju/utils/readpass"
	"launchpad.net/gnuflag"

	"github.com/juju/juju/cmd/juju/block"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/environs/configstore"
)

// randomPasswordNotify is called when a random password is generated.
var randomPasswordNotify = func(string) {}

const userChangePasswordDoc = `
Change the password for the user you are currently logged in as,
or as an admin, change the password for another user.

Examples:
  # You will be prompted to enter a password.
  juju change-user-password

  # Change the password to a random strong password.
  juju change-user-password --generate

  # Change the password for bob, this always uses a random password
  juju change-user-password bob

`

func NewChangePasswordCommand() cmd.Command {
	return modelcmd.WrapController(&changePasswordCommand{})
}

// changePasswordCommand changes the password for a user.
type changePasswordCommand struct {
	modelcmd.ControllerCommandBase
	api      ChangePasswordAPI
	writer   EnvironInfoCredsWriter
	Generate bool
	User     string
}

// Info implements Command.Info.
func (c *changePasswordCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "change-user-password",
		Args:    "[username]",
		Purpose: "changes the password for a user",
		Doc:     userChangePasswordDoc,
	}
}

// SetFlags implements Command.SetFlags.
func (c *changePasswordCommand) SetFlags(f *gnuflag.FlagSet) {
	f.BoolVar(&c.Generate, "generate", false, "generate a new strong password")
}

// Init implements Command.Init.
func (c *changePasswordCommand) Init(args []string) error {
	var err error
	c.User, err = cmd.ZeroOrOneArgs(args)
	if c.User != "" {
		// TODO(axw) too magical. change or error if Generate is not
		// specified
		c.Generate = true
	}
	return err
}

// ChangePasswordAPI defines the usermanager API methods that the change
// password command uses.
type ChangePasswordAPI interface {
	SetPassword(username, password string) error
	Close() error
}

// EnvironInfoCredsWriter defines methods of the configstore API info that
// are used to change the password.
type EnvironInfoCredsWriter interface {
	Write() error
	APICredentials() configstore.APICredentials
	SetAPICredentials(creds configstore.APICredentials)
}

// Run implements Command.Run.
func (c *changePasswordCommand) Run(ctx *cmd.Context) error {
	if c.api == nil {
		api, err := c.NewUserManagerAPIClient()
		if err != nil {
			return errors.Trace(err)
		}
		c.api = api
		defer c.api.Close()
	}

	newPassword, err := c.generateOrReadPassword(ctx, c.Generate)
	if err != nil {
		return errors.Trace(err)
	}

	var accountName string
	controllerName := c.ControllerName()
	store := c.ClientStore()
	if c.User != "" {
		accountName = names.NewUserTag(accountName).Canonical()
	} else {
		accountName, err = store.CurrentAccount(controllerName)
		if err != nil {
			return errors.Trace(err)
		}
	}
	accountDetails, err := store.AccountByName(controllerName, accountName)
	if err != nil {
		return errors.Trace(err)
	}

	oldPassword := accountDetails.Password
	accountDetails.Password = newPassword
	if err = c.api.SetPassword(accountDetails.User, newPassword); err != nil {
		return block.ProcessBlockedError(err, block.BlockChange)
	}

	if err := store.UpdateAccount(controllerName, accountName, *accountDetails); err != nil {
		logger.Errorf("updating the cached credentials failed, reverting to original password")
		if setErr := c.api.SetPassword(accountDetails.User, oldPassword); setErr != nil {
			logger.Errorf(
				"failed to set password back, you will need to edit your "+
					"accounts file by hand to specify the password: %q",
				newPassword,
			)
			return errors.Annotate(setErr, "failed to set password back")
		}
		return errors.Annotate(err, "failed to record password change for client")
	}
	ctx.Infof("Your password has been updated.")
	return nil
}

var readPassword = readpass.ReadPassword

func (*changePasswordCommand) generateOrReadPassword(ctx *cmd.Context, generate bool) (string, error) {
	if generate {
		password, err := utils.RandomPassword()
		if err != nil {
			return "", errors.Annotate(err, "failed to generate random password")
		}
		randomPasswordNotify(password)
		return password, nil
	}

	// Don't add the carriage returns before readPassword, but add
	// them directly after the readPassword so any errors are output
	// on their own lines.
	fmt.Fprint(ctx.Stdout, "password: ")
	password, err := readPassword()
	fmt.Fprint(ctx.Stdout, "\n")
	if err != nil {
		return "", errors.Trace(err)
	}
	fmt.Fprint(ctx.Stdout, "type password again: ")
	verify, err := readPassword()
	fmt.Fprint(ctx.Stdout, "\n")
	if err != nil {
		return "", errors.Trace(err)
	}
	if password != verify {
		return "", errors.New("Passwords do not match")
	}
	return password, nil
}
