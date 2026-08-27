package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/premday/sonic-tools/internal/analyzer"
	"github.com/premday/sonic-tools/internal/view"
	"github.com/premday/sonic-tools/sonic"

	redis "github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
)

const timeout = 120 * time.Second

var jsonFmt bool

// output prints the collected data as JSON, or the rendered text.
func output(data any, rendered string) error {
	if !jsonFmt {
		fmt.Print(rendered)
		return nil
	}

	out, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to convert to JSON: %w", err)
	}
	fmt.Println(string(out))

	return nil
}

// lldpNeighbors is best effort: when lldpd is down the neighbor columns are left empty
// instead of failing the whole command.
func lldpNeighbors(ctx context.Context) sonic.LLDP {
	lldp, err := sonic.LLDPNeighbors(ctx)
	if err != nil {
		log.Println("LLDP is not available:", err)
	}
	return lldp
}

func ipInfo(ctx context.Context, rdb *redis.Client, ip string) error {
	ipAnalyzer, err := analyzer.NewIPAnalyzer(ctx, rdb, ip)
	if err != nil {
		return err
	}

	neighbor := ipAnalyzer.GetNeighborInfo(ctx)
	interfaces := ipAnalyzer.GetInterfaceInfo(ctx)
	routing := ipAnalyzer.GetRoutingInfo(ctx)

	data := struct {
		TargetIP   string                   `json:"target_ip"`
		Neighbor   *analyzer.NeighborInfo   `json:"neighbor,omitempty"`
		Interfaces []analyzer.InterfaceLink `json:"interfaces,omitempty"`
		Routes     []analyzer.Route         `json:"routes,omitempty"`
	}{
		TargetIP:   ip,
		Interfaces: interfaces.Links,
		Routes:     routing.Routes,
	}
	if neighbor.Neighbor.Found {
		data.Neighbor = &neighbor
	}

	sections := []string{neighbor.String(), interfaces.String(), routing.String()}

	return output(data, fmt.Sprintf("\n%s\n", strings.Join(sections, "\n")))
}

func main() {
	if err := run(); err != nil {
		log.Fatalln(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	rdb := sonic.NewRedis()
	defer rdb.Close()

	cobra.EnablePrefixMatching = true

	rootCmd := &cobra.Command{
		Use:           "premshow",
		Short:         "Custom SONiC show CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.PersistentFlags().BoolVar(&jsonFmt, "json", false, "return output in json format")

	deviceGroup := &cobra.Group{ID: "device", Title: "Device state:"}
	infoGroup := &cobra.Group{ID: "info", Title: "Info gathering:"}
	rootCmd.AddGroup(deviceGroup, infoGroup)

	rootCmd.AddCommand(
		&cobra.Command{
			Use:     "all",
			Short:   "Show the whole device state",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				device := sonic.Collect(ctx, rdb)
				return output(device, view.Device(device))
			},
		},
		&cobra.Command{
			Use:     "optical [intf]",
			Short:   "Show the DOM values of the transceivers reporting them",
			GroupID: deviceGroup.ID,
			Args:    cobra.MaximumNArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				if len(args) == 1 {
					intf, err := sonic.FindInterface(ctx, rdb, sonic.LLDP{}, args[0])
					if err != nil {
						return err
					}
					return output(intf.Optic, view.Optic(intf))
				}

				interfaces, err := sonic.Interfaces(ctx, rdb, sonic.LLDP{})
				if err != nil {
					return err
				}

				optics := map[string]sonic.Optic{}
				for _, intf := range interfaces {
					if intf.Optic.Present {
						optics[intf.Name] = intf.Optic
					}
				}

				return output(optics, view.Optics(interfaces))
			},
		},
		&cobra.Command{
			Use:     "interfaces [intf]",
			Short:   "Show interfaces status, transceivers, counters and LLDP neighbors",
			GroupID: deviceGroup.ID,
			Args:    cobra.MaximumNArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				lldp := lldpNeighbors(ctx)

				if len(args) == 1 {
					intf, err := sonic.FindInterface(ctx, rdb, lldp, args[0])
					if err != nil {
						return err
					}
					return output(intf, view.Interface(intf))
				}

				interfaces, err := sonic.Interfaces(ctx, rdb, lldp)
				if err != nil {
					return err
				}
				return output(interfaces, view.Interfaces(interfaces))
			},
		},
		&cobra.Command{
			Use:     "platform",
			Short:   "Show platform, image version and serial number",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				platform, err := sonic.PlatformInfo(ctx, rdb)
				if err != nil {
					return err
				}
				return output(platform, view.Platform(platform))
			},
		},
		&cobra.Command{
			Use:     "bgp [neighbor]",
			Short:   "Show BGP neighbors",
			GroupID: deviceGroup.ID,
			Args:    cobra.MaximumNArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				if len(args) == 1 {
					neighbor, err := sonic.FindBGPNeighbor(ctx, rdb, args[0])
					if err != nil {
						return err
					}
					return output(neighbor, view.BGPNeighbor(args[0], neighbor))
				}

				neighbors, err := sonic.BGPStatus(ctx, rdb)
				if err != nil {
					return err
				}
				return output(neighbors, view.BGPNeighbors(neighbors))
			},
		},
		&cobra.Command{
			Use:     "route-map [name]",
			Short:   "Show BGP route-maps",
			GroupID: deviceGroup.ID,
			Args:    cobra.MaximumNArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				if len(args) == 1 {
					routeMap, err := sonic.FindRouteMap(ctx, args[0])
					if err != nil {
						return err
					}
					return output(routeMap, view.RouteMap(args[0], routeMap))
				}

				routeMaps, err := sonic.RouteMaps(ctx)
				if err != nil {
					return err
				}
				return output(routeMaps, view.RouteMaps(routeMaps))
			},
		},
		&cobra.Command{
			Use:     "psu",
			Short:   "Show power supplies status",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				psu, err := sonic.PSUStatus(ctx)
				if err != nil {
					return err
				}
				return output(psu, view.PSU(psu))
			},
		},
		&cobra.Command{
			Use:     "fan",
			Short:   "Show fans status",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				fans, err := sonic.FanStatus(ctx, rdb)
				if err != nil {
					return err
				}
				return output(fans, view.Fans(fans))
			},
		},
		&cobra.Command{
			Use:     "temperature",
			Short:   "Show temperature sensors",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				temperatures, err := sonic.TemperatureStatus(ctx, rdb)
				if err != nil {
					return err
				}
				return output(temperatures, view.Temperatures(temperatures))
			},
		},
		&cobra.Command{
			Use:     "containers",
			Short:   "Show docker containers status",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				containers, err := sonic.Containers(ctx)
				if err != nil {
					return err
				}
				return output(containers, view.Containers(containers))
			},
		},
		&cobra.Command{
			Use:     "dhcp-relay",
			Short:   "Show the number of relay processes in the dhcp_relay container",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				processes, err := sonic.DHCPRelayProcesses(ctx)
				if err != nil {
					return err
				}
				return output(processes, view.DHCPRelay(processes))
			},
		},
		&cobra.Command{
			Use:     "users",
			Short:   "Show local users",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				users, err := sonic.HumanUsers()
				if err != nil {
					return err
				}
				return output(users, view.Users(users))
			},
		},
		&cobra.Command{
			Use:     "snmp",
			Short:   "Show SNMP configuration",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				config, err := sonic.SNMPConfig()
				if err != nil {
					return err
				}
				return output(config, view.SNMP(config))
			},
		},
		&cobra.Command{
			Use:     "iptables",
			Short:   "Show iptables and ip6tables rules",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				rules, err := sonic.IPTablesRules(ctx)
				if err != nil {
					return err
				}
				return output(rules, view.IPTables(rules))
			},
		},
		&cobra.Command{
			Use:     "sysctl",
			Short:   "Show some kernel settings",
			GroupID: deviceGroup.ID,
			Args:    cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				sysctls := sonic.Sysctls(sonic.SysctlKeys)
				return output(sysctls, view.Sysctls(sysctls))
			},
		},
		&cobra.Command{
			Use:     "ip <address>",
			Short:   "Get aggregated information for the given IP address",
			GroupID: infoGroup.ID,
			Args:    cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return ipInfo(ctx, rdb, args[0])
			},
		},
	)

	return rootCmd.Execute()
}
