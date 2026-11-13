package view

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/premday/sonic-tools/sonic"

	"github.com/goccy/go-yaml"
)

// Device renders every section of the device state.
func Device(device sonic.Device) string {
	sections := []string{
		Platform(device.Platform),
		Interfaces(device.Interfaces),
		Optics(device.Interfaces),
		PSU(device.PSU),
		Fans(device.Fans),
		Temperatures(device.Temperatures),
		BGPNeighbors(device.BGPNeighbors),
		RouteMaps(device.RouteMaps),
		Containers(device.Containers),
		DHCPRelay(device.DHCPRelayProcesses),
		Users(device.Users),
		SNMP(device.SNMP),
		Sysctls(device.Sysctls),
		IPTables(device.IPTables),
	}

	if len(device.Errors) > 0 {
		sections = append(sections, Header("Collection errors")+Lines(device.Errors))
	}

	return strings.Join(sections, "\n")
}

func Platform(platform sonic.Platform) string {
	return Header("Platform") + NewTableOf(
		"Hostname", platform.Hostname,
		"Platform", platform.Name,
		"HwSKU", platform.HwSKU,
		"MAC", platform.MAC,
		"Role", platform.Role,
		"AS number", strconv.Itoa(platform.ASN),
		"Serial", platform.Serial,
		"Build", platform.Version.Build,
		"Kernel", platform.Version.Kernel,
		"ASIC", platform.Version.ASIC,
	).String()
}

func Interfaces(interfaces []sonic.Interface) string {
	if len(interfaces) == 0 {
		return Header("Interfaces") + Comment("No interface found")
	}

	t := NewTable(
		"Interface", "Alias", "Admin", "Oper", "Speed", "MTU", "Description", "Optic", "Errors",
		"LLDP host", "LLDP port", "LLDP port descr",
	)
	for _, intf := range interfaces {
		t.Row(
			intf.Name,
			intf.Alias,
			intf.AdminStatus,
			intf.OperStatus,
			speed(intf.Speed),
			strconv.Itoa(intf.MTU),
			intf.Description,
			intf.Optic.Type,
			errors(intf.Counters),
			intf.Neighbor.Host,
			intf.Neighbor.Port,
			intf.Neighbor.PortDescription,
		)
	}

	return Header("Interfaces") + t.String()
}

// Interface renders a single interface, with its transceiver and counters.
// The transceiver rows are left out when the cage is empty, and the measured ones
// when it holds a cable which reports no DOM value.
func Interface(intf sonic.Interface) string {
	details := slices.Concat([]string{
		"Interface", intf.Name,
		"Alias", intf.Alias,
		"Description", intf.Description,
		"Admin status", intf.AdminStatus,
		"Oper status", intf.OperStatus,
		"Speed", speed(intf.Speed),
		"MTU", strconv.Itoa(intf.MTU),
		"FEC", intf.FEC,
		"Lanes", intf.Lanes,
		"LLDP host", intf.Neighbor.Host,
		"LLDP port", intf.Neighbor.Port,
		"LLDP port description", intf.Neighbor.PortDescription,
	}, opticRows(intf.Optic))

	out := Header(intf.Name) + NewTableOf(details...).String() + laneTable(intf.Optic)

	counters := NewTableOf(
		"In octets", count(intf.Counters.InOctets),
		"Out octets", count(intf.Counters.OutOctets),
		"In errors", count(intf.Counters.InErrors),
		"Out errors", count(intf.Counters.OutErrors),
		"In discards", count(intf.Counters.InDiscards),
		"Out discards", count(intf.Counters.OutDiscards),
		"In drops", count(intf.Counters.InDrops),
		"Out drops", count(intf.Counters.OutDrops),
		"FEC correctable", count(intf.Counters.FECCorrectable),
		"FEC uncorrectable", count(intf.Counters.FECUncorrectable),
		"FEC symbol errors", count(intf.Counters.FECSymbolErrors),
	)

	return out + counters.String()
}

// Optic renders the transceiver of a single interface.
func Optic(intf sonic.Interface) string {
	if !intf.Optic.Present {
		return Header(intf.Name) + Comment("No transceiver")
	}

	return Header(intf.Name) + NewTableOf(opticRows(intf.Optic)...).String() + laneTable(intf.Optic)
}

// opticRows describes a transceiver, its measures are only listed when it reports DOM values.
func opticRows(optic sonic.Optic) []string {
	if !optic.Present {
		return nil
	}

	identity := []string{
		"Optic", optic.Type,
		"Optic vendor", optic.Manufacturer,
		"Optic model", optic.Model,
		"Optic serial", optic.Serial,
	}
	if len(optic.Lanes) == 0 {
		return identity
	}

	return slices.Concat(identity, []string{
		"Temperature °C", optic.Temperature.String(),
		"Temperature limit", optic.Thresholds.TemperatureHigh.String(),
		"Voltage V", optic.Voltage.String(),
		"Voltage limits", limits(optic.Thresholds.VoltageLow, optic.Thresholds.VoltageHigh),
		"RX power limits", limits(optic.Thresholds.RXPowerLow, optic.Thresholds.RXPowerHigh),
		"TX power limits", limits(optic.Thresholds.TXPowerLow, optic.Thresholds.TXPowerHigh),
	})
}

// laneTable is the per lane measures of a transceiver, empty when it reports none.
func laneTable(optic sonic.Optic) string {
	if len(optic.Lanes) == 0 {
		return ""
	}

	t := NewTable("Lane", "RX dBm", "TX dBm", "Bias mA")
	for _, lane := range optic.Lanes {
		t.Row(strconv.Itoa(lane.Index), lane.RXPower.String(), lane.TXPower.String(), lane.TXBias.String())
	}

	return t.String()
}

// Optics renders the DOM values of the interfaces which have a transceiver reporting them.
func Optics(interfaces []sonic.Interface) string {
	measured := make([]sonic.Interface, 0, len(interfaces))
	for _, intf := range interfaces {
		if intf.Optic.Present && len(intf.Optic.Lanes) > 0 {
			measured = append(measured, intf)
		}
	}

	if len(measured) == 0 {
		return Header("Optics") + Comment("No transceiver reporting DOM values")
	}

	t := NewTable("Interface", "Optic", "Temp °C", "Vcc V", "RX dBm", "TX dBm", "Bias mA", "RX limits", "TX limits")
	for _, intf := range measured {
		optic := intf.Optic
		lanes := len(optic.Lanes)
		rx, tx, bias := make([]string, 0, lanes), make([]string, 0, lanes), make([]string, 0, lanes)
		for _, lane := range optic.Lanes {
			rx = append(rx, lane.RXPower.String())
			tx = append(tx, lane.TXPower.String())
			bias = append(bias, lane.TXBias.String())
		}

		t.Row(
			intf.Name,
			optic.Type,
			optic.Temperature.String(),
			optic.Voltage.String(),
			strings.Join(rx, " "),
			strings.Join(tx, " "),
			strings.Join(bias, " "),
			limits(optic.Thresholds.RXPowerLow, optic.Thresholds.RXPowerHigh),
			limits(optic.Thresholds.TXPowerLow, optic.Thresholds.TXPowerHigh),
		)
	}

	return Header("Optics") + t.String()
}

func BGPNeighbors(peers map[string]sonic.BGPNeighbor) string {
	if len(peers) == 0 {
		return Header("BGP") + Comment("No neighbor found")
	}

	neighbors := NewTable(
		"Neighbor", "Description", "Peer group", "Remote AS", "State", "Uptime", "Received", "Sent",
		"Interface", "Interface alias", "Interface state",
	)
	addrs := slices.Collect(maps.Keys(peers))
	sonic.SortPeers(addrs)

	for _, addr := range addrs {
		neighbor := peers[addr]
		received, sent := 0, 0
		for _, safi := range neighbor.SAFI {
			received += safi.AcceptedPrefixCounter
			sent += safi.SentPrefixCounter
		}

		neighbors.Row(
			addr,
			neighbor.Description,
			neighbor.PeerGroup,
			strconv.Itoa(neighbor.RemoteAs),
			neighbor.BGPState,
			neighbor.Uptime.String(),
			strconv.Itoa(received),
			strconv.Itoa(sent),
			neighbor.LocalInterface,
			neighbor.LocalInterfaceAlias,
			neighbor.LocalInterfaceStatus,
		)
	}

	return Header("BGP") + neighbors.String()
}

// BGPNeighbor renders a single session and its address families.
func BGPNeighbor(addr string, neighbor sonic.BGPNeighbor) string {
	session := NewTableOf(
		"Neighbor", addr,
		"Description", neighbor.Description,
		"Peer group", neighbor.PeerGroup,
		"Remote AS", strconv.Itoa(neighbor.RemoteAs),
		"Local AS", strconv.Itoa(neighbor.LocalAs),
		"Local address", neighbor.LocalAddress.String(),
		"State", neighbor.BGPState,
		"Uptime", neighbor.Uptime.String(),
		"Last reset", neighbor.LastResetReason,
		"Interface", neighbor.LocalInterface,
		"Interface alias", neighbor.LocalInterfaceAlias,
		"Interface state", neighbor.LocalInterfaceStatus,
	)

	families := NewTable("Address family", "Import policy", "Export policy", "Received", "Sent", "Maximum")
	for _, family := range slices.Sorted(maps.Keys(neighbor.SAFI)) {
		safi := neighbor.SAFI[family]
		families.Row(
			family,
			safi.ImportPolicy,
			safi.ExportPolicy,
			strconv.Itoa(safi.AcceptedPrefixCounter),
			strconv.Itoa(safi.SentPrefixCounter),
			strconv.Itoa(safi.PrefixAllowedMax),
		)
	}

	out := Header(addr) + session.String()
	if len(neighbor.SAFI) > 0 {
		out += families.String()
	}

	return out
}

func RouteMaps(routeMaps map[string]sonic.RouteMap) string {
	if len(routeMaps) == 0 {
		return Header("Route maps") + Comment("No route map found")
	}

	t := NewTable("Route map", "Rules", "Invoked")
	for _, name := range slices.Sorted(maps.Keys(routeMaps)) {
		routeMap := routeMaps[name]
		t.Row(name, strconv.Itoa(len(routeMap.Rules)), strconv.Itoa(routeMap.Invoked))
	}

	return Header("Route maps") + t.String()
}

// RouteMap renders the rules of a single route map.
func RouteMap(name string, routeMap sonic.RouteMap) string {
	t := NewTable("Sequence", "Action", "Match", "Set", "Invoked")
	for _, rule := range routeMap.Rules {
		t.Row(
			strconv.Itoa(rule.SequenceNumber),
			rule.Action,
			strings.Join(rule.MatchClauses, ", "),
			strings.Join(rule.SetClauses, ", "),
			strconv.Itoa(rule.Invoked),
		)
	}

	return Header(name) + t.String()
}

func PSU(psus []sonic.PSU) string {
	if len(psus) == 0 {
		return Header("PSU") + Comment("No PSU found")
	}

	t := NewTable("PSU", "Presence", "Status", "LED", "Model", "Serial", "Voltage", "Current", "Power")
	for _, psu := range psus {
		t.Row(
			psu.Name,
			psu.Presence.String(),
			psu.Status.String(),
			psu.LedStatus,
			psu.Model,
			psu.Serial,
			psu.Voltage.String(),
			psu.Current.String(),
			psu.Power.String(),
		)
	}

	return Header("PSU") + t.String()
}

func Fans(fans []sonic.Fan) string {
	if len(fans) == 0 {
		return Header("Fans") + Comment("No fan found")
	}

	t := NewTable("Fan", "Drawer", "Presence", "Status", "LED", "Speed", "Direction")
	for _, fan := range fans {
		t.Row(fan.Name, fan.DrawerName, fan.Presence.String(), fan.Status.String(), fan.LedStatus, fan.Speed.String(), fan.Direction)
	}

	return Header("Fans") + t.String()
}

func Temperatures(temperatures []sonic.Temperature) string {
	if len(temperatures) == 0 {
		return Header("Temperatures") + Comment("No sensor found")
	}

	t := NewTable("Sensor", "Temperature", "High threshold", "Critical threshold", "Status")
	for _, temperature := range temperatures {
		status := "N/A"
		if temperature.Warning != nil {
			status = "OK"
			if *temperature.Warning {
				status = "warning"
			}
		}

		t.Row(
			temperature.Name,
			temperature.Temperature.String(),
			temperature.HighThreshold.String(),
			temperature.CriticalThreshold.String(),
			status,
		)
	}

	return Header("Temperatures") + t.String()
}

func Containers(containers []sonic.Container) string {
	if len(containers) == 0 {
		return Header("Containers") + Comment("No container found")
	}

	t := NewTable("Container", "Image", "State", "Status")
	for _, container := range containers {
		t.Row(container.Name, container.Image, container.State, container.Status)
	}

	return Header("Containers") + t.String()
}

func DHCPRelay(processes int) string {
	return Header("DHCP relay") + NewTableOf("Relay processes", strconv.Itoa(processes)).String()
}

func Users(users []string) string {
	return Header("Users") + Lines(users)
}

func SNMP(config map[string]any) string {
	out, err := yaml.Marshal(config)
	if err != nil {
		return Header("SNMP") + Comment(fmt.Sprintf("failed to render configuration: %s", err))
	}
	return Header("SNMP") + Lines(strings.Split(strings.TrimSpace(string(out)), "\n"))
}

func Sysctls(sysctls map[string]string) string {
	if len(sysctls) == 0 {
		return Header("Sysctl") + Comment("No setting found")
	}

	t := NewTable("Setting", "Value")
	for _, key := range slices.Sorted(maps.Keys(sysctls)) {
		t.Row(key, sysctls[key])
	}
	return Header("Sysctl") + t.String()
}

func IPTables(rules sonic.IPTables) string {
	return ipTables("iptables", rules.IPv4) + "\n" + ipTables("ip6tables", rules.IPv6)
}

func ipTables(name string, filter sonic.IPTablesFilter) string {
	if len(filter.Policies) == 0 && len(filter.Rules) == 0 {
		return Header(name) + Comment("No rule found")
	}

	out := Header(name)

	if len(filter.Policies) > 0 {
		policies := NewTable("Chain", "Policy")
		for _, chain := range slices.Sorted(maps.Keys(filter.Policies)) {
			policies.Row(chain, filter.Policies[chain])
		}
		out += policies.String()
	}

	if len(filter.Rules) > 0 {
		rules := NewTable("Chain", "Protocol", "Source", "Destination", "Port", "In", "Out", "Target")
		for _, rule := range filter.Rules {
			rules.Row(
				rule.Chain,
				rule.Protocol,
				rule.Source,
				rule.Destination,
				rule.DestPort,
				rule.InInterface,
				rule.OutInterface,
				rule.Target,
			)
		}
		out += rules.String()
	}

	return out
}

// Lines renders a list of values, one per line.
func Lines(values []string) string {
	if len(values) == 0 {
		return Comment("None")
	}

	var buf strings.Builder
	for _, value := range values {
		buf.WriteString(Comment(value))
	}

	return buf.String()
}

// NewTableOf returns a two columns table from alternating names and values.
func NewTableOf(pairs ...string) *Table {
	t := NewTable("Field", "Value")
	for i := 0; i+1 < len(pairs); i += 2 {
		t.Row(pairs[i], pairs[i+1])
	}
	return t
}

func limits(low, high sonic.NAFloat) string {
	if low.Value == nil && high.Value == nil {
		return ""
	}
	return fmt.Sprintf("%s .. %s", low, high)
}

// count renders a counter, empty when the ASIC does not publish it.
func count(value *uint64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatUint(*value, 10)
}

// errors sums the counters which report a physical problem, it is empty when none is published.
func errors(counters sonic.Counters) string {
	published := []*uint64{counters.InErrors, counters.OutErrors, counters.FECUncorrectable}

	total, reported := uint64(0), false
	for _, counter := range published {
		if counter != nil {
			total, reported = total+*counter, true
		}
	}
	if !reported {
		return ""
	}

	return strconv.FormatUint(total, 10)
}

func speed(mbps int) string {
	if mbps == 0 {
		return ""
	}
	if mbps%1000 == 0 {
		return fmt.Sprintf("%dG", mbps/1000)
	}
	return fmt.Sprintf("%dM", mbps)
}
