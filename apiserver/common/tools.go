// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package common

import (
	"fmt"
	"sort"

	"github.com/juju/errors"
	"github.com/juju/juju/apiserver/params"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	"github.com/juju/juju/state"
	coretools "github.com/juju/juju/tools"
	"github.com/juju/juju/version"
	"github.com/juju/names"
)

type ToolsSource interface {
	AllToolsMetadata() ([]state.ToolsMetadata, error)
}

type ToolsGetterInterface interface {
	state.EntityFinder
	ToolsSource
	EnvironConfig() (*config.Config, error)
}

// ToolsGetter implements a common Tools method for use by various
// facades.
type ToolsGetter struct {
	st         ToolsGetterInterface
	getCanRead GetAuthFunc
}

// NewToolsGetter returns a new ToolsGetter. The GetAuthFunc will be
// used on each invocation of Tools to determine current permissions.
func NewToolsGetter(st ToolsGetterInterface, getCanRead GetAuthFunc) *ToolsGetter {
	return &ToolsGetter{
		st:         st,
		getCanRead: getCanRead,
	}
}

// Tools finds the tools necessary for the given agents.
func (t *ToolsGetter) Tools(args params.Entities) (params.ToolsResults, error) {
	result := params.ToolsResults{
		Results: make([]params.ToolsResult, len(args.Entities)),
	}
	canRead, err := t.getCanRead()
	if err != nil {
		return result, err
	}
	agentVersion, cfg, err := t.getGlobalAgentVersion()
	if err != nil {
		return result, err
	}
	env, err := environs.New(cfg)
	if err != nil {
		return result, errors.Trace(err)
	}
	for i, entity := range args.Entities {
		tag, err := names.ParseTag(entity.Tag)
		if err != nil {
			result.Results[i].Error = ServerError(ErrPerm)
			continue
		}
		agentTools, err := t.oneAgentTools(canRead, tag, agentVersion, env)
		if err == nil {
			result.Results[i].Tools = agentTools
		}
		result.Results[i].Error = ServerError(err)
	}
	return result, nil
}

func (t *ToolsGetter) getGlobalAgentVersion() (version.Number, *config.Config, error) {
	// Get the Agent Version requested in the Environment Config
	nothing := version.Number{}
	cfg, err := t.st.EnvironConfig()
	if err != nil {
		return nothing, nil, err
	}
	agentVersion, ok := cfg.AgentVersion()
	if !ok {
		return nothing, nil, fmt.Errorf("agent version not set in environment config")
	}
	return agentVersion, cfg, nil
}

func (t *ToolsGetter) oneAgentTools(canRead AuthFunc, tag names.Tag, agentVersion version.Number, env environs.Environ) (*coretools.Tools, error) {
	if !canRead(tag) {
		return nil, ErrPerm
	}
	entity, err := t.st.FindEntity(tag)
	if err != nil {
		return nil, err
	}
	tooler, ok := entity.(state.AgentTooler)
	if !ok {
		return nil, NotSupportedError(tag, "agent tools")
	}
	existingTools, err := tooler.AgentTools()
	if err != nil {
		return nil, err
	}
	allMetadata, err := t.st.AllToolsMetadata()
	if err != nil {
		return nil, err
	}
	list, err := findMatchingTools(allMetadata, params.FindToolsParams{
		Number:       agentVersion,
		MajorVersion: -1,
		MinorVersion: -1,
		Series:       existingTools.Version.Series,
		Arch:         existingTools.Version.Arch,
	})
	if err == coretools.ErrNoMatches {
		err = errors.NewNotFound(err, "tools not found")
	}
	if err != nil {
		return nil, err
	}
	return list[0], nil
}

// ToolsSetter implements a common Tools method for use by various
// facades.
type ToolsSetter struct {
	st          state.EntityFinder
	getCanWrite GetAuthFunc
}

// NewToolsSetter returns a new ToolsGetter. The GetAuthFunc will be
// used on each invocation of Tools to determine current permissions.
func NewToolsSetter(st state.EntityFinder, getCanWrite GetAuthFunc) *ToolsSetter {
	return &ToolsSetter{
		st:          st,
		getCanWrite: getCanWrite,
	}
}

// SetTools updates the recorded tools version for the agents.
func (t *ToolsSetter) SetTools(args params.EntitiesVersion) (params.ErrorResults, error) {
	results := params.ErrorResults{
		Results: make([]params.ErrorResult, len(args.AgentTools)),
	}
	canWrite, err := t.getCanWrite()
	if err != nil {
		return results, errors.Trace(err)
	}
	for i, agentTools := range args.AgentTools {
		tag, err := names.ParseTag(agentTools.Tag)
		if err != nil {
			results.Results[i].Error = ServerError(ErrPerm)
			continue
		}
		err = t.setOneAgentVersion(tag, agentTools.Tools.Version, canWrite)
		results.Results[i].Error = ServerError(err)
	}
	return results, nil
}

func (t *ToolsSetter) setOneAgentVersion(tag names.Tag, vers version.Binary, canWrite AuthFunc) error {
	if !canWrite(tag) {
		return ErrPerm
	}
	entity0, err := t.st.FindEntity(tag)
	if err != nil {
		return err
	}
	entity, ok := entity0.(state.AgentTooler)
	if !ok {
		return NotSupportedError(tag, "agent tools")
	}
	return entity.SetAgentVersion(vers)
}

// FindTools returns a List containing all tools matching the given parameters.
func FindTools(t ToolsSource, args params.FindToolsParams) (params.FindToolsResult, error) {
	var result params.FindToolsResult
	allMetadata, err := t.AllToolsMetadata()
	if err != nil {
		return result, err
	}
	list, err := findMatchingTools(allMetadata, args)
	if err == coretools.ErrNoMatches {
		err = errors.NewNotFound(err, "tools not found")
	}
	result.List = list
	result.Error = ServerError(err)
	return result, nil
}

// ToolsURL returns a URL for the apiserver-provided tools
// that can be used to distinguish them from tools obtained
// provided via other sources.
func ToolsURL(v version.Binary) string {
	return fmt.Sprintf("apiserver://tools/%s", v)
}

func findMatchingTools(metadata []state.ToolsMetadata, args params.FindToolsParams) (coretools.List, error) {
	list := make(coretools.List, len(metadata))
	for i, m := range metadata {
		tools := &coretools.Tools{
			Version: m.Version,
			Size:    m.Size,
			SHA256:  m.SHA256,
			URL:     ToolsURL(m.Version),
		}
		list[i] = tools
	}
	sort.Sort(list)
	filter := coretools.Filter{
		Number: args.Number,
		Arch:   args.Arch,
		Series: args.Series,
	}
	list, err := list.Match(filter)
	if err != nil {
		return nil, err
	}
	var matching coretools.List
	for _, tools := range list {
		if args.MajorVersion > 0 && tools.Version.Major != args.MajorVersion {
			continue
		}
		if args.MinorVersion != -1 && tools.Version.Minor != args.MinorVersion {
			continue
		}
		matching = append(matching, tools)
	}
	if len(matching) == 0 {
		return nil, coretools.ErrNoMatches
	}
	return matching, nil
}
