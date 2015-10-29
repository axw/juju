// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azureutils_test

import (
	"fmt"
	"net"

	jc "github.com/juju/testing/checkers"
	gc "gopkg.in/check.v1"

	"github.com/juju/juju/provider/azure/internal/azureutils"
	"github.com/juju/juju/testing"
)

type iputilsSuite struct {
	testing.BaseSuite
}

var _ = gc.Suite(&iputilsSuite{})

func (*iputilsSuite) TestNextSubnetIP(c *gc.C) {
	assertNextSubnetIP(c, "10.0.0.0/8", nil, "10.0.0.4")
	assertNextSubnetIP(c, "10.0.0.0/8", []string{"10.0.0.1"}, "10.0.0.4")
	assertNextSubnetIP(c, "10.0.0.0/8", []string{"10.0.0.1", "10.0.0.4"}, "10.0.0.5")
}

func (*iputilsSuite) TestNextSubnetIPErrors(c *gc.C) {
	// The subnet is too small to have any non-reserved addresses.
	assertNextSubnetIPError(
		c,
		"10.1.2.0/30",
		nil,
		"no addresses available in 10.1.2.0/30",
	)

	// All addresses in use.
	var addresses []string
	for i := 1; i < 255; i++ {
		addr := fmt.Sprintf("10.0.0.%d", i)
		addresses = append(addresses, addr)
	}
	assertNextSubnetIPError(
		c, "10.0.0.0/24", addresses,
		"no addresses available in 10.0.0.0/24",
	)
}

func (*iputilsSuite) TestNextSubnet(c *gc.C) {
	assertNextSubnet(c, "10.0.0.0/8", 16, nil, "10.0.0.0/16")
	assertNextSubnet(c, "10.0.0.0/8", 16, []string{"10.0.0.0/16"}, "10.1.0.0/16")
	assertNextSubnet(c, "10.0.0.0/8", 16, []string{"10.1.0.0/16", "10.0.0.0/16"}, "10.2.0.0/16")
	assertNextSubnet(c, "10.0.0.0/8", 16, []string{"10.2.0.0/16", "10.0.0.0/16"}, "10.1.0.0/16")

	// subnets with mismatched prefixes are ignored
	assertNextSubnet(c, "10.0.0.0/8", 16, []string{"10.0.0.0/8"}, "10.0.0.0/16")
	assertNextSubnet(c, "10.0.0.0/8", 16, []string{"11.0.0.0/16"}, "10.0.0.0/16")
}

func (*iputilsSuite) TestNextSubnetErrors(c *gc.C) {
	// Subnet prefix is <= vnet prefix.
	assertNextSubnetError(c, "10.0.0.0/8", 8, nil, "subnet prefix /8 >= vnet prefix /8")

	// All subnets in use.
	var assigned []string
	for i := 0; i < 256; i++ {
		subnet := fmt.Sprintf("10.%d.0.0/16", i)
		assigned = append(assigned, subnet)
	}
	assertNextSubnetError(c, "10.0.0.0/8", 16, assigned, "no /16 subnets available in 10.0.0.0/8")
}

func assertNextSubnetIP(c *gc.C, ipnetString string, inuseStrings []string, expectedString string) {
	ipnet := parseIPNet(c, ipnetString)
	inuse := parseIPs(c, inuseStrings...)
	next, err := azureutils.NextSubnetIP(ipnet, inuse)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(next.String(), gc.Equals, expectedString)
}

func assertNextSubnetIPError(c *gc.C, ipnetString string, inuseStrings []string, expect string) {
	ipnet := parseIPNet(c, ipnetString)
	inuse := parseIPs(c, inuseStrings...)
	_, err := azureutils.NextSubnetIP(ipnet, inuse)
	c.Assert(err, gc.ErrorMatches, expect)
}

func assertNextSubnet(c *gc.C, ipnetString string, subnetPrefix int, inuseSubnetStrings []string, expectedString string) {
	ipnet := parseIPNet(c, ipnetString)
	inuseSubnets := make([]*net.IPNet, len(inuseSubnetStrings))
	for i, s := range inuseSubnetStrings {
		inuseSubnets[i] = parseIPNet(c, s)
	}
	next, err := azureutils.NextSubnet(ipnet, subnetPrefix, inuseSubnets)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(next.String(), gc.Equals, expectedString)
}

func assertNextSubnetError(c *gc.C, ipnetString string, subnetPrefix int, inuseSubnetStrings []string, expect string) {
	ipnet := parseIPNet(c, ipnetString)
	inuseSubnets := make([]*net.IPNet, len(inuseSubnetStrings))
	for i, s := range inuseSubnetStrings {
		inuseSubnets[i] = parseIPNet(c, s)
	}
	_, err := azureutils.NextSubnet(ipnet, subnetPrefix, inuseSubnets)
	c.Assert(err, gc.ErrorMatches, expect)
}

func parseIPs(c *gc.C, ipStrings ...string) []net.IP {
	ips := make([]net.IP, len(ipStrings))
	for i, ipString := range ipStrings {
		ip := net.ParseIP(ipString)
		c.Assert(ip, gc.NotNil, gc.Commentf("failed to parse IP %q", ipString))
		ips[i] = ip
	}
	return ips
}

func parseIPNet(c *gc.C, cidr string) *net.IPNet {
	_, ipnet, err := net.ParseCIDR(cidr)
	c.Assert(err, jc.ErrorIsNil)
	return ipnet
}
