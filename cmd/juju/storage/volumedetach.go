// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package storage

import (
	"errors"

	"github.com/juju/cmd"
	"github.com/juju/names"

	"github.com/juju/juju/apiserver/params"
)

const VolumeDetachCommandDoc = `
Detach volumes (disks) from machines in the environment.

options:
-e, --environment (= "")
    juju environment to operate in
-o, --output (= "")
    specify an output file
volume [volume ...]
  IDs of volumes to detach
`

// VolumeDetachCommand lists storage volumes.
type VolumeDetachCommand struct {
	VolumeCommandBase
	volumes []names.VolumeTag
	out     cmd.Output
}

// Init implements Command.Init.
func (c *VolumeDetachCommand) Init(args []string) (err error) {
	if len(args) == 0 {
		return errors.New("one or more volume IDs must be specified")
	}
	c.volumes = make([]names.VolumeTag, len(args))
	for i, arg := range args {
		c.volumes[i] = names.NewVolumeTag(arg)
	}
	return nil
}

// Info implements Command.Info.
func (c *VolumeDetachCommand) Info() *cmd.Info {
	return &cmd.Info{
		Name:    "detach",
		Purpose: "detach storage volumes",
		Doc:     VolumeDetachCommandDoc,
	}
}

// Run implements Command.Run.
func (c *VolumeDetachCommand) Run(ctx *cmd.Context) (err error) {
	api, err := getVolumeDetachAPI(c)
	if err != nil {
		return err
	}
	defer api.Close()

	ids := make([]params.MachineStorageId, len(c.volumes))
	for i, v := range c.volumes {
		ids[i] = params.MachineStorageId{
			MachineTag:    "", // omitted, detach from only machine
			AttachmentTag: v.String(),
		}
	}
	results, err := api.DetachMachineStorage(ids)
	if err != nil {
		return err
	}
	return results.Combine()
}

var getVolumeDetachAPI = (*VolumeDetachCommand).getVolumeDetachAPI

// VolumeDetachAPI defines the API methods that the volume list command use.
type VolumeDetachAPI interface {
	Close() error
	DetachMachineStorage([]params.MachineStorageId) (params.ErrorResults, error)
}

func (c *VolumeDetachCommand) getVolumeDetachAPI() (VolumeDetachAPI, error) {
	return c.NewStorageAPI()
}
