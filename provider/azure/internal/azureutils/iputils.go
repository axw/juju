// Copyright 2015 Canonical Ltd.
// Licensed under the AGPLv3, see LICENCE file for details.

package azureutils

import (
	"math/big"
	"net"

	"github.com/juju/errors"
)

// NextGlobalUnicastIPAddress returns the next available
// global unicast IP address in a given subnet.
func NextGlobalUnicastIPAddress(subnet *net.IPNet, ipsInUse []net.IP) (net.IP, error) {
	ones, bits := subnet.Mask.Size()
	subnetMaskUint32 := ipUint32(net.IP(subnet.Mask))

	inUse := big.NewInt(0)
	for _, ip := range ipsInUse {
		if !subnet.Contains(ip) {
			continue
		}
		index := ipIndex(ip, subnetMaskUint32)
		inUse = inUse.SetBit(inUse, index, 1)
	}

	// Now iterate through all addresses in the subnet and return the
	// first global-unicast address that is not in use.
	n := ipUint32(subnet.IP)
	for i := 0; i < (1 << uint64(bits-ones)); i++ {
		ip := uint32IP(n + uint32(i))
		if !ip.IsGlobalUnicast() {
			continue
		}
		index := ipIndex(ip, subnetMaskUint32)
		if inUse.Bit(index) == 0 {
			return ip, nil
		}
	}
	return nil, errors.Errorf(
		"no global unicast IP addresses available in %s",
		subnet,
	)
}

// ipIndex calculates the index of the IP in the subnet.
// e.g. 10.0.0.1 in 10.0.0.0/8 has index 1.
func ipIndex(ip net.IP, subnetMask uint32) int {
	return int(ipUint32(ip) & ^subnetMask)
}

func ipUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32IP(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}
