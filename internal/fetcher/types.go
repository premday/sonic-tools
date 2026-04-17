package fetcher

import (
	"encoding/json"
	"fmt"
)

type RouteTable map[string][]RouteEntry

type RouteEntry struct {
	DestSelected             bool      `json:"destSelected"`
	Distance                 int       `json:"distance"`
	Installed                bool      `json:"installed"`
	InstalledNexthopGroupID  int       `json:"installedNexthopGroupId"`
	InternalFlags            int       `json:"internalFlags"`
	InternalNextHopActiveNum int       `json:"internalNextHopActiveNum"`
	InternalNextHopNum       int       `json:"internalNextHopNum"`
	InternalStatus           int       `json:"internalStatus"`
	Metric                   int       `json:"metric"`
	NexthopGroupID           int       `json:"nexthopGroupId"`
	Nexthops                 []Nexthop `json:"nexthops"`
	Prefix                   string    `json:"prefix"`
	PrefixLen                int       `json:"prefixLen"`
	Protocol                 string    `json:"protocol"`
	Selected                 bool      `json:"selected"`
	Table                    int       `json:"table"`
	Uptime                   string    `json:"uptime"`
	VrfID                    int       `json:"vrfId"`
	VrfName                  string    `json:"vrfName"`
}

type Nexthop struct {
	Active            bool   `json:"active"`
	Afi               string `json:"afi"`
	DirectlyConnected bool   `json:"directlyConnected"`
	Fib               bool   `json:"fib"`
	Flags             int    `json:"flags"`
	InterfaceIndex    int    `json:"interfaceIndex"`
	InterfaceName     string `json:"interfaceName"`
	IP                string `json:"ip"`
	Weight            int    `json:"weight"`
}

type LLDP struct {
	LLDP LLDPInterfaces `json:"lldp"`
}

type LLDPInterfaces struct {
	Interface []map[string]InterfaceDetail `json:"interface"`
}

type InterfaceDetail struct {
	RID     string           `json:"rid"`
	Age     string           `json:"age"`
	Chassis ChassisContainer `json:"chassis"`
	Port    Port             `json:"port"`
}

type ChassisContainer map[string]Chassis

type Chassis struct {
	ID         ID     `json:"id"`
	Descr      string `json:"descr"`
	MgmtIP     any    `json:"mgmt-ip"`
	Capability any    `json:"capability"`
}

type ID struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// type Capability struct {
// 	Type    string `json:"type"`
// 	Enabled bool   `json:"enabled"`
// }

type Port struct {
	ID    ID     `json:"id"`
	Descr string `json:"descr"`
	TTL   string `json:"ttl"`
}

func (cc *ChassisContainer) UnmarshalJSON(data []byte) error {
	// Initialize the map
	*cc = make(map[string]Chassis)

	// handle: {"id": ...}
	var directChassis Chassis
	if err := json.Unmarshal(data, &directChassis); err == nil {
		// Check if this looks like a valid direct chassis (has required fields)
		if directChassis.ID.Type != "" || directChassis.ID.Value != "" {
			// Use chassis ID value as the key for direct chassis
			key := directChassis.ID.Value
			if key == "" {
				key = "N/A"
			}
			(*cc)[key] = directChassis
			return nil
		}
	}

	// handle: {"<hostame>": {"id": ...}}
	var chassisMap map[string]Chassis
	if err := json.Unmarshal(data, &chassisMap); err != nil {
		return fmt.Errorf("failed to unmarshal chassis as either direct object or map: %w", err)
	}

	*cc = chassisMap
	return nil
}
