package sonic

import (
	"encoding/json"
	"maps"
	"net/netip"
	"regexp"
	"slices"
	"testing"
)

func TestBGPNeighbors(t *testing.T) {
	tests := map[string]struct {
		platform  string
		peer      string
		state     string
		remoteAs  int
		hasUptime bool
		address   string
		accepted  int
		policy    string
		// the connection counters and the last reset are reported per session, not per family
		established int
		dropped     int
		lastReset   string
	}{
		// FRR omits hostForeign entirely while the session is down (SONiC 202211)
		"down without address": {
			platform: "msn2700", peer: "192.0.2.2", state: "Active", remoteAs: 4200000002,
			policy: "RM-SERVER_192_0_2_2-IN", lastReset: "Waiting for NHT",
		},
		"established v4": {
			platform: "msn2700", peer: "203.0.113.1", state: "Established", remoteAs: 4200000003,
			hasUptime: true, address: "203.0.113.1", accepted: 4732, policy: "RM-CLOS-IN",
			established: 1, lastReset: "Waiting for peer OPEN",
		},
		"established v6": {
			platform: "msn2700", peer: "2001:db8:0:fe:f15:0:201:101", state: "Established", remoteAs: 4200000003,
			hasUptime: true, address: "2001:db8:0:fe:f15:0:201:101", accepted: 1427, policy: "RM-CLOS-IN",
			established: 1, lastReset: "Waiting for peer OPEN",
		},
		// FRR reports "Unknown" instead of an address on this release (SONiC 202505)
		"down with unknown address": {
			platform: "w6510", peer: "198.51.100.2", state: "Active", remoteAs: 4200000002,
			policy: "RM-SERVER_198_51_100_2-IN", lastReset: "No path to specified Neighbor",
		},
		"established on the other platform": {
			platform: "w6510", peer: "203.0.113.145", state: "Established", remoteAs: 4200000003,
			hasUptime: true, address: "203.0.113.145", accepted: 4591, policy: "RM-CLOS-IN",
			established: 2, dropped: 1, lastReset: "Waiting for peer OPEN",
		},
	}

	for name, test := range tests {
		neighbors := map[string]BGPNeighbor{}
		fixtureJSON(t, test.platform, "bgp_neighbors.json", &neighbors)

		neighbor, exists := neighbors[test.peer]
		if !exists {
			t.Fatalf("%s: neighbor %s not found", name, test.peer)
		}

		if neighbor.BGPState != test.state {
			t.Errorf("%s: wrong state, want: %s, got: %s", name, test.state, neighbor.BGPState)
		}
		if neighbor.RemoteAs != test.remoteAs {
			t.Errorf("%s: wrong remote AS, want: %d, got: %d", name, test.remoteAs, neighbor.RemoteAs)
		}
		if !neighbor.IsEBGP() {
			t.Errorf("%s: session with AS %d should be external", name, neighbor.RemoteAs)
		}
		if test.address == "" && neighbor.RemoteAddress.IsValid() {
			t.Errorf("%s: address should be empty, got: %q", name, neighbor.RemoteAddress)
		}
		if test.address != "" && neighbor.RemoteAddress.String() != test.address {
			t.Errorf("%s: wrong address, want: %q, got: %q", name, test.address, neighbor.RemoteAddress)
		}
		if neighbor.ConnectionsEstablished != test.established || neighbor.ConnectionsDropped != test.dropped {
			t.Errorf("%s: want %d connections established and %d dropped, got %d and %d", name,
				test.established, test.dropped, neighbor.ConnectionsEstablished, neighbor.ConnectionsDropped)
		}
		if neighbor.LastResetReason != test.lastReset {
			t.Errorf("%s: wrong last reset, want: %q, got: %q", name, test.lastReset, neighbor.LastResetReason)
		}
		if hasUptime := neighbor.Uptime.Seconds() > 0; hasUptime != test.hasUptime {
			t.Errorf("%s: uptime %s, want any uptime: %t", name, neighbor.Uptime, test.hasUptime)
		}

		accepted, policy := 0, ""
		for _, safi := range neighbor.SAFI {
			accepted += safi.AcceptedPrefixCounter
			policy = safi.ImportPolicy
		}
		if accepted != test.accepted {
			t.Errorf("%s: wrong accepted prefixes, want: %d, got: %d", name, test.accepted, accepted)
		}
		if policy != test.policy {
			t.Errorf("%s: wrong import policy, want: %s, got: %s", name, test.policy, policy)
		}
	}
}

func TestBGPNeighborIsEBGP(t *testing.T) {
	tests := map[string]struct {
		neighbor BGPNeighbor
		want     bool
	}{
		"external":         {BGPNeighbor{RemoteAs: 4200000002, LocalAs: 4200000001}, true},
		"internal":         {BGPNeighbor{RemoteAs: 4200000001, LocalAs: 4200000001}, false},
		"unknown remote":   {BGPNeighbor{LocalAs: 4200000001}, false},
		"unknown local":    {BGPNeighbor{RemoteAs: 4200000002}, false},
		"nothing reported": {BGPNeighbor{}, false},
	}

	for name, test := range tests {
		if got := test.neighbor.IsEBGP(); got != test.want {
			t.Errorf("%s: want: %t, got: %t", name, test.want, got)
		}
	}
}

func TestBGPNeighborsMarshal(t *testing.T) {
	for _, platform := range platforms {
		neighbors := map[string]BGPNeighbor{}
		fixtureJSON(t, platform, "bgp_neighbors.json", &neighbors)

		if _, err := json.Marshal(neighbors); err != nil {
			t.Fatalf("%s: failed to marshal neighbors back: %s", platform, err)
		}
	}
}

func TestSnakeCase(t *testing.T) {
	tests := map[string]string{
		"ipv4Unicast":        "ipv4_unicast",
		"ipv6Unicast":        "ipv6_unicast",
		"ipv4Multicast":      "ipv4_multicast",
		"ipv4LabeledUnicast": "ipv4_labeled_unicast",
		"l2VpnEvpn":          "l2_vpn_evpn",
		"l2VPNEvpn":          "l2_vpn_evpn",  // a run of capitals is one word
		"ipv4_unicast":       "ipv4_unicast", // converting twice changes nothing
		"":                   "",
	}

	for name, want := range tests {
		if got := snakeCase(name); got != want {
			t.Errorf("%q: want: %q, got: %q", name, want, got)
		}
	}
}

// TestBGPAddressFamiliesAreSnakeCase covers what TestJSONTagsAreSnakeCase cannot see: the
// families are map keys, so they are renamed while parsing FRR rather than by a struct tag.
func TestBGPAddressFamiliesAreSnakeCase(t *testing.T) {
	pattern := regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)

	for _, platform := range platforms {
		neighbors := map[string]BGPNeighbor{}
		fixtureJSON(t, platform, "bgp_neighbors.json", &neighbors)

		families := map[string]bool{}
		for peer, neighbor := range neighbors {
			for family := range neighbor.SAFI {
				if !pattern.MatchString(family) {
					t.Errorf("%s: neighbor %s publishes the family %q, want snake_case", platform, peer, family)
				}
				families[family] = true
			}
		}

		// the renaming must keep the entries, both devices report the two unicast families
		for _, want := range []string{"ipv4_unicast", "ipv6_unicast"} {
			if !families[want] {
				t.Errorf("%s: no session publishes %s, got: %v", platform, want, slices.Sorted(maps.Keys(families)))
			}
		}
	}
}

func TestParseRouteMaps(t *testing.T) {
	// the route-maps of bgpd must win over the ones of zebra, which never has a hit counter
	wantInvoked := map[string]int{"msn2700": 115309, "w6510": 18614}

	for _, platform := range platforms {
		routeMaps, err := parseRouteMaps(fixture(t, platform, "route_map.json"))
		if err != nil {
			t.Fatalf("%s: %s", platform, err)
		}

		clos, exists := routeMaps["RM-CLOS-IN"]
		if !exists {
			t.Fatalf("%s: RM-CLOS-IN not found in %v", platform, routeMaps)
		}
		if clos.Invoked != wantInvoked[platform] {
			t.Errorf("%s: wrong invoked counter, want: %d, got: %d", platform, wantInvoked[platform], clos.Invoked)
		}
		if len(clos.Rules) != 6 {
			t.Errorf("%s: wrong number of rules, want: 6, got: %d", platform, len(clos.Rules))
		}

		rule := clos.Rules[0]
		if rule.SequenceNumber != 10 || rule.Type != "permit" || rule.Action != "Continue to next entry" {
			t.Errorf("%s: wrong first rule: %+v", platform, rule)
		}
		if len(rule.MatchClauses) != 1 || rule.MatchClauses[0] != "ipv6 address prefix-list PF-ANY_IPV6" {
			t.Errorf("%s: wrong match clauses: %v", platform, rule.MatchClauses)
		}

		// the outgoing policy is where the set clauses are
		out := routeMaps["RM-CLOS-OUT"]
		if len(out.Rules) != 8 {
			t.Errorf("%s: wrong number of rules in RM-CLOS-OUT, want: 8, got: %d", platform, len(out.Rules))
		}
	}
}

func TestParseRouteMapsWithoutBGP(t *testing.T) {
	// only zebra answered, its route-maps are the fallback
	routeMaps, err := parseRouteMaps(`{"zebra":{"RM-CLOS-IN":{"invoked":0,"rules":[]}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := routeMaps["RM-CLOS-IN"]; !exists {
		t.Fatalf("route-maps of zebra should be used as fallback, got: %v", routeMaps)
	}

	routeMaps, err = parseRouteMaps("")
	if err != nil {
		t.Fatal(err)
	}
	if len(routeMaps) != 0 {
		t.Fatalf("want no route map, got: %v", routeMaps)
	}
}

func TestLocalInterface(t *testing.T) {
	// the VLAN covers the whole server subnet, the /30 of the port is inside it
	addrs := []InterfaceAddr{
		{"Vlan506", netip.MustParsePrefix("192.0.2.0/24")},
		{"Ethernet8", netip.MustParsePrefix("192.0.2.5/30")},
		{"Ethernet0", netip.MustParsePrefix("203.0.113.0/31")},
		{"Ethernet0", netip.MustParsePrefix("2001:db8:0:fe::100/127")},
		{"Loopback0", netip.MustParsePrefix("203.0.113.204/32")},
	}

	tests := map[string]string{
		"192.0.2.6":          "Ethernet8", // inside both subnets, the /30 wins
		"192.0.2.130":        "Vlan506",   // only the VLAN covers it
		"203.0.113.1":        "Ethernet0",
		"2001:db8:0:fe::101": "Ethernet0",
		"198.51.100.1":       "", // no local subnet holds it
		"2001:db8:0:ff::1":   "",
	}

	for peer, want := range tests {
		if got := localInterface(addrs, netip.MustParseAddr(peer)); got != want {
			t.Errorf("%s: want %q, got %q", peer, want, got)
		}
	}

	// the order of the addresses must not matter
	slices.Reverse(addrs)
	if got := localInterface(addrs, netip.MustParseAddr("192.0.2.6")); got != "Ethernet8" {
		t.Errorf("reversed order: want Ethernet8, got %q", got)
	}
}
