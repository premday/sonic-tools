package fetcher

import (
	"fmt"
	"net/netip"

	"github.com/vishvananda/netlink"
)

type IPNeighbor struct {
	Interface string
	Alias     string
	MAC       string
	Found     bool
}

func FetchNetlinkNeighbor(addr netip.Addr) (IPNeighbor, error) {
	var family int
	if addr.Is4() {
		family = netlink.FAMILY_V4
	} else {
		family = netlink.FAMILY_V6
	}

	neighs, err := netlink.NeighList(0, family)
	if err != nil {
		return IPNeighbor{}, fmt.Errorf("failed to list neighbours: %w", err)
	}

	for _, n := range neighs {
		a, err := netip.ParseAddr(n.IP.String())
		if err != nil {
			continue
		}
		if a != addr {
			continue
		}

		iface, err := netlink.LinkByIndex(n.LinkIndex)
		if err != nil {
			return IPNeighbor{}, fmt.Errorf("unable to interface from index '%d'", n.LinkIndex)
		}

		return IPNeighbor{
			Interface: iface.Attrs().Name,
			Alias:     iface.Attrs().Alias,
			MAC:       n.HardwareAddr.String(),
			Found:     true,
		}, nil
	}

	return IPNeighbor{}, nil
}
