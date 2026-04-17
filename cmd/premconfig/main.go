package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/premday/sonic-tools/internal/fetcher"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	redis "github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"
)

var (
	tblHeaderStyle = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	tblCellStyle   = lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)
	tblBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))

	summaryStyle = lipgloss.NewStyle().Faint(true).MarginTop(1)
)

type descriptionResult struct {
	Interface      string
	OldDescription string
	NewDescription string
	Status         string
}

func renderResults(results []descriptionResult, dryRun bool) string {
	var buf strings.Builder

	lt := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(tblBorderStyle).
		Headers("Interface", "Old description", "New description").
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return tblHeaderStyle
			}
			return tblCellStyle
		})

	for _, r := range results {
		oldDescr := r.OldDescription
		if oldDescr == "" {
			oldDescr = "-"
		}
		newDescr := r.NewDescription
		if newDescr == "" {
			newDescr = "-"
		}
		if r.Status == "unchanged" {
			newDescr = "unchanged"
		}
		lt.Row(r.Interface, oldDescr, newDescr)
	}

	buf.WriteString(lt.String() + "\n")

	changed, total := 0, len(results)
	for _, r := range results {
		if r.Status == "updated" || r.Status == "dry-run" {
			changed++
		}
	}

	summary := fmt.Sprintf("%d / %d interfaces changed", changed, total)
	if dryRun {
		summary += " (dry-run)"
	}
	buf.WriteString(summaryStyle.Render(summary) + "\n")

	return buf.String()
}

// sanitizeDescription removes any characters that are not alphanumeric, colon, hyphen, or dot.
var descrRegex = regexp.MustCompile(`[^a-zA-Z0-9:\-\.]+`)

func sanitizeDescription(description string) string {
	return descrRegex.ReplaceAllString(description, "")
}

func getOldDescription(ctx context.Context, rdb *redis.Client, intf string) string {
	val, err := rdb.HGet(ctx, fmt.Sprintf("PORT|%s", intf), "description").Result()
	if err != nil {
		return ""
	}
	return val
}

func UpdateDescriptionWithLLDP(ctx context.Context, rdb *redis.Client, lldp fetcher.LLDP, intf, prefix string, dryRun bool) descriptionResult {
	result := descriptionResult{
		Interface:      intf,
		OldDescription: getOldDescription(ctx, rdb, intf),
	}

	remoteHost, remoteIntf := lldp.ExtractInterfaceNeighbor(intf)
	if remoteHost == "" {
		result.Status = "skipped"
		result.NewDescription = result.OldDescription
		return result
	}

	description := remoteHost
	if remoteIntf != "N/A" && remoteIntf != "" {
		description = fmt.Sprintf("%s:%s", description, remoteIntf)
	}

	if prefix != "" {
		description = fmt.Sprintf("%s%s", prefix, description)
	}

	description = sanitizeDescription(description)
	result.NewDescription = description

	return SetInterfaceDescription(ctx, rdb, result, dryRun)
}

func SetInterfaceDescription(ctx context.Context, rdb *redis.Client, result descriptionResult, dryRun bool) descriptionResult {
	conn := rdb.Conn()
	defer conn.Close()

	if err := conn.Select(ctx, fetcher.CONFIGDB).Err(); err != nil {
		result.Status = fmt.Sprintf("failed to select CONFIG_DB: %s", err)
		return result
	}

	key := fmt.Sprintf("PORT|%s", result.Interface)
	exists, err := rdb.Exists(ctx, key).Result()
	if err != nil || exists == 0 {
		result.Status = fmt.Sprintf("error: interface %s does not exist", result.Interface)
		return result
	}

	result.NewDescription = sanitizeDescription(result.NewDescription)

	if result.NewDescription == result.OldDescription {
		result.Status = "unchanged"
		return result
	}

	if dryRun {
		result.Status = "dry-run"
		return result
	}

	_, err = rdb.HSet(ctx, key, "description", result.NewDescription).Result()
	if err != nil {
		result.Status = fmt.Sprintf("error: %s", err)
		return result
	}

	result.Status = "updated"
	return result
}

func main() {
	dryRun := false
	verbose := false
	intf := &cobra.Command{Use: "interface", Short: "Edit interfaces"}

	autoDescription := &cobra.Command{
		Use:   "auto-description <intf|all> [prefix]",
		Short: "Set interface description using LLDP data",
		Args:  cobra.RangeArgs(1, 2),
		Run: func(cmd *cobra.Command, args []string) {
			lldp, err := fetcher.FetchLLDPNeighbor()
			if err != nil {
				fmt.Printf("LLDP failure: %s\n", err.Error())
				os.Exit(1)
			}

			rdb := redis.NewClient(&redis.Options{
				Addr:     "127.0.0.1:6379",
				Password: "",
				DB:       fetcher.CONFIGDB,
			})

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			prefix := ""
			if len(args) > 1 {
				prefix = args[1]
			}

			var results []descriptionResult

			if args[0] == "all" {
				interfaces, err := fetcher.FetchInterfaceNeighbors(ctx, rdb)
				if err != nil {
					fmt.Printf("failed to fetch interfaces: %s", err.Error())
					os.Exit(1)
				}

				for intfName, descr := range interfaces {
					if !strings.HasPrefix(descr, prefix) {
						continue
					}

					result := UpdateDescriptionWithLLDP(ctx, rdb, lldp, intfName, prefix, dryRun)
					if result.Status == "skipped" && !verbose {
						continue
					}
					results = append(results, result)
				}
			} else {
				result := UpdateDescriptionWithLLDP(ctx, rdb, lldp, args[0], prefix, dryRun)
				results = append(results, result)
			}

			fmt.Print("\n" + renderResults(results, dryRun) + "\n")
		},
	}
	descrCmd := &cobra.Command{
		Use:   "description <intf> <description>",
		Short: "Set interface description",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			rdb := redis.NewClient(&redis.Options{
				Addr:     "127.0.0.1:6379",
				Password: "",
			})

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			result := descriptionResult{
				Interface:      args[0],
				OldDescription: getOldDescription(ctx, rdb, args[0]),
				NewDescription: args[1],
			}
			result = SetInterfaceDescription(ctx, rdb, result, dryRun)

			fmt.Print("\n" + renderResults([]descriptionResult{result}, dryRun) + "\n")
		},
	}
	rootCmd := &cobra.Command{
		Use:   "premconfig",
		Short: "Custom SONiC config CLI",
	}

	autoDescription.Flags().BoolVar(&dryRun, "dry-run", false, "do not apply changes")
	autoDescription.Flags().BoolVarP(&verbose, "verbose", "v", false, "verbose")
	descrCmd.Flags().BoolVar(&dryRun, "dry-run", false, "do not apply changes")

	intf.AddCommand(autoDescription)
	intf.AddCommand(descrCmd)

	rootCmd.AddCommand(intf)

	if err := rootCmd.Execute(); err != nil {
		log.Fatalln(err)
	}
}
