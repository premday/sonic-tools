# Test fixtures

Anonymized dumps of two real L3-RA, used by the tests of the `sonic` package:

| Directory  | Device                | Image             | Why both |
| ---------- | --------------------- | ----------------- | -------- |
| `msn2700/` | NVIDIA MSN2700        | 202211, FRR 8     | route-maps keyed `BGP`, `hostForeign` absent while a session is down, no FEC counter |
| `w6510/`   | Micas M2-W6510-32C    | 202505, FRR 10    | route-maps keyed `bgpd`, `hostForeign` reported as `"Unknown"`, transceivers without DOM |

`lldp.json`, `lldp_full.json`, `lldp_lab.json` and `showiproute*.txt` at the root are older
hand-written fixtures, they are not tied to a platform.

## 1. Collect on the device

```bash
d=/tmp/fixtures/$(hostname -s); mkdir -p "$d"

vtysh -c "show bgp neighbors json"      > "$d/bgp_neighbors.json"
vtysh -c "show route-map json"          > "$d/route_map.json"
lldpctl -f json                         > "$d/lldpctl.json"
psushow -s --json                       > "$d/psushow.json"
docker ps --all --format '{{json .}}'   > "$d/docker_ps.jsonl"
docker top dhcp_relay -e                > "$d/docker_top_dhcp_relay.txt"
sudo iptables -S                        > "$d/iptables.txt"
sudo ip6tables -S                       > "$d/ip6tables.txt"
decode-syseeprom -s                     > "$d/serial.txt"
cp /etc/sonic/sonic_version.yml "$d/"

for table in 'DEVICE_METADATA|*' 'PORT|*' 'DEVICE_NEIGHBOR|*' 'SNMP|*' 'SNMP_COMMUNITY|*'; do
  sonic-db-dump -n CONFIG_DB -k "$table"
done                                    # one file per table, see the names below
sonic-db-dump -n APPL_DB     -k 'PORT_TABLE:*'                > "$d/appldb_port_table.json"
sonic-db-dump -n APPL_DB     -k 'INTF_TABLE:*'                > "$d/appldb_intf_table.json"
sonic-db-dump -n APPL_DB     -k 'VLAN_TABLE:*'                > "$d/appldb_vlan_table.json"
sonic-db-dump -n STATE_DB    -k 'TRANSCEIVER_INFO|*'          > "$d/statedb_transceiver_info.json"
sonic-db-dump -n STATE_DB    -k 'TRANSCEIVER_DOM_SENSOR|*'    > "$d/statedb_transceiver_dom_sensor.json"
sonic-db-dump -n STATE_DB    -k 'TRANSCEIVER_DOM_THRESHOLD|*' > "$d/statedb_transceiver_dom_threshold.json"
sonic-db-dump -n STATE_DB    -k 'FAN_INFO|*'                  > "$d/statedb_fan_info.json"
sonic-db-dump -n STATE_DB    -k 'TEMPERATURE_INFO|*'          > "$d/statedb_temperature_info.json"
sonic-db-dump -n COUNTERS_DB -k 'COUNTERS_PORT_NAME_MAP'      > "$d/countersdb_port_name_map.json"
sonic-db-dump -n COUNTERS_DB -k 'COUNTERS_LAG_NAME_MAP'       > "$d/countersdb_lag_name_map.json"

# the counters of one port: 'COUNTERS:oid:*' also holds the queue and priority group ones
oid=$(sonic-db-cli COUNTERS_DB hget COUNTERS_PORT_NAME_MAP Ethernet0)
sonic-db-dump -n COUNTERS_DB -k "COUNTERS:$oid"               > "$d/countersdb_counters.json"
```

The CONFIG_DB files are named `configdb_device_metadata.json`, `configdb_port.json`,
`configdb_device_neighbor.json`, `configdb_snmp.json` and `configdb_snmp_community.json`. `sonic-db-dump` writes `{"<key>": {"type", "value", "ttl",
"expireat"}}`, which the tests load into an in-memory Redis, one file per database
(`dumpDatabases` in `redis_test.go`).

## 2. Trim

Keep six ports per device, chosen for what they exercise rather than for coverage of the chassis:

- an uplink that is up, with a 100G optic reporting DOM, and an established BGP session
- a second uplink, on the msn2700 one whose LLDP port description points at the wrong device
- two ports facing servers without an LLDP daemon: their chassis is inlined and identified by a MAC
- a transceiver without any DOM value (a DAC), only the w6510 has them
- a port that is administratively up but operationally down, and an empty cage

Trim the other files to match: the `PORT`, `PORT_TABLE`, `DEVICE_NEIGHBOR`, `TRANSCEIVER_*` and
`COUNTERS_PORT_NAME_MAP` entries of those ports, the `INTF_TABLE` entries of those ports plus
`Loopback0`, the LLDP entries of those ports plus `eth0`, the BGP neighbors reachable through one
of the kept subnets plus two which are not, four route-maps in **both** protocol objects, four
containers, six `dhcrelay` processes, eight temperature sensors, all fans.

## 3. Anonymize

Structure is preserved exactly — key names, field names, number of entries, lane counts, DOM
readings, counters. Only identifiers are rewritten, and consistently across every file of both
devices, so that the cross-references still line up (LLDP neighbor ↔ port description ↔ BGP peer
↔ route-map name).

| Data | Replacement |
| --- | --- |
| IPv4 | RFC 5737: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`, last octet preserved |
| management IPv4 | RFC 2544: `198.18.0.0/15`, it needs a /16 |
| IPv6 | RFC 3849: `2001:db8::/32` |
| MAC | RFC 7042: `00:00:5e:00:53:xx` |
| AS numbers | RFC 6996 private range (`4200000001`), **not** RFC 5398, which is 16-bit only: the real ones are 4-byte and that is worth keeping |
| host names | RFC 2606 `example.net`, shape kept: `ra01-15-p02-dc1-pnet.example.net` |
| serial numbers | same length, `SN0000…`, including the ones nested in JSON values |
| SNMP community, contact | `public`, `network` |
| commit id, builder | generic, the release number (`202211`) is kept, it is what the code branches on |

Addresses embedded in identifiers need the same mapping: FRR names a per-peer route-map
`RM-SERVER_10_216_144_2-IN`, which has to become `RM-SERVER_192_0_2_2-IN`.

The script doing this is deliberately **not** in the repository: it holds the production prefixes,
AS numbers and host names in clear, which is exactly what must stay out of git.

## 4. Verify before committing

```bash
grep -rniE 'crto\.io|<real prefixes>|<real ASNs>|<real serials>' sonic/test_data
go test ./sonic/ ./internal/view/
```

The first command must print nothing. `configdb_snmp_community.json` and `iptables -S` are the two files that carry
secrets — a community string and the management prefixes — so redact them on the device if you
prefer they never transit.

Several tests assert counts taken from these fixtures and have to be updated together with them:
`TestInterfaces` (6 ports, 5 transceivers, 5 and 3 with DOM), `TestInterfaceOIDs` (6),
`TestInterfaceNeighbors` (5), `TestTemperatureStatus` (8), `TestCountRelayProcesses` (6),
`TestParseContainers` (4), and the values checked by `TestFindInterface`, `TestInterfaceOptic`,
`TestBGPNeighbors`, `TestParseRouteMaps` and `TestResolveLocalInterfaces`.
