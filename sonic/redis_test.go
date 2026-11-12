package sonic

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

// fixtureDump is the format of 'sonic-db-dump': the fields of each key, plus its expiration.
type fixtureDump map[string]struct {
	Type  string            `json:"type"`
	Value map[string]string `json:"value"`
}

// dumpDatabases maps the fixture files of a platform to the database they were dumped from.
var dumpDatabases = map[int][]string{
	APPLDB: {
		"appldb_port_table.json",
		"appldb_intf_table.json",
		"appldb_vlan_table.json",
	},
	COUNTERSDB: {
		"countersdb_port_name_map.json",
		"countersdb_lag_name_map.json",
		"countersdb_counters.json",
	},
	CONFIGDB: {
		"configdb_device_metadata.json",
		"configdb_port.json",
		"configdb_device_neighbor.json",
		"configdb_snmp.json",
		"configdb_snmp_community.json",
	},
	STATEDB: {
		"statedb_transceiver_info.json",
		"statedb_transceiver_dom_sensor.json",
		"statedb_transceiver_dom_threshold.json",
		"statedb_fan_info.json",
		"statedb_temperature_info.json",
	},
}

// deviceLLDP is the lldpctl output of a platform, as Interfaces takes it as a parameter.
func deviceLLDP(t *testing.T, platform string) LLDP {
	t.Helper()

	lldp := LLDP{}
	fixtureJSON(t, platform, "lldpctl.json", &lldp)

	return lldp
}

// deviceRedis loads the redis fixtures of a platform into an in-memory server,
// so that the collection runs against the databases of a real device.
func deviceRedis(t *testing.T, platform string) *redis.Client {
	t.Helper()

	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { rdb.Close() })

	for db, files := range dumpDatabases {
		server.Select(db)

		for _, name := range files {
			content, err := os.ReadFile(filepath.Join("test_data", platform, name))
			if err != nil {
				t.Fatal(err)
			}

			dump := fixtureDump{}
			if err := json.Unmarshal(content, &dump); err != nil {
				t.Fatalf("%s/%s: %s", platform, name, err)
			}

			for key, entry := range dump {
				for field, value := range entry.Value {
					server.HSet(key, field, value)
				}
			}
		}
	}

	return rdb
}

func TestInterfaces(t *testing.T) {
	tests := map[string]struct {
		platform string
		names    []string
		optics   int
		withDOM  int
	}{
		"msn2700": {
			platform: "msn2700",
			names:    []string{"Ethernet0", "Ethernet8", "Ethernet9", "Ethernet32", "Ethernet64", "Ethernet100"},
			optics:   5,
			withDOM:  5,
		},
		// this platform reports transceivers without any DOM value, they are direct attach cables
		"w6510": {
			platform: "w6510",
			names:    []string{"Ethernet0", "Ethernet8", "Ethernet10", "Ethernet17", "Ethernet64", "Ethernet96"},
			optics:   5,
			withDOM:  3,
		},
	}

	for name, test := range tests {
		interfaces, err := Interfaces(context.Background(), deviceRedis(t, test.platform), deviceLLDP(t, test.platform))
		if err != nil {
			t.Fatalf("%s: %s", name, err)
		}

		names, optics, withDOM := []string{}, 0, 0
		for _, intf := range interfaces {
			names = append(names, intf.Name)
			if intf.Optic.Present {
				optics++
			}
			if len(intf.Optic.Lanes) > 0 {
				withDOM++
			}
		}

		if strings.Join(names, ",") != strings.Join(test.names, ",") {
			t.Errorf("%s: wrong interfaces,\nwant: %v\ngot:  %v", name, test.names, names)
		}
		if optics != test.optics || withDOM != test.withDOM {
			t.Errorf("%s: want %d transceivers and %d with DOM, got %d and %d",
				name, test.optics, test.withDOM, optics, withDOM)
		}
	}
}

func TestFindInterface(t *testing.T) {
	rdb := deviceRedis(t, "msn2700")

	intf, err := FindInterface(context.Background(), rdb, deviceLLDP(t, "msn2700"), "Ethernet0")
	if err != nil {
		t.Fatal(err)
	}

	if intf.Alias != "etp1" || intf.Speed != 100000 || intf.MTU != 9100 {
		t.Errorf("wrong configuration: %+v", intf.PortConfig)
	}
	if intf.AdminStatus != "up" || intf.OperStatus != "up" {
		t.Errorf("wrong status: admin %s, oper %s", intf.AdminStatus, intf.OperStatus)
	}
	if intf.Description != "LOCAL:sp01-15-p02-dc1-pnet.example.net:Ethernet16" {
		t.Errorf("wrong description: %s", intf.Description)
	}
	if intf.Neighbor.Host != "sp01-15-p02-dc1-pnet.example.net" || intf.Neighbor.Port != "Ethernet16" {
		t.Errorf("wrong LLDP neighbor: %+v", intf.Neighbor)
	}

	// the counters of this port are the ones dumped from the device
	if counter(intf.Counters.InOctets) != "54137380704" || counter(intf.Counters.OutOctets) != "8039200111" {
		t.Errorf("wrong octet counters: in %s, out %s",
			counter(intf.Counters.InOctets), counter(intf.Counters.OutOctets))
	}
	// a published counter at zero means no error, an absent one is null
	if counter(intf.Counters.InErrors) != "0" || counter(intf.Counters.OutErrors) != "0" {
		t.Errorf("wrong error counters: in %s, out %s",
			counter(intf.Counters.InErrors), counter(intf.Counters.OutErrors))
	}
	if intf.Counters.FECUncorrectable != nil {
		t.Errorf("this platform does not publish FEC counters, want null, got: %s",
			counter(intf.Counters.FECUncorrectable))
	}

	if _, err := FindInterface(context.Background(), rdb, LLDP{}, "Ethernet4"); err == nil {
		t.Error("an interface which is not configured should not be found")
	}
}

func TestInterfaceOptic(t *testing.T) {
	tests := map[string]struct {
		platform    string
		intf        string
		present     bool
		lanes       int
		opticType   string
		vendor      string
		temperature float64
		rxPower     float64
		rxPowerLow  float64
	}{
		"100G optic with DOM": {
			platform: "msn2700", intf: "Ethernet0", present: true, lanes: 4,
			opticType: "QSFP28 or later", vendor: "FS", temperature: 44.648,
			rxPower: 0.931, rxPowerLow: -13.002,
		},
		"direct attach cable without DOM": {
			platform: "w6510", intf: "Ethernet10", present: true, lanes: 0,
			opticType: "QSFP28 or later", vendor: "OEM",
		},
		"empty cage": {
			platform: "msn2700", intf: "Ethernet64",
		},
	}

	for name, test := range tests {
		intf, err := FindInterface(context.Background(), deviceRedis(t, test.platform), LLDP{}, test.intf)
		if err != nil {
			t.Fatalf("%s: %s", name, err)
		}
		optic := intf.Optic

		if optic.Present != test.present {
			t.Errorf("%s: want present: %t, got: %t", name, test.present, optic.Present)
		}
		if len(optic.Lanes) != test.lanes {
			t.Errorf("%s: want %d lanes, got %d", name, test.lanes, len(optic.Lanes))
		}
		if optic.Type != test.opticType || optic.Manufacturer != test.vendor {
			t.Errorf("%s: wrong transceiver: %q from %q", name, optic.Type, optic.Manufacturer)
		}

		if test.lanes == 0 {
			// nothing is measured, every value must be null rather than zero
			if optic.Temperature.Value != nil || optic.Voltage.Value != nil {
				t.Errorf("%s: DOM values should be null, got %q / %q", name, optic.Temperature, optic.Voltage)
			}
			continue
		}

		if optic.Temperature.Value == nil || *optic.Temperature.Value != test.temperature {
			t.Errorf("%s: wrong temperature, want: %v, got: %q", name, test.temperature, optic.Temperature)
		}
		if optic.Lanes[0].RXPower.Value == nil || *optic.Lanes[0].RXPower.Value != test.rxPower {
			t.Errorf("%s: wrong RX power, want: %v, got: %q", name, test.rxPower, optic.Lanes[0].RXPower)
		}
		if optic.Thresholds.RXPowerLow.Value == nil || *optic.Thresholds.RXPowerLow.Value != test.rxPowerLow {
			t.Errorf("%s: wrong RX power alarm, want: %v, got: %q", name, test.rxPowerLow, optic.Thresholds.RXPowerLow)
		}
		if optic.Lanes[0].TXBias.Value == nil {
			t.Errorf("%s: the bias of the first lane should be reported", name)
		}
	}
}

func TestOperStatus(t *testing.T) {
	ctx := context.Background()
	appl, err := openDB(ctx, deviceRedis(t, "msn2700"), APPLDB)
	if err != nil {
		t.Fatal(err)
	}
	defer appl.Close()

	tests := map[string]string{
		"Ethernet0":   "up",   // PORT_TABLE
		"Ethernet64":  "down", // PORT_TABLE
		"Vlan506":     "",     // VLAN interfaces do not report a status
		"PortChannel": "",     // not in any table
	}

	for intf, want := range tests {
		status, err := operStatus(ctx, appl, intf)
		if err != nil {
			t.Fatalf("%s: %s", intf, err)
		}
		if status != want {
			t.Errorf("%s: want status %q, got %q", intf, want, status)
		}
	}
}

func TestInterfacesAddrs(t *testing.T) {
	addrs, err := InterfacesAddrs(context.Background(), deviceRedis(t, "msn2700"))
	if err != nil {
		t.Fatal(err)
	}

	// the fixture holds v4 and v6 addresses, and interfaces without any address
	want := map[string]int{"Ethernet0": 2, "Ethernet8": 1, "Ethernet9": 1, "Ethernet32": 2, "Loopback0": 2}
	got := map[string]int{}
	for _, addr := range addrs {
		got[addr.Name]++
	}

	for name, count := range want {
		if got[name] != count {
			t.Errorf("%s: want %d addresses, got %d (%v)", name, count, got[name], addrs)
		}
	}

	prefixes, err := IPInterface(context.Background(), deviceRedis(t, "msn2700"), "Ethernet0")
	if err != nil {
		t.Fatal(err)
	}
	if len(prefixes) != 2 {
		t.Errorf("want 2 prefixes on Ethernet0, got %v", prefixes)
	}
}

func TestPlatformInfo(t *testing.T) {
	tests := map[string]Platform{
		"msn2700": {
			Hostname: "ra01-15-p02-dc1-pnet.example.net", HwSKU: "ACS-MSN2700",
			Name: "x86_64-mlnx_msn2700-r0", MAC: "00:00:5e:00:53:00", ASN: 4200000001, Role: "ToRRouter",
		},
		"w6510": {
			Hostname: "rb01-15-p02-dc1-pnet.example.net", HwSKU: "M2-W6510-32C",
			Name: "x86_64-micas_m2-w6510-32c-r0", MAC: "00:00:5e:00:53:08", ASN: 4200000001, Role: "ToRRouter",
		},
	}

	for platform, want := range tests {
		// the version file and the EEPROM are not readable outside of a switch
		got, _ := PlatformInfo(context.Background(), deviceRedis(t, platform))
		got.Version, got.Serial = Version{}, ""

		if got != want {
			t.Errorf("%s: wrong platform,\nwant: %+v\ngot:  %+v", platform, want, got)
		}
	}
}

func TestFanStatus(t *testing.T) {
	tests := map[string][]string{
		"msn2700": {"fan1", "fan2", "fan3", "fan4", "fan5", "fan6", "fan7", "fan8", "psu1_fan1", "psu2_fan1"},
		"w6510": {
			"Fantray1_1", "Fantray1_2", "Fantray2_1", "Fantray2_2", "Fantray3_1", "Fantray3_2",
			"Fantray4_1", "Fantray4_2", "Fantray5_1", "Fantray5_2", "PSU1_FAN1", "PSU2_FAN1",
		},
	}

	for platform, want := range tests {
		fans, err := FanStatus(context.Background(), deviceRedis(t, platform))
		if err != nil {
			t.Fatalf("%s: %s", platform, err)
		}

		names := []string{}
		for _, fan := range fans {
			names = append(names, fan.Name)

			// 'True' and 'False' are the values written by the platform daemons
			if fan.Presence.String() != "OK" || fan.Status.String() != "OK" {
				t.Errorf("%s: %s should be present and OK, got: %s / %s",
					platform, fan.Name, fan.Presence, fan.Status)
			}
			if fan.Speed.Value == nil || fan.DrawerName == "" {
				t.Errorf("%s: incomplete fan: %+v", platform, fan)
			}
		}

		if strings.Join(names, ",") != strings.Join(want, ",") {
			t.Errorf("%s: wrong fans,\nwant: %v\ngot:  %v", platform, want, names)
		}
	}
}

func TestTemperatureStatus(t *testing.T) {
	for _, platform := range platforms {
		temperatures, err := TemperatureStatus(context.Background(), deviceRedis(t, platform))
		if err != nil {
			t.Fatalf("%s: %s", platform, err)
		}
		if len(temperatures) != 8 {
			t.Fatalf("%s: want 8 sensors, got %d", platform, len(temperatures))
		}

		for _, sensor := range temperatures {
			// SONiC reports 'False' when the sensor is below its threshold
			if sensor.Warning == nil {
				t.Errorf("%s: %s should report a warning status", platform, sensor.Name)
				continue
			}
			if *sensor.Warning {
				t.Errorf("%s: %s should not be in warning: %+v", platform, sensor.Name, sensor)
			}
		}
	}
}

func TestInterfaceNeighbors(t *testing.T) {
	neighbors, err := InterfaceNeighbors(context.Background(), deviceRedis(t, "msn2700"))
	if err != nil {
		t.Fatal(err)
	}

	// the 'name' of the table is the whole peer identity, as configured by the generator
	if neighbors["Ethernet0"] != "LOCAL:sp01-15-p02-dc1-pnet.example.net:Ethernet16" {
		t.Errorf("wrong expected neighbor: %v", neighbors)
	}
	if neighbors["Ethernet8"] != "SERVER:server1:eth0" {
		t.Errorf("wrong expected neighbor of a server port: %v", neighbors)
	}
	// Ethernet64 is not cabled, it has no entry
	if len(neighbors) != 5 {
		t.Errorf("want 5 configured neighbors, got %d: %v", len(neighbors), neighbors)
	}
}

func TestResolveLocalInterfaces(t *testing.T) {
	tests := map[string]struct {
		platform  string
		peer      string
		intf      string
		alias     string
		operState string
	}{
		"established uplink": {
			platform: "msn2700", peer: "203.0.113.1", intf: "Ethernet0", alias: "etp1", operState: "up",
		},
		"uplink v6 session": {
			platform: "msn2700", peer: "2001:db8:0:fe:f15:0:201:101", intf: "Ethernet0", alias: "etp1", operState: "up",
		},
		// a breakout port, its alias carries the lane suffix
		"server on a /30": {
			platform: "msn2700", peer: "192.0.2.6", intf: "Ethernet8", alias: "etp3a", operState: "up",
		},
		// no local address covers this peer: the session is not expected on any interface
		"peer out of every subnet": {
			platform: "msn2700", peer: "192.0.2.14",
		},
		"uplink of the other platform": {
			platform: "w6510", peer: "203.0.113.97", intf: "Ethernet64", alias: "etp17", operState: "up",
		},
	}

	for name, test := range tests {
		neighbors := map[string]BGPNeighbor{}
		fixtureJSON(t, test.platform, "bgp_neighbors.json", &neighbors)

		if err := resolveLocalInterfaces(context.Background(), deviceRedis(t, test.platform), neighbors); err != nil {
			t.Fatalf("%s: %s", name, err)
		}

		neighbor := neighbors[test.peer]
		if neighbor.LocalInterface != test.intf {
			t.Errorf("%s: wrong interface, want: %q, got: %q", name, test.intf, neighbor.LocalInterface)
		}
		if neighbor.LocalInterfaceAlias != test.alias {
			t.Errorf("%s: wrong alias, want: %q, got: %q", name, test.alias, neighbor.LocalInterfaceAlias)
		}
		if neighbor.LocalInterfaceStatus != test.operState {
			t.Errorf("%s: wrong interface state, want: %q, got: %q", name, test.operState, neighbor.LocalInterfaceStatus)
		}
	}
}

func TestSNMPConfig(t *testing.T) {
	want := map[string]any{
		"SNMP": map[string]map[string]string{
			"CONTACT":  {"Contact": "network"},
			"LOCATION": {"Location": "dc1-01.15"},
		},
		"SNMP_COMMUNITY": map[string]map[string]string{
			"public": {"TYPE": "RO"},
		},
	}

	for _, platform := range platforms {
		config, err := SNMPConfig(context.Background(), deviceRedis(t, platform))
		if err != nil {
			t.Fatalf("%s: %s", platform, err)
		}
		if !reflect.DeepEqual(config, want) {
			t.Errorf("%s: wrong SNMP configuration,\nwant: %v\ngot:  %v", platform, want, config)
		}
	}
}

func TestSNMPConfigNotConfigured(t *testing.T) {
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { rdb.Close() })

	config, err := SNMPConfig(context.Background(), rdb)
	if err != nil {
		t.Fatal(err)
	}
	if len(config) != 0 {
		t.Errorf("a table which is not configured should be left out, got: %v", config)
	}
}

// counter formats a counter for the failure messages.
func counter(value *uint64) string {
	if value == nil {
		return "null"
	}
	return strconv.FormatUint(*value, 10)
}

func TestInterfaceOIDs(t *testing.T) {
	ctx := context.Background()

	for _, platform := range platforms {
		counters, err := openDB(ctx, deviceRedis(t, platform), COUNTERSDB)
		if err != nil {
			t.Fatal(err)
		}
		defer counters.Close()

		oids, err := interfaceOIDs(ctx, counters)
		if err != nil {
			t.Fatalf("%s: %s", platform, err)
		}

		// the LAG map of both devices holds a single empty field, it must not become an entry
		if len(oids) != 6 {
			t.Errorf("%s: want the 6 ports of the fixture, got %d: %v", platform, len(oids), oids)
		}
		if _, exists := oids[""]; exists {
			t.Errorf("%s: an interface with no name should not be mapped: %v", platform, oids)
		}
		if !strings.HasPrefix(oids["Ethernet0"], "oid:0x") {
			t.Errorf("%s: wrong OID for Ethernet0: %q", platform, oids["Ethernet0"])
		}
	}
}

func TestTemperatureMeasures(t *testing.T) {
	temperatures, err := TemperatureStatus(context.Background(), deviceRedis(t, "msn2700"))
	if err != nil {
		t.Fatal(err)
	}

	measures := map[string]Temperature{}
	for _, sensor := range temperatures {
		measures[sensor.Name] = sensor
	}

	// a sensor of the ASIC reports everything
	asic := measures["ASIC"]
	if asic.Temperature.Value == nil || *asic.Temperature.Value != 63 {
		t.Errorf("wrong ASIC temperature: %q", asic.Temperature)
	}
	if asic.CriticalThreshold.Value == nil || *asic.CriticalThreshold.Value != 120 {
		t.Errorf("wrong ASIC critical threshold: %q", asic.CriticalThreshold)
	}

	// a cage without transceiver reports 'N/A', which must be null and not zero
	empty := measures["xSFP module 8 Temp"]
	if empty.Temperature.Value != nil {
		t.Errorf("an unread sensor should be null, got: %q", empty.Temperature)
	}

	// the thresholds of the ambient sensors are not reported either
	ambient := measures["Ambient Fan Side Temp"]
	if ambient.Temperature.Value == nil || ambient.HighThreshold.Value != nil {
		t.Errorf("wrong ambient sensor: %+v", ambient)
	}
}

func TestCountersPublishedOrNull(t *testing.T) {
	ctx := context.Background()
	rdb := deviceRedis(t, "msn2700")

	// the fixture only holds the counters of Ethernet0
	withCounters, err := FindInterface(ctx, rdb, LLDP{}, "Ethernet0")
	if err != nil {
		t.Fatal(err)
	}
	if withCounters.Counters.InErrors == nil || *withCounters.Counters.InErrors != 0 {
		t.Errorf("a published counter at zero means no error, got: %s", counter(withCounters.Counters.InErrors))
	}
	// this ASIC does not publish the FEC counters at all
	if withCounters.Counters.FECCorrectable != nil {
		t.Errorf("an unpublished counter should be null, got: %s", counter(withCounters.Counters.FECCorrectable))
	}

	without, err := FindInterface(ctx, rdb, LLDP{}, "Ethernet32")
	if err != nil {
		t.Fatal(err)
	}
	if without.Counters.InOctets != nil {
		t.Errorf("an interface without counters should report null, got: %s", counter(without.Counters.InOctets))
	}
}
