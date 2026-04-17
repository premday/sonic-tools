package fetcher

import (
	"errors"
	"net/netip"
)

// GetOtherP2PHost returns the other host IP in the provided CIDR.
//
// It assumes the input is a host in a p2p network (/30 or /31 for ipv4, /127 for ipv6).
func GetOtherP2PHost(prefix netip.Prefix) (netip.Prefix, error) {
	isV4 := prefix.Addr().Is4()
	mask := prefix.Bits()

	switch {
	case isV4 && mask == 30:
		otherHost := prefix.Addr().As4()

		// check if network or broadcast address, we only need the last two bits (3 = 0b11)
		if last2bits := otherHost[3] & 3; last2bits == 0 || last2bits == 3 {
			return netip.Prefix{}, errors.New("not supported address: network or broadcast address")
		}

		otherHost[3] ^= 3 // toggle the last two bits to get the other host (using XOR 0b11)
		return netip.PrefixFrom(netip.AddrFrom4(otherHost), prefix.Bits()), nil

	case isV4 && mask == 31:
		otherHost := prefix.Addr().As4()
		otherHost[3] ^= 1 // toggle the last bit to get the other host (using XOR 0b01)
		return netip.PrefixFrom(netip.AddrFrom4(otherHost), prefix.Bits()), nil

	case !isV4 && mask == 127:
		otherHost := prefix.Addr().As16()
		otherHost[15] ^= 1 // toggle the last bit to get the other host (using XOR 0b01)
		return netip.PrefixFrom(netip.AddrFrom16(otherHost), prefix.Bits()), nil

	default:
		return netip.Prefix{}, errors.New("unsupported network: not point to point")
	}
}
