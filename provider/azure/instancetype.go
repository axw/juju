// Copyright 2013 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import (
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/arm/compute"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/juju/errors"
	"github.com/juju/utils/arch"

	"github.com/juju/juju/constraints"
	"github.com/juju/juju/environs"
	"github.com/juju/juju/environs/imagemetadata"
	"github.com/juju/juju/environs/instances"
	"github.com/juju/juju/environs/simplestreams"
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
func findMatchingImages(env environs.Environ, region, series string, arches []string) ([]*imagemetadata.ImageMetadata, error) {
	endpoint := getEndpoint(region)
	constraint := imagemetadata.NewImageConstraint(simplestreams.LookupParams{
		CloudSpec: simplestreams.CloudSpec{region, endpoint},
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
		return nil, fmt.Errorf("no OS images found for region %q, series %q, architectures %q (and endpoint: %q)", region, series, arches, endpoint)
	} else if err != nil {
		return nil, err
	}
	return images, nil
}

// getEndpoint returns the simplestreams endpoint to use for the given Azure
// region (e.g. West Europe or China North).
func getEndpoint(region string) string {
	if strings.HasPrefix(region, "China") {
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
	sizeName := to.String(size.Name)
	for i, name := range machineSizeCost {
		if sizeName == name {
			cost = i
			break
		}
	}
	if cost == len(machineSizeCost) {
		logger.Warningf("found unknown VM size %q", sizeName)
	}

	vtype := "Hyper-V"
	return instances.InstanceType{
		Id:       sizeName,
		Name:     sizeName,
		Arches:   []string{arch.AMD64},
		CpuCores: uint64(to.Int(size.NumberOfCores)),
		Mem:      uint64(to.Int(size.MemoryInMB)),
		RootDisk: uint64(to.Int(size.OsDiskSizeInMB)),
		Cost:     uint64(cost),
		VirtType: &vtype,
		// tags are not currently supported by azure
	}
}

// findInstanceSpec returns the InstanceSpec that best satisfies the supplied
// InstanceConstraint.
func findInstanceSpec(
	env environs.Environ,
	instanceTypesMap map[string]instances.InstanceType,
	constraint *instances.InstanceConstraint,
) (*instances.InstanceSpec, error) {
	constraint.Constraints = defaultToBaselineSpec(constraint.Constraints)
	/*
		imageData, err := findMatchingImages(env, constraint.Region, constraint.Series, constraint.Arches)
		if err != nil {
			return nil, err
		}
		images := instances.ImageMetadataToImages(imageData)
	*/
	// TODO(axw) hack, don't use simplestreams.
	images := []instances.Image{{
		Id:       "juju",
		Arch:     arch.AMD64,
		VirtType: "Hyper-V",
	}}
	instanceTypes := make([]instances.InstanceType, 0, len(instanceTypesMap))
	for _, instanceType := range instanceTypesMap {
		instanceTypes = append(instanceTypes, instanceType)
	}
	return instances.FindInstanceSpec(images, constraint, instanceTypes)
}

// If you specify no constraints at all, you're going to get the smallest
// instance type available. In practice that one's a bit small, so unless
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
