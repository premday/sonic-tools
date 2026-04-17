package fetcher

import (
	"fmt"
	"net/netip"
)

type Route struct {
	NextHopIP         string
	LocalInterface    string
	DirectlyConnected bool
}

func ExtractRoutes(routeTable RouteTable) []Route {
	outIntf := []Route{}
	for _, gateways := range routeTable {
		for _, gw := range gateways {
			for _, nh := range gw.Nexthops {
				outIntf = append(outIntf, Route{nh.IP, nh.InterfaceName, nh.DirectlyConnected})
			}
		}
	}
	return outIntf
}

func FetchIPRoute(addr netip.Addr) (RouteTable, error) {
	ipCmd := "ip"
	if !addr.Is4() {
		ipCmd = "ipv6"
	}
	command := fmt.Sprintf("show %s route %s json", ipCmd, addr.String())
	var routeTable RouteTable
	if err := runCommand(command, &routeTable); err != nil {
		return nil, err
	}
	return routeTable, nil
}
