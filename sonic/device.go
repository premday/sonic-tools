// Package sonic reads and writes the state of a Community SONiC switch.
//
// It gets its data from the local Redis databases, the SONiC and FRR CLIs,
// the configuration files and the kernel. It must run on the switch itself.
package sonic

import (
	"context"
	"fmt"
	"os"

	redis "github.com/redis/go-redis/v9"
)

// Device is the full state of a switch, as needed to validate it.
type Device struct {
	Hostname           string                 `json:"hostname"`
	Platform           Platform               `json:"platform"`
	Interfaces         []Interface            `json:"interfaces"`
	PSU                []PSU                  `json:"psu"`
	Fans               []Fan                  `json:"fans"`
	Temperatures       []Temperature          `json:"temperatures"`
	BGPNeighbors       map[string]BGPNeighbor `json:"bgp_neighbors"`
	RouteMaps          map[string]RouteMap    `json:"route_maps"`
	Containers         []Container            `json:"containers"`
	Users              []string               `json:"users"`
	SNMP               map[string]any         `json:"snmp"`
	DHCPRelayProcesses int                    `json:"dhcp_relay_processes"`
	IPTables           IPTables               `json:"iptables"`
	Sysctls            map[string]string      `json:"sysctls"`
	Errors             []string               `json:"errors"`
}

// Collect gathers the whole device state. Every section is best effort: failures are
// reported in Errors, so that a device can still be validated on partial data.
func Collect(ctx context.Context, rdb *redis.Client) Device {
	device := Device{Sysctls: Sysctls(SysctlKeys)}

	fail := func(section string, err error) {
		device.Errors = append(device.Errors, fmt.Sprintf("%s: %s", section, err))
	}

	var err error
	if device.Hostname, err = os.Hostname(); err != nil {
		fail("hostname", err)
	}
	if device.Platform, err = PlatformInfo(ctx, rdb); err != nil {
		fail("platform", err)
	}

	// the interfaces are collected even when lldpd is down, the neighbors are then empty
	lldp, err := LLDPNeighbors(ctx)
	if err != nil {
		fail("lldp", err)
	}
	if device.Interfaces, err = Interfaces(ctx, rdb, lldp); err != nil {
		fail("interfaces", err)
	}
	if device.PSU, err = PSUStatus(ctx); err != nil {
		fail("psu", err)
	}
	if device.Fans, err = FanStatus(ctx, rdb); err != nil {
		fail("fan", err)
	}
	if device.Temperatures, err = TemperatureStatus(ctx, rdb); err != nil {
		fail("temperature", err)
	}
	if device.BGPNeighbors, err = BGPStatus(ctx, rdb); err != nil {
		fail("bgp", err)
	}
	if device.RouteMaps, err = RouteMaps(ctx); err != nil {
		fail("route-maps", err)
	}
	if device.Containers, err = Containers(ctx); err != nil {
		fail("containers", err)
	}
	if device.Users, err = HumanUsers(); err != nil {
		fail("users", err)
	}
	if device.SNMP, err = SNMPConfig(ctx, rdb); err != nil {
		fail("snmp", err)
	}
	if device.DHCPRelayProcesses, err = DHCPRelayProcesses(ctx); err != nil {
		fail("dhcp relay", err)
	}
	if device.IPTables, err = IPTablesRules(ctx); err != nil {
		fail("iptables", err)
	}

	return device
}
