package analyzer

import (
	"context"
	"net/netip"
	"strings"

	"github.com/premday/sonic-tools/internal/fetcher"

	"log"
)

// InterfaceLink holds all per-interface data for the merged Interface section.
type InterfaceLink struct {
	Interface   string
	Description string
	LLDPHost    string
	LLDPPort    string
	Address     netip.Prefix
}

// InterfaceInfo holds interfaces whose IP subnet contains the target IP,
// enriched with CONFIG_DB description and LLDP neighbour data.
type InterfaceInfo struct {
	ip    netip.Addr
	Links []InterfaceLink
}

func (i InterfaceInfo) String() string {
	var buf strings.Builder
	buf.WriteString(sectionHeader("Matching interface"))

	if len(i.Links) == 0 {
		buf.WriteString(sectionNotFound("No matching interface"))
		return buf.String()
	}

	t := newTable("Interface", "Address", "Description", "LLDP host", "LLDP port")
	for _, link := range i.Links {
		t.addRow(
			link.Interface,
			link.Address.String(),
			link.Description,
			link.LLDPHost,
			link.LLDPPort,
		)
	}
	buf.WriteString(t.String())

	return buf.String()
}

// GetInterfaceInfo returns interface information for all interfaces whose IP
// subnet contains the target IP, merging address, description and LLDP data.
func (a *IPAnalyzer) GetInterfaceInfo(ctx context.Context) InterfaceInfo {
	info := InterfaceInfo{ip: a.netIP}

	for _, intf := range a.interfaceAddr {
		if !intf.Prefix.Contains(a.netIP) {
			continue
		}

		lldpHost, lldpPort := a.lldpNeighbors.ExtractInterfaceNeighbor(intf.Name)
		intfInfo, err := fetcher.GetInterfaceInformation(ctx, a.rdb, intf.Name)
		if err != nil {
			log.Println("failed to extract interface information")
		}

		info.Links = append(info.Links, InterfaceLink{
			Interface:   intf.Name,
			Description: intfInfo.Description,
			LLDPHost:    lldpHost,
			LLDPPort:    lldpPort,
			Address:     intf.Prefix,
		})
	}

	return info
}
