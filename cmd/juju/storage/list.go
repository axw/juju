// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage

import (
	"github.com/juju/cmd"
	"launchpad.net/gnuflag"

	"github.com/juju/juju/apiserver/params"
)

const ListCommandDoc = `
List information about storage instances.

options:
-e, --environment (= "")
   juju environment to operate in
-o, --output (= "")
   specify an output
`

// ListCommand attempts to release storage instance.
type ListCommand struct {
	StorageCommandBase
	out cmd.Output
}

// Init implements Command.Init.
func (c *ListCommand) Init(args []string) (err error) {
	return cmd.CheckEmpty(args)
}

// Info implements Command.Info.
func (c *ListCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "list",
		Purpose: "lists storage instance",
		Doc:     ListCommandDoc,
	}
}

// SetFlags implements Command.SetFlags.
func (c *ListCommand) SetFlags(f *gnuflag.FlagSet) {
	c.StorageCommandBase.SetFlags(f)
	c.out.AddFlags(f, "tabular", map[string]cmd.Formatter{
		"yaml":    cmd.FormatYaml,
		"json":    cmd.FormatJson,
		"tabular": formatListTabular,
	})
}

// Run implements Command.Run.
func (c *ListCommand) Run(ctx *cmd.Context) (err error) {
	api, err := getStorageListAPI(c)
	if err != nil {
		return err
	}
	defer api.Close()

	result, err := api.List()
	if err != nil {
		return err
	}
	output, err := formatStorageInfo(result)
	if err != nil {
		return err
	}
	return c.out.Write(ctx, output)
}

var (
	getStorageListAPI = (*ListCommand).getStorageListAPI
)

// StorageAPI defines the API methods that the storage commands use.
type StorageListAPI interface {
	Close() error
	List() ([]params.StorageInstance, error)
}

func (c *ListCommand) getStorageListAPI() (StorageListAPI, error) {
	return c.NewStorageAPI()
}
