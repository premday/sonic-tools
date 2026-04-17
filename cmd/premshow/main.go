package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/premday/sonic-tools/internal/analyzer"

	redis "github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
)

func printIPInfo(ip string, jsonFmt bool) error {
	if ip == "" {
		return errors.New("--ip parameter is mandatory")
	}

	// get info
	rdb := redis.NewClient(&redis.Options{
		Addr:     "127.0.0.1:6379",
		Password: "",
		DB:       0,
	})
	defer rdb.Close()

	ctx := context.Background()
	ipAnalyzer, err := analyzer.NewIPAnalyzer(ctx, rdb, ip)
	if err != nil {
		return err
	}

	neighborInfo := ipAnalyzer.GetNeighborInfo()
	interfaceInfo := ipAnalyzer.GetInterfaceInfo(ctx)
	routingInfo := ipAnalyzer.GetRoutingInfo(ctx)

	// print
	if jsonFmt {
		info := struct {
			TargetIP   string
			Neighbor   *analyzer.NeighborInfo   `json:"Neighbor,omitempty"`
			Interfaces []analyzer.InterfaceLink `json:"Interfaces,omitempty"`
			Routes     []analyzer.Route         `json:"Routes,omitempty"`
		}{
			TargetIP:   ip,
			Interfaces: interfaceInfo.Links,
			Routes:     routingInfo.Routes,
		}
		if neighborInfo.Neighbor.Found {
			info.Neighbor = &neighborInfo
		}

		out, err := json.Marshal(info)
		if err != nil {
			return fmt.Errorf("failed to convert to JSON: %w", err)
		}
		fmt.Println(string(out))
	} else {
		sections := []string{
			neighborInfo.String(),
			interfaceInfo.String(),
			routingInfo.String(),
		}
		fmt.Printf("\n%s\n", strings.Join(sections, "\n"))
	}

	return nil
}

func main() {
	var jsonFmt *bool

	infoGroup := &cobra.Group{ID: "info", Title: "Info gathering:"}

	ipCmd := &cobra.Command{
		Use:     "ip <address>",
		Short:   "Get aggregated information for the given IP address",
		Args:    cobra.ExactArgs(1),
		GroupID: infoGroup.ID,
		Run: func(cmd *cobra.Command, args []string) {
			if err := printIPInfo(args[0], *jsonFmt); err != nil {
				fmt.Println(err)
				os.Exit(1)
			}
		},
	}
	rootCmd := &cobra.Command{
		Use:   "premshow",
		Short: "Custom SONiC show CLI",
	}
	jsonFmt = rootCmd.PersistentFlags().Bool("json", false, "return output in json format")

	rootCmd.AddGroup(infoGroup)
	rootCmd.AddCommand(ipCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatalln(err)
	}
}
