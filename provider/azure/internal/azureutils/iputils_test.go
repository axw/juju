// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azureutils_test

import (
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

func (*iputilsSuite) TestNextGlobalUnicastIPAddress(c *gc.C) {
	assertNextGlobalUnicastAddress(c, "10.0.0.0/8", nil, "10.0.0.0")
	assertNextGlobalUnicastAddress(c, "10.0.0.0/8", []string{"10.0.0.0"}, "10.0.0.1")
	assertNextGlobalUnicastAddress(c, "10.0.0.0/8", []string{"10.0.0.0", "10.0.0.1", "10.0.0.3"}, "10.0.0.2")
	assertNextGlobalUnicastAddress(c, "10.1.2.0/31", nil, "10.1.2.0")
	assertNextGlobalUnicastAddress(c, "10.1.2.0/31", []string{"10.1.2.0"}, "10.1.2.1")
}

func assertNextGlobalUnicastAddress(c *gc.C, ipnetString string, inuseStrings []string, expectedString string) {
	ipnet := parseIPNet(c, ipnetString)
	inuse := parseIPs(c, inuseStrings...)
	next, err := azureutils.NextGlobalUnicastIPAddress(ipnet, inuse)
	c.Assert(err, jc.ErrorIsNil)
	c.Assert(next.String(), gc.Equals, expectedString)
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
