// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package commands

import (
	"os"
	"strings"

	"github.com/juju/cmd"
	"github.com/juju/errors"

	"github.com/juju/juju/api/modelmanager"
	"github.com/juju/juju/cmd/modelcmd"
	"github.com/juju/juju/juju/osenv"
	"github.com/juju/juju/jujuclient"
)

func newSwitchCommand() cmd.Command {
	return modelcmd.WrapBase(&switchCommand{
		Store: jujuclient.NewFileClientStore(),
	})
}

type switchCommand struct {
	modelcmd.JujuCommandBase
	Store  jujuclient.ClientStore
	Target string
}

var switchDoc = `
Switch to the specified model, or controller.

If the name identifies controller, the client will switch to the
active model for that controller. Otherwise, the name must specify
either the name of a model within the active controller, or a
fully-qualified model with the format "controller:model".
`

func (c *switchCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "switch",
		Args:    "<controller>|<model>|<controller>:<model>",
		Purpose: "change the active Juju model",
		Doc:     switchDoc,
	}
}

func (c *switchCommand) Init(args []string) error {
	if len(args) == 0 {
		return errors.Errorf("missing controller or model name")
	}
	if err := cmd.CheckEmpty(args[1:]); err != nil {
		return err
	}
	c.Target = args[0]
	return nil
}

func (c *switchCommand) Run(ctx *cmd.Context) error {
	// Switch is an alternative way of dealing with environments than using
	// the JUJU_MODEL environment setting, and as such, doesn't play too well.
	// If JUJU_MODEL is set we should report that as the current environment,
	// and not allow switching when it is set.
	if env := os.Getenv(osenv.JujuModelEnvKey); env != "" {
		return errors.Errorf("cannot switch when JUJU_MODEL is overriding the model (set to %q)", env)
	}

	// If the name identifies a controller, then set that as the current one.
	if _, err := c.Store.ControllerByName(c.Target); err == nil {
		return modelcmd.SetCurrentController(ctx, c.Target)
	} else if !errors.IsNotFound(err) {
		return errors.Trace(err)
	}

	// The target is not a controller, so check for a model with
	// the given name. The name can be qualified with the controller
	// name (<controller>:<model>), or unqualified; in the latter
	// case, the model must exist in the current controller.
	currentControllerName, err := modelcmd.ReadCurrentController()
	if err != nil {
		return errors.Trace(err)
	}
	var controllerName, modelName string
	if i := strings.IndexRune(c.Target, ':'); i > 0 {
		controllerName, modelName = c.Target[:i], c.Target[i+1:]
	} else {
		controllerName = currentControllerName
		modelName = c.Target
	}

	err = c.Store.SetCurrentModel(controllerName, modelName)
	if errors.IsNotFound(err) {
		// The model isn't known locally, so we must query the controller.
		if err := c.refreshModels(ctx, controllerName); err != nil {
			return errors.Annotate(err, "refreshing models cache")
		}
		if err := c.Store.SetCurrentModel(controllerName, modelName); err != nil {
			return errors.Trace(err)
		}
	} else if err != nil {
		return errors.Trace(err)
	}
	if currentControllerName != controllerName {
		if err := modelcmd.SetCurrentController(ctx, controllerName); err != nil {
			return errors.Trace(err)
		}
	}
	// TODO(axw) log transition here, rather than in modelcmd.SetCurrent...
	return nil
}

func (c *switchCommand) refreshModels(ctx *cmd.Context, controllerName string) error {
	// TODO(axw) need to get the user name from accounts.yaml.
	userName := "admin"

	ctx.Verbosef("listing models for %q on %q", userName, controllerName)
	conn, err := c.NewAPIRoot(c.Store, controllerName, "")
	if err != nil {
		return errors.Trace(err)
	}
	defer conn.Close()
	modelManager := modelmanager.NewClient(conn)
	models, err := modelManager.ListModels(userName)
	if err != nil {
		return errors.Trace(err)
	}

	// Cache model information locally.
	for _, model := range models {
		err := c.Store.UpdateModel(controllerName, model.Name, jujuclient.ModelDetails{
			model.UUID,
		})
		if err != nil {
			return errors.Trace(err)
		}
	}
	return nil
}
