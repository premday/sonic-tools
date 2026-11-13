package sonic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// Addr is an IP address of a BGP session, it is empty when FRR reports it as "Unknown".
type Addr struct {
	netip.Addr
}

func (a *Addr) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	a.Addr, _ = netip.ParseAddr(value)
	return nil
}

type BGPNeighbor struct {
	RemoteAddress Addr            `json:"remote_address"`
	LocalAddress  Addr            `json:"local_address"`
	RemoteAs      int             `json:"remote_as"`
	LocalAs       int             `json:"local_as"`
	PeerGroup     string          `json:"peer_group"`
	Description   string          `json:"description"`
	SAFI          map[string]SAFI `json:"safi"`
	BGPState      string          `json:"bgp_state"`
	Uptime        Uptime          `json:"uptime"`

	ConnectionsEstablished int    `json:"connections_established"`
	ConnectionsDropped     int    `json:"connections_dropped"`
	LastResetReason        string `json:"last_reset_reason"`

	// LocalInterface is the interface an eBGP session is expected to use, even when it is down.
	// It is empty for iBGP and for multihop sessions.
	LocalInterface       string `json:"local_interface"`
	LocalInterfaceAlias  string `json:"local_interface_alias"`
	LocalInterfaceStatus string `json:"local_interface_status"`
}

// frrNeighbor is a BGPNeighbor with the field names of FRR. Struct tags are ignored when
// converting between two structs, so the parsed value is returned as-is.
type frrNeighbor struct {
	RemoteAddress Addr            `json:"hostForeign"`
	LocalAddress  Addr            `json:"hostLocal"`
	RemoteAs      int             `json:"remoteAs"`
	LocalAs       int             `json:"localAs"`
	PeerGroup     string          `json:"peerGroup"`
	Description   string          `json:"nbrDesc"`
	SAFI          map[string]SAFI `json:"addressFamilyInfo"`
	BGPState      string          `json:"bgpState"`
	Uptime        Uptime          `json:"bgpTimerUpEstablishedEpoch"`

	ConnectionsEstablished int    `json:"connectionsEstablished"`
	ConnectionsDropped     int    `json:"connectionsDropped"`
	LastResetReason        string `json:"lastResetDueTo"`

	LocalInterface       string `json:"-"`
	LocalInterfaceAlias  string `json:"-"`
	LocalInterfaceStatus string `json:"-"`
}

func (n *BGPNeighbor) UnmarshalJSON(data []byte) error {
	var frr frrNeighbor
	if err := json.Unmarshal(data, &frr); err != nil {
		return err
	}
	*n = BGPNeighbor(frr)
	n.SAFI = snakeCaseFamilies(frr.SAFI)

	return nil
}

// snakeCaseFamilies renames the address families FRR reports as 'ipv4Unicast' or 'l2VpnEvpn'.
// They are map keys, so unlike every other name of the document no struct tag can rename them.
// A nil map stays nil, so a session which never came up still publishes a null 'safi'.
func snakeCaseFamilies(families map[string]SAFI) map[string]SAFI {
	if families == nil {
		return nil
	}

	renamed := make(map[string]SAFI, len(families))
	for family, safi := range families {
		renamed[snakeCase(family)] = safi
	}

	return renamed
}

// snakeCase converts a camelCase name, leaving an already converted one untouched. An underscore
// goes before a run of capitals and before its last letter, so a hypothetical 'l2VPNEvpn' reads
// 'l2_vpn_evpn' rather than 'l2_v_p_n_evpn'.
func snakeCase(name string) string {
	var out strings.Builder
	out.Grow(len(name) + 4)

	for i := range len(name) {
		char := name[i]
		if !isUpper(char) {
			out.WriteByte(char)
			continue
		}

		startsRun := i > 0 && !isUpper(name[i-1])
		endsRun := i+1 < len(name) && !isUpper(name[i+1])
		if i > 0 && (startsRun || endsRun) {
			out.WriteByte('_')
		}
		out.WriteByte(char + 'a' - 'A')
	}

	return out.String()
}

func isUpper(char byte) bool {
	return char >= 'A' && char <= 'Z'
}

// IsEBGP reports whether the session is an external one.
func (n BGPNeighbor) IsEBGP() bool {
	return n.RemoteAs != 0 && n.LocalAs != 0 && n.RemoteAs != n.LocalAs
}

type SAFI struct {
	ImportPolicy          string `json:"import_policy"`
	ExportPolicy          string `json:"export_policy"`
	AcceptedPrefixCounter int    `json:"accepted_prefix_counter"`
	SentPrefixCounter     int    `json:"sent_prefix_counter"`
	PrefixAllowedMax      int    `json:"prefix_allowed_max"`
}

type frrSAFI struct {
	ImportPolicy          string `json:"routeMapForIncomingAdvertisements"`
	ExportPolicy          string `json:"routeMapForOutgoingAdvertisements"`
	AcceptedPrefixCounter int    `json:"acceptedPrefixCounter"`
	SentPrefixCounter     int    `json:"sentPrefixCounter"`
	PrefixAllowedMax      int    `json:"prefixAllowedMax"`
}

func (s *SAFI) UnmarshalJSON(data []byte) error {
	var frr frrSAFI
	if err := json.Unmarshal(data, &frr); err != nil {
		return err
	}
	*s = SAFI(frr)

	return nil
}

type RouteMap struct {
	Invoked int    `json:"invoked"`
	Rules   []Rule `json:"rules"`
}

type Rule struct {
	SequenceNumber int      `json:"sequence_number"`
	Type           string   `json:"type"`
	Invoked        int      `json:"invoked"`
	MatchClauses   []string `json:"match_clauses"`
	SetClauses     []string `json:"set_clauses"`
	Action         string   `json:"action"`
}

type frrRule struct {
	SequenceNumber int      `json:"sequenceNumber"`
	Type           string   `json:"type"`
	Invoked        int      `json:"invoked"`
	MatchClauses   []string `json:"matchClauses"`
	SetClauses     []string `json:"setClauses"`
	Action         string   `json:"action"`
}

func (r *Rule) UnmarshalJSON(data []byte) error {
	var frr frrRule
	if err := json.Unmarshal(data, &frr); err != nil {
		return err
	}
	*r = Rule(frr)

	return nil
}

// Uptime is how long a BGP session has been established.
// FRR reports the epoch of the last establishment, it is null when the session never came up.
type Uptime struct {
	time.Duration
}

func (u *Uptime) UnmarshalJSON(data []byte) error {
	var epoch int64
	if err := json.Unmarshal(data, &epoch); err != nil {
		return err
	}
	if epoch > 0 {
		u.Duration = time.Since(time.Unix(epoch, 0)).Truncate(time.Second)
	}
	return nil
}

func (u Uptime) MarshalJSON() ([]byte, error) {
	return json.Marshal(int64(u.Seconds()))
}

// BGPStatus returns the neighbors of the device, with the local interface of the eBGP sessions.
func BGPStatus(ctx context.Context, rdb *redis.Client) (map[string]BGPNeighbor, error) {
	neighbors, err := BGPNeighbors(ctx)
	if err != nil {
		return nil, err
	}

	if err := resolveLocalInterfaces(ctx, rdb, neighbors); err != nil {
		return neighbors, err
	}

	return neighbors, nil
}

// resolveLocalInterfaces finds the interface each eBGP session is expected to use: the one
// whose subnet contains the neighbor address, or the peering interface for unnumbered peers.
func resolveLocalInterfaces(ctx context.Context, rdb *redis.Client, neighbors map[string]BGPNeighbor) error {
	addrs, err := InterfacesAddrs(ctx, rdb)
	if err != nil {
		return err
	}

	appl, err := openDB(ctx, rdb, APPLDB)
	if err != nil {
		return err
	}
	defer appl.Close()

	config, err := openDB(ctx, rdb, CONFIGDB)
	if err != nil {
		return err
	}
	defer config.Close()

	for peer, neighbor := range neighbors {
		if !neighbor.IsEBGP() {
			continue
		}

		// FRR keys the unnumbered peers by interface name instead of address
		neighbor.LocalInterface = peer
		if addr, err := netip.ParseAddr(peer); err == nil {
			neighbor.LocalInterface = localInterface(addrs, addr)
		}

		if neighbor.LocalInterface != "" {
			neighbor.LocalInterfaceAlias = config.HGet(ctx, "PORT|"+neighbor.LocalInterface, "alias").Val()
			neighbor.LocalInterfaceStatus, err = operStatus(ctx, appl, neighbor.LocalInterface)
			if err != nil {
				return err
			}
		}

		neighbors[peer] = neighbor
	}

	return nil
}

// localInterface returns the interface holding an address in its most specific subnet,
// as the traffic to the neighbor leaves through that one.
func localInterface(addrs []InterfaceAddr, addr netip.Addr) string {
	name, longest := "", -1

	for _, intf := range addrs {
		if intf.Prefix.Contains(addr) && intf.Prefix.Bits() > longest {
			name, longest = intf.Name, intf.Prefix.Bits()
		}
	}

	return name
}

func BGPNeighbors(ctx context.Context) (map[string]BGPNeighbor, error) {
	peers := map[string]BGPNeighbor{}
	if err := vtyshJSON(ctx, &peers, "show bgp neighbors json"); err != nil {
		return nil, err
	}
	return peers, nil
}

// FindBGPNeighbor returns a single neighbor, with the local interface of an eBGP session.
func FindBGPNeighbor(ctx context.Context, rdb *redis.Client, remoteAddr string) (BGPNeighbor, error) {
	out, err := vtysh(ctx, fmt.Sprintf("show bgp neighbors %s json", remoteAddr))
	if err != nil {
		return BGPNeighbor{}, err
	}

	var notFound struct {
		NoSuchNeighbor bool `json:"bgpNoSuchNeighbor"`
	}
	if err := json.Unmarshal([]byte(out), &notFound); err != nil {
		return BGPNeighbor{}, fmt.Errorf("failed to parse output of 'vtysh': %w", err)
	}
	if notFound.NoSuchNeighbor {
		return BGPNeighbor{}, fmt.Errorf("neighbor %s not found", remoteAddr)
	}

	peers := map[string]BGPNeighbor{}
	if err := json.Unmarshal([]byte(out), &peers); err != nil {
		return BGPNeighbor{}, fmt.Errorf("failed to parse output of 'vtysh': %w", err)
	}

	if _, exists := peers[remoteAddr]; !exists {
		return BGPNeighbor{}, fmt.Errorf("neighbor %s not found", remoteAddr)
	}

	if err := resolveLocalInterfaces(ctx, rdb, peers); err != nil {
		return peers[remoteAddr], err
	}

	return peers[remoteAddr], nil
}

func RouteMaps(ctx context.Context) (map[string]RouteMap, error) {
	res, err := vtysh(ctx, "show route-map json")
	if err != nil {
		return nil, err
	}
	return parseRouteMaps(res)
}

// parseRouteMaps reads the route-maps of bgpd out of the vtysh output, which holds one JSON
// object per daemon keyed by protocol name ('BGP' and 'ZEBRA', 'bgpd' and 'zebra' since
// FRR 10). Route-maps are shared by the daemons, so the others are only a fallback.
func parseRouteMaps(out string) (map[string]RouteMap, error) {
	routeMaps := map[string]RouteMap{}

	decoder := json.NewDecoder(strings.NewReader(out))
	for decoder.More() {
		var data map[string]map[string]RouteMap
		if err := decoder.Decode(&data); err != nil {
			return nil, fmt.Errorf("failed to parse route maps: %w", err)
		}

		for protocol, maps := range data {
			if strings.Contains(strings.ToLower(protocol), "bgp") {
				return maps, nil
			}
			for name, routeMap := range maps {
				routeMaps[name] = routeMap
			}
		}
	}

	return routeMaps, nil
}

func FindRouteMap(ctx context.Context, name string) (RouteMap, error) {
	// we get all route-maps and then filter in the code, because in older versions (< FRR 8.5)
	// 'show route-map <name> json' does not work
	routeMaps, err := RouteMaps(ctx)
	if err != nil {
		return RouteMap{}, err
	}

	routeMap, exists := routeMaps[name]
	if !exists {
		return RouteMap{}, fmt.Errorf("route map %s not found", name)
	}

	return routeMap, nil
}

func RunningBGPConfig(ctx context.Context) (string, error) {
	return vtysh(ctx, "show running-config")
}

func StartupBGPConfig() (string, error) {
	data, err := os.ReadFile(FRRFile)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", FRRFile, err)
	}
	return string(data), nil
}

func SaveBGPConfig() (string, error) {
	return runDetached("vtysh", "-w")
}
