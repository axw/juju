// Copyright 2012-2014 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package user

import (
	"encoding/base64"
	"fmt"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/names"
	"github.com/juju/utils"
	"launchpad.net/gnuflag"

	"github.com/juju/juju/cmd/juju/block"
	"github.com/juju/juju/cmd/modelcmd"
)

const useraddCommandDoc = `
Add users to an existing model.

The user information is stored within an existing model, and will be
lost when the model is destroyed.  A server file will be written out in
the current directory.  You can control the name and location of this file
using the --output option.

Examples:
    # Add user "foobar" with a strong random password is generated.
    juju add-user foobar


See Also:
    juju help change-user-password
`

// AddUserAPI defines the usermanager API methods that the add command uses.
type AddUserAPI interface {
	AddUser(username, displayName, password string) (names.UserTag, []byte, error)
	Close() error
}

func NewAddCommand() cmd.Command {
	return modelcmd.WrapController(&addCommand{})
}

// addCommand adds new users into a Juju Server.
type addCommand struct {
	modelcmd.ControllerCommandBase
	api         AddUserAPI
	User        string
	DisplayName string
	OutPath     string
}

// Info implements Command.Info.
func (c *addCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "add-user",
		Args:    "<username> [<display name>]",
		Purpose: "adds a user",
		Doc:     useraddCommandDoc,
	}
}

// SetFlags implements Command.SetFlags.
func (c *addCommand) SetFlags(f *gnuflag.FlagSet) {
	f.StringVar(&c.OutPath, "o", "", "specify the model file for new user")
	f.StringVar(&c.OutPath, "output", "", "")
}

// Init implements Command.Init.
func (c *addCommand) Init(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no username supplied")
	}
	c.User, args = args[0], args[1:]
	if len(args) > 0 {
		c.DisplayName, args = args[0], args[1:]
	}
	if c.OutPath == "" {
		c.OutPath = c.User + ".server"
	}
	return cmd.CheckEmpty(args)
}

// Run implements Command.Run.
func (c *addCommand) Run(ctx *cmd.Context) error {
	if c.api == nil {
		api, err := c.NewUserManagerAPIClient()
		if err != nil {
			return errors.Trace(err)
		}
		c.api = api
		defer c.api.Close()
	}

	// Add a user without a password. This will generate a temporary
	// secret key, which we'll print out for the user to supply to
	// "juju register".
	_, secretKey, err := c.api.AddUser(c.User, c.DisplayName, "")
	if err != nil {
		return block.ProcessBlockedError(err, block.BlockChange)
	}

	displayName := c.User
	if c.DisplayName != "" {
		displayName = fmt.Sprintf("%s (%s)", c.DisplayName, c.User)
	}

	// Generate the base64-encoded string for the user to pass to
	// "juju register".
	//info, err := c.ConnectionInfo()
	//if err != nil {
	//	return errors.Trace(err)
	//}
	//registrationData, err := json.Marshal(map[string]interface{}{
	//	"a": info.APIEndpoint().Addresses,
	//	"u": c.User,
	//	"k": secretKey,
	//})
	//if err != nil {
	//	return errors.Trace(err)
	//}
	registrationData := []byte(secretKey)

	ctx.Infof("User %q added", displayName)
	ctx.Infof("Please send this command to %v:", c.User)
	ctx.Infof("    juju register %s",
		utils.ShQuote(base64.StdEncoding.EncodeToString(registrationData)),
	)

	return nil
}
