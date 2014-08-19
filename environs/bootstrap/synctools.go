// Copyright 2012, 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package bootstrap

import (
	"fmt"

	"github.com/juju/errors"
	"github.com/juju/utils/set"

	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/config"
	envtools "github.com/juju/juju/environs/tools"
	"github.com/juju/juju/juju/arch"
	coretools "github.com/juju/juju/tools"
	"github.com/juju/juju/version"
)

const noToolsNoUploadMessage = `Juju cannot bootstrap because no tools are available for your environment.
You may want to use the 'tools-metadata-url' configuration setting to specify the tools location.
`

// Unless otherwise specified, we will upload tools for all lts series on bootstrap
// when --upload-tools is used.
// ToolsLtsSeries records the known lts series.
var ToolsLtsSeries = []string{"precise", "trusty"}

// SeriesToUpload returns the supplied series with duplicates removed if
// non-empty; otherwise it returns a default list of series we should
// probably upload, based on cfg.
func SeriesToUpload(cfg *config.Config, series []string) []string {
	unique := set.NewStrings(series...)
	if unique.IsEmpty() {
		unique.Add(version.Current.Series)
		for _, toolsSeries := range ToolsLtsSeries {
			unique.Add(toolsSeries)
		}
		if series, ok := cfg.DefaultSeries(); ok {
			unique.Add(series)
		}
	}
	return unique.SortedValues()
}

// validateUploadAllowed returns an error if an attempt to upload tools should
// not be allowed.
func validateUploadAllowed(env environs.Environ, toolsArch *string) error {
	// Now check that the architecture for which we are setting up an
	// environment matches that from which we are bootstrapping.
	hostArch := arch.HostArch()
	// We can't build tools for a different architecture if one is specified.
	if toolsArch != nil && *toolsArch != hostArch {
		return fmt.Errorf("cannot build tools for %q using a machine running on %q", *toolsArch, hostArch)
	}
	// If no architecture is specified, ensure the target provider supports instances matching our architecture.
	supportedArchitectures, err := env.SupportedArchitectures()
	if err != nil {
		return fmt.Errorf(
			"no packaged tools available and cannot determine environment's supported architectures: %v", err)
	}
	archSupported := false
	for _, arch := range supportedArchitectures {
		if hostArch == arch {
			archSupported = true
			break
		}
	}
	if !archSupported {
		envType := env.Config().Type()
		return errors.Errorf("environment %q of type %s does not support instances running on %q", env.Config().Name(), envType, hostArch)
	}
	return nil
}

// findAvailableTools returns a list of available tools,
// including tools that may be locally built and then
// uploaded. Tools that need to be built will have an
// empty URL.
func findAvailableTools(env environs.Environ, cons constraints.Value, upload bool) (coretools.List, error) {
	var availableTools coretools.List
	if upload {
		// We're forcing an upload; ensure we can do so,
		// and validate the series requested.
		if err := validateUploadAllowed(env, cons.Arch); err != nil {
			return nil, err
		}
	} else {
		// We're not forcing an upload, so look for tools
		// in the environment's simplestreams search paths.
		var vers *version.Number
		if agentVersion, ok := env.Config().AgentVersion(); ok {
			vers = &agentVersion
		}
		logger.Debugf("looking for bootstrap tools: version=%v", vers)
		params := envtools.BootstrapToolsParams{
			Version: vers,
			Arch:    cons.Arch,
		}
		toolsList, findToolsErr := envtools.FindBootstrapTools(env, params)
		if findToolsErr != nil && !errors.IsNotFound(findToolsErr) {
			return nil, findToolsErr
		}
		// Even if we're successful above, we continue on in case the
		// tools found do not include the local architecture.
		if version.Current.IsDev() && (vers == nil || version.Current.Number == *vers) {
			// (but only if we're running a dev build,
			// and it's the same as agent-version.)
			if validateUploadAllowed(env, cons.Arch) != nil {
				return toolsList, findToolsErr
			}
		} else {
			return toolsList, findToolsErr
		}
		availableTools = toolsList
	}

	var archSeries set.Strings
	for _, tools := range availableTools {
		archSeries.Add(tools.Version.Arch + tools.Version.Series)
	}
	for _, series := range version.SupportedSeries() {
		if os, err := version.GetOSFromSeries(series); err != nil || os != version.Ubuntu {
			continue
		}
		if archSeries.Contains(version.Current.Arch + series) {
			continue
		}
		binary := version.Current
		binary.Series = series
		availableTools = append(availableTools, &coretools.Tools{Version: binary})
	}
	return availableTools, nil
}
