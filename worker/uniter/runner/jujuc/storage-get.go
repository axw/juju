// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package jujuc

import (
	"github.com/juju/cmd"
	"github.com/juju/errors"
	"launchpad.net/gnuflag"
)

// StorageGetCommand implements the storage-get command.
type StorageGetCommand struct {
	cmd.CommandBase
	ctx               Context
	storageInstanceId string
	key               string
	out               cmd.Output
}

func NewStorageGetCommand(ctx Context) cmd.Command {
	return &StorageGetCommand{ctx: ctx}
}

func (c *StorageGetCommand) Info() *cmd.Info {
	doc := `
When no <key> is supplied, all keys values are printed.
`
	return &cmd.Info{
		Name:    "storage-get",
		Args:    "[<key>]",
		Purpose: "print information for storage instance with specified id",
		Doc:     doc,
	}
}

func (c *StorageGetCommand) SetFlags(f *gnuflag.FlagSet) {
	sV := newStorageIdValue(c.ctx, &c.storageInstanceId)
	c.out.AddFlags(f, "smart", cmd.DefaultFormatters)
	f.Var(sV, "s", "specify a storage instance by id")
}

func (c *StorageGetCommand) Init(args []string) error {
	if c.storageInstanceId == "" {
		return errors.New("no storage instance specified")
	}
	key, err := cmd.ZeroOrOneArgs(args)
	if err != nil {
		return err
	}
	c.key = key
	return nil
}

func (c *StorageGetCommand) Run(ctx *cmd.Context) error {
	storageInstance, ok := c.ctx.StorageInstance(c.storageInstanceId)
	if !ok {
		return nil
	}
	values := map[string]interface{}{
		"kind":     storageInstance.Kind.String(),
		"location": storageInstance.Location,
	}
	if c.key == "" {
		return c.out.Write(ctx, values)
	}
	if value, ok := values[c.key]; ok {
		return c.out.Write(ctx, value)
	}
	return errors.Errorf("invalid storage attribute %q", c.key)
}
