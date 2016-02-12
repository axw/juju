// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller

import (
	"os"
	"path"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"github.com/juju/utils"
	"launchpad.net/gnuflag"

	"github.com/juju/juju/api"
	"github.com/juju/juju/cmd/modelcmd"
)

// NewLoginCommand returns a command to allow the user to login to a controller.
func NewLoginCommand() cmd.Command {
	return modelcmd.WrapController(&loginCommand{})
}

// loginCommand logs in to a Juju controller and caches the connection
// information.
type loginCommand struct {
	modelcmd.ControllerCommandBase
	loginAPIOpen api.OpenFunc
	// TODO (thumper): when we support local cert definitions
	// allow the use to specify the user and server address.
	// user      string
	// address   string
	Server       cmd.FileVar
	Name         string
	KeepPassword bool
}

var loginDoc = `
login connects to a juju controller and caches the information that juju
needs to connect to the api server in the $(JUJU_DATA)/models directory.

In order to login to a controller, you need to have a user already created for you
in that controller. The way that this occurs is for an existing user on the controller
to create you as a user. This will generate a file that contains the
information needed to connect.

If you have been sent one of these server files, you can login by doing the
following:

    # if you have saved the server file as ~/erica.server
    juju login --server=~/erica.server test-controller

A new strong random password is generated to replace the password defined in
the server file. The 'test-controller' will also become the current controller that
the juju command will talk to by default.

If you have used the 'api-info' command to generate a copy of your current
credentials for a controller, you should use the --keep-password option as it will
mean that you will still be able to connect to the api server from the
computer where you ran api-info.

See Also:
    juju help list-models
    juju help use-model
    juju help create-model
    juju help add-user
    juju help switch
`

// Info implements Command.Info
func (c *loginCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name: "login",
		// TODO(thumper): support user and address options
		// Args: "<name> [<server address>[:<server port>]]"
		Args:    "<name>",
		Purpose: "login to a Juju Controller",
		Doc:     loginDoc,
	}
}

// SetFlags implements Command.SetFlags.
func (c *loginCommand) SetFlags(f *gnuflag.FlagSet) {
	f.Var(&c.Server, "server", "path to yaml-formatted server file")
	f.BoolVar(&c.KeepPassword, "keep-password", false, "do not generate a new random password")
}

// SetFlags implements Command.Init.
func (c *loginCommand) Init(args []string) error {
	if len(args) == 0 {
		return errors.New("no name specified")
	}
	c.Name, args = args[0], args[1:]
	return cmd.CheckEmpty(args)
}

// cookieFile returns the path to the cookie used to store authorization
// macaroons. The returned value can be overridden by setting the
// JUJU_COOKIEFILE environment variable.
func cookieFile() string {
	if file := os.Getenv("JUJU_COOKIEFILE"); file != "" {
		return file
	}
	return path.Join(utils.Home(), ".go-cookies")
}

// Run implements Command.Run
func (c *loginCommand) Run(ctx *cmd.Context) error {
	/*
		if c.loginAPIOpen == nil {
			c.loginAPIOpen = c.ControllerCommandBase.APIOpen
		}

		// TODO(thumper): as we support the user and address
		// change this check here.
		if c.Server.Path == "" {
			return errors.New("no server file specified")
		}

		serverYAML, err := c.Server.Read(ctx)
		if err != nil {
			return errors.Trace(err)
		}

		var serverDetails modelcmd.ServerFile
		if err := goyaml.Unmarshal(serverYAML, &serverDetails); err != nil {
			return errors.Trace(err)
		}

		info := api.Info{
			Addrs:  serverDetails.Addresses,
			CACert: serverDetails.CACert,
		}
		var userTag names.UserTag
		if serverDetails.Username != "" {
			// Construct the api.Info struct from the provided values
			// and attempt to connect to the remote server before we do anything else.
			if !names.IsValidUser(serverDetails.Username) {
				return errors.Errorf("%q is not a valid username", serverDetails.Username)
			}

			userTag = names.NewUserTag(serverDetails.Username)
			if !userTag.IsLocal() {
				// Remote users do not have their passwords stored in Juju
				// so we never attempt to change them.
				c.KeepPassword = true
			}
			info.Tag = userTag
		}

		if serverDetails.Password != "" {
			info.Password = serverDetails.Password
		}

		if serverDetails.Password == "" || serverDetails.Username == "" {
			info.UseMacaroons = true
		}
		if c == nil {
			panic("nil c")
		}
		if c.loginAPIOpen == nil {
			panic("no loginAPIOpen")
		}
		apiState, err := c.loginAPIOpen(&info, api.DefaultDialOpts())
		if err != nil {
			return errors.Trace(err)
		}
		defer apiState.Close()

		// If we get to here, the credentials supplied were sufficient to connect
		// to the Juju Controller and login. Now we cache the details.
		controllerInfo, err := c.cacheConnectionInfo(serverDetails, apiState)
		if err != nil {
			return errors.Trace(err)
		}
		ctx.Infof("cached connection details as controller %q", c.Name)

		// If we get to here, we have been able to connect to the API server, and
		// also have been able to write the cached information. Now we can change
		// the user's password to a new randomly generated strong password, and
		// update the cached information knowing that the likelihood of failure is
		// minimal.
		if !c.KeepPassword {
			if err := c.updatePassword(ctx, apiState, userTag, controllerInfo); err != nil {
				return errors.Trace(err)
			}
		}

		return errors.Trace(modelcmd.SetCurrentController(ctx, c.Name))
	*/
	return errors.NotImplementedf("login")
}
