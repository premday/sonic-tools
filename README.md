# SONiC CLI tools

Two small CLI tools to operate Community [SONiC](https://sonic-net.github.io/SONiC/) switches.
Both tools be able to connect to the local Redis instance on `127.0.0.1:6379` and must be run directly on the switch.

## Tools

### premshow

Aggregates information about a given IP address into a single command, replacing several native SONiC commands.

```
premshow ip <address> [--json]
```

For a given IP address it displays:

- **Neighbor** — ARP/NDP entry (MAC address, interface, VLAN)
- **Interface** — interfaces whose subnet contains the IP, with address, description, and LLDP neighbor
- **Routing** — matching routes with next-hops

The `--json` flag outputs the result as JSON instead of a formatted table.

### premconfig

Manages switch configuration.

```
# Set an interface description manually
premconfig interface description <intf> <description>

# Set interface descriptions automatically from LLDP neighbor data
premconfig interface auto-description <intf|all> [prefix]
```

The `auto-description` command reads LLDP neighbor data and writes the remote hostname (and port) as the interface description.
Passing `all` instead of an interface name applies the update to every interface.
An optional prefix is prepended to every generated description.

Both subcommands accept a `--dry-run` flag to preview changes without applying them.
`auto-description` also accepts `-v` / `--verbose` to include skipped interfaces in the output.
