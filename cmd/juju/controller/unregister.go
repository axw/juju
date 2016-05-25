// Copyright 2016 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package controller

import (
	"fmt"

	"github.com/juju/cmd"
	"github.com/juju/errors"
	"launchpad.net/gnuflag"

	"github.com/juju/juju/cmd/modelcmd"
)

// NewUnregisterCommand returns a command to allow the user to unregister a
// controller.
func NewUnregisterCommand() cmd.Command {
	return modelcmd.WrapController(
		&unregisterCommand{},
		modelcmd.ControllerSkipFlags,
		modelcmd.ControllerSkipDefault,
	)
}

const unregisterPurpose = `Unregisters a controller from the client.`
const unregisterDoc = `
Unregisters the specified controller from the client without attempting
to destroy any cloud resources.

This command may be used to unregister controllers that are defunct,
or previously shared but no longer required. To destroy all of the
resources managed by a controller, use "juju destroy-controller".

Examples:

    juju unregister local.ctrl

See also:

    bootstrap
    destroy-controller
    register
`

type unregisterCommand struct {
	modelcmd.ControllerCommandBase
	assumeYes bool
}

func (c *unregisterCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "unregister",
		Args:    "<controller-name>",
		Purpose: unregisterPurpose,
		Doc:     unregisterDoc,
	}
}

func (c *unregisterCommand) SetFlags(f *gnuflag.FlagSet) {
	c.ControllerCommandBase.SetFlags(f)
	f.BoolVar(&c.assumeYes, "y", false, "Do not ask for confirmation")
	f.BoolVar(&c.assumeYes, "yes", false, "")
}

func (c *unregisterCommand) Init(args []string) error {
	switch len(args) {
	case 0:
		return errors.New("no controller specified")
	case 1:
		return c.SetControllerName(args[0])
	default:
		return cmd.CheckEmpty(args[1:])
	}
}

func (c *unregisterCommand) Run(ctx *cmd.Context) error {
	controllerName := c.ControllerName()
	if !c.assumeYes {
		if err := c.confirmUnregister(ctx, controllerName); err != nil {
			return errors.Trace(err)
		}
	}
	return c.ClientStore().RemoveController(controllerName)
}

func (c *unregisterCommand) confirmUnregister(ctx *cmd.Context, controllerName string) error {
	unregisterControllerMessage := `
WARNING! This command will unregister the %q controller.
If the controller is still running, this may render your
controller inaccessible.

Continue [y/N]?  `[1:]

	fmt.Fprintf(ctx.Stdout, unregisterControllerMessage, controllerName)
	confirmed, err := promptConfirmation(ctx)
	if err != nil {
		return errors.Annotate(err, "controller deregistration aborted")
	}
	if !confirmed {
		return errors.New("controller deregistration aborted")
	}
	return nil
}
