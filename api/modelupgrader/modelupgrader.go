// Copyright 2017 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package modelupgrader

import (
	"github.com/juju/errors"
	"github.com/juju/loggo"
	"github.com/juju/version"
	"gopkg.in/juju/names.v2"

	"github.com/juju/juju/api/base"
	"github.com/juju/juju/apiserver/params"
)

var logger = loggo.GetLogger("juju.api.modelupgrader")

// Client provides methods that the Juju client command uses to interact
// with models stored in the Juju Server.
type Client struct {
	facade base.FacadeCaller
}

// NewClient creates a new `Client` based on an existing authenticated API
// connection.
func NewClient(caller base.APICaller) *Client {
	return &Client{base.NewFacadeCaller(caller, "ModelUpgrader")}
}

// ModelEnvironVersion returns the current version of the environ corresponding
// to the specified model.
func (c *Client) ModelEnvironVersion(tag names.ModelTag) (version.Number, error) {
	args := params.Entities{
		Entities: []params.Entity{{Tag: tag.String()}},
	}
	var results params.VersionResults
	err := c.facade.FacadeCall("ModelEnvironVersion", &args, &results)
	if err != nil {
		return version.Zero, errors.Trace(err)
	}
	if len(results.Results) != 1 {
		return version.Zero, errors.Errorf("expected 1 result, got %d", len(results.Results))
	}
	if err := results.Results[0].Error; err != nil {
		return version.Zero, err
	}
	if results.Results[0].Version == nil {
		return version.Zero, errors.New("nil version returned")
	}
	return *results.Results[0].Version, nil
}

// SetModelEnvironVersion sets the current version of the environ corresponding
// to the specified model.
func (c *Client) SetModelEnvironVersion(tag names.ModelTag, v version.Number) error {
	args := params.EntityVersionNumbers{
		Entities: []params.EntityVersionNumber{{
			Tag:     tag.String(),
			Version: v.String(),
		}},
	}
	var results params.ErrorResults
	err := c.facade.FacadeCall("SetModelEnvironVersion", &args, &results)
	if err != nil {
		return errors.Trace(err)
	}
	return results.OneError()
}
