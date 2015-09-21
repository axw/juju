// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azure

import "fmt"

var knownRegions = []string{
	"Japan West",
	"West Europe",
	"East US 2",
	"Southeast Asia",
	"Central India",
	"South Central US",
	"Australia Southeast",
	"West India",
	"Australia East",
	"East Asia",
	"East US",
	"Central US",
	"West US",
	"North Europe",
	"North Central US",
	"Japan East",
	"China East",
	"South India",
	"China North",
	"Brazil South",
}

// regionFromLocation returns the region string defined in simplestreams
// image metadata that coresponds to the canonicalized location string
// that Azure understands.
func regionFromLocation(location string) string {
	for _, region := range knownRegions {
		if canonicalLocation(region) == location {
			return region
		}
	}
	// TODO(axw) if we find a location that we don't know about,
	// query Azure for the list of valid locations, which returns
	// with the strings that simplestreams knows and loves.
	panic(fmt.Sprintf("unknown location %q", location))
}
