package fetcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	redis "github.com/redis/go-redis/v9"
)

type FDBEntry struct {
	VlanID int
	MAC    string
	Iface  string
}

type ResolvedNeighbor struct {
	IP     netip.Addr `json:"ip"`
	MAC    string     `json:"mac"`
	Iface  string     `json:"iface"`
	VlanID int        `json:"vlan_id"`
	IsVlan bool       `json:"is_vlan"`
	Found  bool       `json:"found"`
}

// ResolveNeighborInterface finds the real physical/logical interface for a
// given IP address. On SONiC, when a neighbor is learned on a VLAN interface
// (e.g. Vlan1000), the kernel neighbor table reports the VLAN interface name
// and the MAC address. This function looks up that MAC in the ASIC_DB FDB
// table to find the actual port (e.g. Ethernet20, PortChannel0001) where the
// MAC was learned.
//
// The resolution follows the same logic as the SONiC nbrshow utility.
//
// The FDB resolution requires the following lookups in Redis:
//   - COUNTERS_DB: COUNTERS_PORT_NAME_MAP + COUNTERS_LAG_NAME_MAP -> OID-to-interface-name map
//   - ASIC_DB: SAI_OBJECT_TYPE_BRIDGE_PORT -> bridge-port-OID to port-OID map
//   - ASIC_DB: SAI_OBJECT_TYPE_FDB_ENTRY -> (VLAN, MAC) to bridge-port-OID
//   - ASIC_DB: SAI_OBJECT_TYPE_VLAN -> bvid to VLAN ID (when needed)
func ResolveNeighborInterface(ctx context.Context, rdb *redis.Client, addr netip.Addr) (ResolvedNeighbor, error) {
	neighbor, err := FetchNetlinkNeighbor(addr)
	if err != nil {
		return ResolvedNeighbor{}, fmt.Errorf("failed to fetch neighbor for %s: %w", addr, err)
	}
	if !neighbor.Found {
		return ResolvedNeighbor{}, nil
	}

	result := ResolvedNeighbor{
		IP:    addr,
		MAC:   neighbor.MAC,
		Iface: neighbor.Interface,
		Found: true,
	}

	vlanRegex := regexp.MustCompile(`^Vlan(\d+)$`)
	matches := vlanRegex.FindStringSubmatch(neighbor.Interface)
	if matches == nil {
		return result, nil
	}

	vlanID, err := strconv.Atoi(matches[1])
	if err != nil {
		return result, nil //nolint:nilerr // we return as-is if invalid VLAN ID
	}
	result.IsVlan = true
	result.VlanID = vlanID

	// lookup in ASIC_DB to find the underlying port
	result.Iface, err = resolveVlanToPort(ctx, rdb, vlanID, neighbor.MAC)
	if err != nil {
		return result, fmt.Errorf("failed to resolve VLAN interface for %s: %w", addr, err)
	}

	return result, nil
}

// resolveVlanToPort looks up the ASIC_DB FDB table to find the physical/logical interface.
func resolveVlanToPort(ctx context.Context, rdb *redis.Client, vlanID int, mac string) (string, error) {
	interfaceOIDMap, err := getInterfaceOIDMap(ctx, rdb)
	if err != nil {
		return "", fmt.Errorf("failed to get interface OID map: %w", err)
	}

	bridgePortMap, err := getBridgePortMap(ctx, rdb)
	if err != nil {
		return "", fmt.Errorf("failed to get bridge port map: %w", err)
	}

	fdbEntries, err := getFDBEntries(ctx, rdb, interfaceOIDMap, bridgePortMap)
	if err != nil {
		return "", fmt.Errorf("failed to get FDB entries: %w", err)
	}

	for _, fdb := range fdbEntries {
		if fdb.VlanID == vlanID && strings.EqualFold(fdb.MAC, mac) {
			return fdb.Iface, nil
		}
	}

	return "", nil
}

// getInterfaceOIDMap returns a map of SAI OID (without "oid:0x" prefix) to interface name.
func getInterfaceOIDMap(ctx context.Context, rdb *redis.Client) (map[string]string, error) {
	conn := rdb.Conn()
	defer conn.Close()

	if err := conn.Select(ctx, COUNTERSDB).Err(); err != nil {
		return nil, fmt.Errorf("failed to select COUNTERS_DB: %w", err)
	}

	oidMap := make(map[string]string)
	const oidPrefix = "oid:0x"

	portMap, err := conn.HGetAll(ctx, "COUNTERS_PORT_NAME_MAP").Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("failed to get COUNTERS_PORT_NAME_MAP: %w", err)
	}
	for interfaceName, saiOID := range portMap {
		oid := strings.TrimPrefix(saiOID, oidPrefix)
		oidMap[oid] = interfaceName
	}

	return oidMap, nil
}

// getBridgePortMap returns a map of bridge_port_oid <-> port_oid.
func getBridgePortMap(ctx context.Context, rdb *redis.Client) (map[string]string, error) {
	conn := rdb.Conn()
	defer conn.Close()

	if err := conn.Select(ctx, ASICDB).Err(); err != nil {
		return nil, fmt.Errorf("failed to select ASIC_DB: %w", err)
	}

	const keyPattern = "ASIC_STATE:SAI_OBJECT_TYPE_BRIDGE_PORT:*"
	const keyPrefix = "ASIC_STATE:SAI_OBJECT_TYPE_BRIDGE_PORT:oid:0x"
	const oidPrefix = "oid:0x"

	bridgePortMap := make(map[string]string)

	iter := conn.Scan(ctx, 0, keyPattern, 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		bridgePortID := strings.TrimPrefix(key, keyPrefix)

		portIDRaw, err := conn.HGet(ctx, key, "SAI_BRIDGE_PORT_ATTR_PORT_ID").Result()
		if err != nil {
			// Skip entries without a port ID attribute
			continue
		}
		bridgePortMap[bridgePortID] = strings.TrimPrefix(portIDRaw, oidPrefix)
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan bridge port entries: %w", err)
	}

	return bridgePortMap, nil
}

type fdbKeyJSON struct {
	BvID     string `json:"bvid"`
	MAC      string `json:"mac"`
	SwitchID string `json:"switch_id"`
	Vlan     string `json:"vlan"`
}

func getFDBEntries(ctx context.Context, rdb *redis.Client, ifOIDMap map[string]string, bridgePortMap map[string]string) ([]FDBEntry, error) {
	conn := rdb.Conn()
	defer conn.Close()

	if err := conn.Select(ctx, ASICDB).Err(); err != nil {
		return nil, fmt.Errorf("failed to select ASIC_DB: %w", err)
	}

	const keyPattern = "ASIC_STATE:SAI_OBJECT_TYPE_FDB_ENTRY:*"
	const oidPrefix = "oid:0x"

	entries := []FDBEntry{}

	iter := conn.Scan(ctx, 0, keyPattern, 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()

		// the key format is: ASIC_STATE:SAI_OBJECT_TYPE_FDB_ENTRY:{json}
		parts := strings.SplitN(key, ":", 3)
		if len(parts) < 3 {
			continue
		}

		fdbKey := fdbKeyJSON{}
		if err := json.Unmarshal([]byte(parts[2]), &fdbKey); err != nil {
			continue
		}

		bridgePortIDRaw, err := conn.HGet(ctx, key, "SAI_FDB_ENTRY_ATTR_BRIDGE_PORT_ID").Result()
		if err != nil {
			continue
		}
		bridgePortID := strings.TrimPrefix(bridgePortIDRaw, oidPrefix)

		portOID, ok := bridgePortMap[bridgePortID]
		if !ok {
			continue
		}

		interfaceName, ok := ifOIDMap[portOID]
		if !ok {
			interfaceName = portOID
		}

		vlanID := 0
		if fdbKey.Vlan != "" {
			vlanID, _ = strconv.Atoi(fdbKey.Vlan)
		} else if fdbKey.BvID != "" {
			vid, err := getVlanIDFromBvID(ctx, conn, fdbKey.BvID)
			if err != nil || vid == 0 {
				continue
			}
			vlanID = vid
		}

		entries = append(entries, FDBEntry{
			VlanID: vlanID,
			MAC:    strings.ToUpper(fdbKey.MAC),
			Iface:  interfaceName,
		})
	}
	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan FDB entries: %w", err)
	}

	return entries, nil
}

// getVlanIDFromBvID resolves a Bridge VLAN OID (bvid) to a numeric VLAN ID.
func getVlanIDFromBvID(ctx context.Context, conn *redis.Conn, bvid string) (int, error) {
	key := fmt.Sprintf("ASIC_STATE:SAI_OBJECT_TYPE_VLAN:%s", bvid)
	vlanIDStr, err := conn.HGet(ctx, key, "SAI_VLAN_ATTR_VLAN_ID").Result()
	if err != nil {
		return 0, fmt.Errorf("failed to get VLAN ID for bvid %s: %w", bvid, err)
	}
	vlanID, err := strconv.Atoi(vlanIDStr)
	if err != nil {
		return 0, fmt.Errorf("invalid VLAN ID '%s' for bvid %s: %w", vlanIDStr, bvid, err)
	}
	return vlanID, nil
}
