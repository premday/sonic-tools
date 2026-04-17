package analyzer

import (
	"context"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/premday/sonic-tools/internal/fetcher"

	"log"
)

type routeLocalAsset struct {
	Host           string
	Interface      string
	InterfaceAlias string
	Addresses      []netip.Prefix
}

type routeRemoteAsset struct {
	Host      string
	Interface string
	Address   netip.Addr
}

type Route struct {
	Local   routeLocalAsset
	Remote  routeRemoteAsset
	NextHop string
}

type RoutingInfo struct {
	ip     netip.Addr
	Routes []Route
}

func (r RoutingInfo) String() string {
	var buf strings.Builder
	buf.WriteString(sectionHeader("Routing"))

	if len(r.Routes) == 0 {
		buf.WriteString(sectionNotFound("No route found"))
		return buf.String()
	}

	t := newTable("Interface", "Alias", "Address", "Next hop", "LLDP host", "LLDP port")
	for _, route := range r.Routes {
		localAddrs := []string{}
		for _, addr := range route.Local.Addresses {
			localAddrs = append(localAddrs, addr.String())
		}

		t.addRow(
			route.Local.Interface,
			route.Local.InterfaceAlias,
			strings.Join(localAddrs, "|"),
			route.NextHop,
			route.Remote.Host,
			route.Remote.Interface,
		)
	}
	buf.WriteString(t.String())

	return buf.String()
}

func (a *IPAnalyzer) GetRoutingInfo(ctx context.Context) RoutingInfo {
	info := RoutingInfo{ip: a.netIP}
	for _, gw := range a.routes {
		localInterfaceAddresses, err := fetcher.GetIPInterface(ctx, a.rdb, gw.LocalInterface)
		if err != nil {
			fmt.Printf("failed to get IP address for %s: %s", gw.LocalInterface, err)
		}
		localAddresses := []string{}
		localPrefixes := []netip.Prefix{}
		for _, localAddr := range localInterfaceAddresses {
			if a.netIP.Is4() == localAddr.Addr().Is4() {
				localAddresses = append(localAddresses, localAddr.Addr().String())
				localPrefixes = append(localPrefixes, localAddr)
			}
		}

		remoteIP, _ := netip.ParseAddr(gw.NextHopIP)
		nextHop := gw.NextHopIP
		// if route is "connected" type, remote IP is the one passed to the CLI
		if gw.DirectlyConnected {
			nextHop = "connected"
			// unless the IP passed to the CLI is a local IP address
			if !slices.Contains(localAddresses, a.netIP.String()) {
				remoteIP = a.netIP
			} else if len(localPrefixes) > 0 {
				if otherIP, err := fetcher.GetOtherP2PHost(localPrefixes[0]); err == nil {
					remoteIP = otherIP.Addr()
				}
			}
		}

		intfInfo, err := fetcher.GetInterfaceInformation(ctx, a.rdb, gw.LocalInterface)
		if err != nil {
			log.Println("failed to extract interface information")
		}
		lldpHost, lldpPort := a.lldpNeighbors.ExtractInterfaceNeighbor(gw.LocalInterface)

		info.Routes = append(info.Routes, Route{
			Local: routeLocalAsset{
				Host:           a.localHostname,
				Interface:      gw.LocalInterface,
				InterfaceAlias: intfInfo.Alias,
				Addresses:      localPrefixes,
			},
			Remote: routeRemoteAsset{
				Host:      lldpHost,
				Interface: lldpPort,
				Address:   remoteIP,
			},
			NextHop: nextHop,
		})
	}
	return info
}
