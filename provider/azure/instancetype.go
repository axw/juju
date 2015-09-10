// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/juju/errors"

	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/imagemetadata"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/environs/simplestreams"
	"github.com/juju/juju/juju/arch"
)

const defaultMem = 1024 // 1GiB

// As long as this code only supports the default simplestreams
// database, which is always signed, there is no point in accepting
// unsigned metadata.
const signedImageDataOnly = true

// findMatchingImages queries simplestreams for OS images that match the given
// requirements.
//
// If it finds no matching images, that's an error.
func findMatchingImages(env environs.Environ, location, series string, arches []string) ([]*imagemetadata.ImageMetadata, error) {
	endpoint := getEndpoint(location)
	constraint := imagemetadata.NewImageConstraint(simplestreams.LookupParams{
		CloudSpec: simplestreams.CloudSpec{location, endpoint},
		Series:    []string{series},
		Arches:    arches,
		Stream:    env.Config().ImageStream(),
	})
	sources, err := environs.ImageMetadataSources(env)
	if err != nil {
		return nil, err
	}
	images, _, err := imagemetadata.Fetch(sources, constraint, signedImageDataOnly)
	if len(images) == 0 || errors.IsNotFound(err) {
		return nil, fmt.Errorf("no OS images found for location %q, series %q, architectures %q (and endpoint: %q)", location, series, arches, endpoint)
	} else if err != nil {
		return nil, err
	}
	return images, nil
}

// getEndpoint returns the simplestreams endpoint to use for the given Azure
// location (e.g. West Europe or China North).
func getEndpoint(location string) string {
	if strings.HasPrefix(location, "China") {
		return "https://management.core.chinacloudapi.cn/"
	}
	return "https://management.core.windows.net/"
}

// newInstanceType creates an InstanceType based on a VirtualMachineSize.
func newInstanceType(size compute.VirtualMachineSize) instances.InstanceType {
	// We're not doing real costs for now; just made-up, relative
	// costs, to ensure we choose the right VMs given matching
	// constraints. This was based on the pricing for West US,
	// and assumes that all regions have the same relative costs.
	//
	// DS is the same price as D, but is targeted at Premium Storage.
	// Likewise for GS and G. We put the premium storage variants
	// directly after their non-premium counterparts.
	machineSizeCost := []string{
		"Standard_A0",
		"Standard_A1",
		"Standard_D1",
		"Standard_DS1",
		"Standard_A2",
		"Standard_D2",
		"Standard_DS2",
		"Standard_D11",
		"Standard_DS11",
		"Standard_A3",
		"Standard_D3",
		"Standard_DS3",
		"Standard_D12",
		"Standard_DS12",
		"Standard_A5", // Yes, A5 is cheaper than A4.
		"Standard_A4",
		"Standard_A6",
		"Standard_G1",
		"Standard_GS1",
		"Standard_D4",
		"Standard_DS4",
		"Standard_D13",
		"Standard_DS13",
		"Standard_A7",
		"Standard_A10",
		"Standard_G2",
		"Standard_GS2",
		"Standard_D14",
		"Standard_DS14",
		"Standard_A8",
		"Standard_A11",
		"Standard_G3",
		"Standard_GS3",
		"Standard_A9",
		"Standard_G4",
		"Standard_GS4",
		"Standard_GS5",
		"Standard_G5",

		// Basic instances are less capable than standard
		// ones, so we don't want to be providing them as
		// a default. This is achieved by costing them at
		// a higher price, even though they are cheaper
		// in reality.
		"Basic_A0",
		"Basic_A1",
		"Basic_A2",
		"Basic_A3",
		"Basic_A4",
	}

	// Anything not in the list is more expensive that is in the list.
	cost := len(machineSizeCost)
	for i, name := range machineSizeCost {
		if size.Name == name {
			cost = i
			break
		}
	}
	if cost == len(machineSizeCost) {
		logger.Warningf("found unknown VM size %q", size.Name)
	}

	vtype := "Hyper-V"
	return instances.InstanceType{
		Id:       size.Name,
		Name:     size.Name,
		Arches:   []string{arch.AMD64},
		CpuCores: uint64(size.NumberOfCores),
		Mem:      uint64(size.MemoryInMB),
		RootDisk: uint64(size.OsDiskSizeInMB),
		Cost:     uint64(cost),
		VirtType: &vtype,
		// tags are not currently supported by azure
	}
}

// isLimitedRoleSize reports whether the named role size is limited to some
// physical hosts only.
func isLimitedRoleSize(name string) bool {
	switch name {
	case "ExtraSmall", "Small", "Medium", "Large", "ExtraLarge":
		// At the time of writing, only the original role sizes are not limited.
		return false
	case "A5", "A6", "A7", "A8", "A9":
		// We never used to filter out A5-A9 role sizes, so leave them in
		// case users have been relying on them. It is *possible* that A-series
		// role sizes are available, but we cannot automatically use them as
		// they *may* not be.
		return false
	}
	return true
}

// findInstanceSpec returns the InstanceSpec that best satisfies the supplied
// InstanceConstraint.
func findInstanceSpec(
	env environs.Environ,
	instanceTypesMap map[string]instances.InstanceType,
	constraint *instances.InstanceConstraint,
) (*instances.InstanceSpec, error) {
	constraint.Constraints = defaultToBaselineSpec(constraint.Constraints)
	imageData, err := findMatchingImages(env, constraint.Region, constraint.Series, constraint.Arches)
	if err != nil {
		return nil, err
	}
	images := instances.ImageMetadataToImages(imageData)
	instanceTypes := make([]instances.InstanceType, 0, len(instanceTypesMap))
	for _, instanceType := range instanceTypesMap {
		instanceTypes = append(instanceTypes, instanceType)
	}
	return instances.FindInstanceSpec(images, constraint, instanceTypes)
}

// If you specify no constraints at all, you're going to get the smallest
// instance type available.  In practice that one's a bit small.  So unless
// the constraints are deliberately set lower, this gives you a set of
// baseline constraints that are just slightly more ambitious than that.
func defaultToBaselineSpec(constraint constraints.Value) constraints.Value {
	result := constraint
	if !result.HasInstanceType() && result.Mem == nil {
		var value uint64 = defaultMem
		result.Mem = &value
	}
	return result
}
