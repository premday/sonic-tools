package view

import (
	"strings"
	"testing"

	"github.com/premday/sonic-tools/sonic"
)

func TestOpticsSkipsInterfacesWithoutDOM(t *testing.T) {
	power, bias := sonic.Measure(-2.53), sonic.Measure(7.48)
	low, high := sonic.Measure(-13.29), sonic.Measure(3.4)
	interfaces := []sonic.Interface{
		{
			Name: "Ethernet0",
			Optic: sonic.Optic{
				Present: true,
				Type:    "QSFP28 100G",
				Lanes: []sonic.OpticLane{
					{Index: 1, RXPower: power, TXPower: power, TXBias: bias},
					{Index: 2, RXPower: power, TXBias: bias},
				},
				Thresholds: sonic.OpticThresholds{RXPowerLow: low, RXPowerHigh: high},
			},
		},
		{Name: "Ethernet8"}, // no transceiver
		{Name: "Ethernet16", Optic: sonic.Optic{Present: true, Type: "QSFP28 100G DAC"}}, // passive cable
	}

	out := Optics(interfaces)

	if !strings.Contains(out, "-2.53 -2.53") {
		t.Fatalf("missing RX power of both lanes:\n%s", out)
	}
	if !strings.Contains(out, "-13.29 .. 3.40") {
		t.Fatalf("missing RX power limits:\n%s", out)
	}
	for _, intf := range []string{"Ethernet8", "Ethernet16"} {
		if strings.Contains(out, intf) {
			t.Fatalf("%s has no DOM value and must not be rendered:\n%s", intf, out)
		}
	}
}

func TestInterfaceWithoutDOM(t *testing.T) {
	power := sonic.Measure(-2.53)
	tests := map[string]struct {
		optic   sonic.Optic
		present []string
		absent  []string
	}{
		"optic reporting DOM": {
			optic: sonic.Optic{
				Present: true, Type: "QSFP28 or later", Manufacturer: "FS",
				Lanes: []sonic.OpticLane{{Index: 1, RXPower: power}},
			},
			present: []string{"Optic vendor", "Temperature °C", "RX dBm"},
		},
		// a direct attach cable is identified but measures nothing
		"cable without DOM": {
			optic:   sonic.Optic{Present: true, Type: "QSFP28 or later", Manufacturer: "OEM"},
			present: []string{"Optic vendor"},
			absent:  []string{"Temperature °C", "RX power limits", "RX dBm", "Bias mA"},
		},
		"empty cage": {
			optic:  sonic.Optic{},
			absent: []string{"Optic vendor", "Optic serial", "Temperature °C", "RX dBm"},
		},
	}

	for name, test := range tests {
		out := Interface(sonic.Interface{Name: "Ethernet0", Optic: test.optic})

		for _, want := range test.present {
			if !strings.Contains(out, want) {
				t.Errorf("%s: %q is missing:\n%s", name, want, out)
			}
		}
		for _, unwanted := range test.absent {
			if strings.Contains(out, unwanted) {
				t.Errorf("%s: %q should not be rendered:\n%s", name, unwanted, out)
			}
		}
	}
}

func TestEmptySections(t *testing.T) {
	// a section which collected nothing says so instead of printing an empty table
	tests := map[string]string{
		"sysctl":       Sysctls(nil),
		"interfaces":   Interfaces(nil),
		"psu":          PSU(nil),
		"fans":         Fans(nil),
		"temperatures": Temperatures(nil),
		"containers":   Containers(nil),
		"bgp":          BGPNeighbors(nil),
		"route maps":   RouteMaps(nil),
		"optics":       Optics(nil),
	}

	for name, out := range tests {
		if strings.Contains(out, "╭") {
			t.Errorf("%s: an empty section should not render a table:\n%s", name, out)
		}
		if len(strings.TrimSpace(out)) == 0 {
			t.Errorf("%s: an empty section should still print its title", name)
		}
	}
}

func TestOpticOfOneInterface(t *testing.T) {
	tests := map[string]struct {
		optic   sonic.Optic
		present []string
		absent  []string
	}{
		"optic reporting DOM": {
			optic: sonic.Optic{
				Present: true, Type: "QSFP28 or later", Manufacturer: "FS",
				Temperature: sonic.Measure(44.648),
				Lanes:       []sonic.OpticLane{{Index: 1, RXPower: sonic.Measure(0.931)}},
			},
			present: []string{"Optic vendor", "Temperature °C", "44.65", "RX dBm", "0.93"},
		},
		"cable without DOM": {
			optic:   sonic.Optic{Present: true, Type: "QSFP28 or later", Manufacturer: "OEM"},
			present: []string{"Optic vendor", "OEM"},
			absent:  []string{"Temperature °C", "RX dBm"},
		},
		"empty cage": {
			absent: []string{"Optic vendor", "RX dBm", "╭"},
		},
	}

	for name, test := range tests {
		out := Optic(sonic.Interface{Name: "Ethernet0", Optic: test.optic})

		if !strings.Contains(out, "Ethernet0") {
			t.Errorf("%s: the interface name is missing:\n%s", name, out)
		}
		for _, want := range test.present {
			if !strings.Contains(out, want) {
				t.Errorf("%s: %q is missing:\n%s", name, want, out)
			}
		}
		for _, unwanted := range test.absent {
			if strings.Contains(out, unwanted) {
				t.Errorf("%s: %q should not be rendered:\n%s", name, unwanted, out)
			}
		}
	}
}

func TestBGPNeighborDetail(t *testing.T) {
	neighbor := sonic.BGPNeighbor{
		RemoteAs: 4200000003, LocalAs: 4200000001, BGPState: "Established", LastResetReason: "Waiting for NHT",
		Description:    "PG-L3_SP:sp01.example.net",
		LocalInterface: "Ethernet0", LocalInterfaceAlias: "etp1", LocalInterfaceStatus: "up",
		SAFI: map[string]sonic.SAFI{
			"ipv4_unicast": {ImportPolicy: "RM-CLOS-IN", AcceptedPrefixCounter: 4732, PrefixAllowedMax: 10000},
			"ipv6_unicast": {ImportPolicy: "RM-CLOS-IN", AcceptedPrefixCounter: 1427},
		},
	}

	out := BGPNeighbor("203.0.113.1", neighbor)
	for _, want := range []string{
		"203.0.113.1", "Established", "4200000003", "Ethernet0", "etp1",
		"ipv4_unicast", "ipv6_unicast", "RM-CLOS-IN", "4732", "1427", "10000", "Waiting for NHT",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q is missing:\n%s", want, out)
		}
	}

	// a session which never came up has no address family
	if out := BGPNeighbor("203.0.113.3", sonic.BGPNeighbor{BGPState: "Active"}); strings.Contains(out, "Address family") {
		t.Errorf("no address family should be rendered:\n%s", out)
	}
}
