package main

import (
	"context"
	"fmt"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/debug"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
	cli "github.com/urfave/cli/v3"
)

func historyCmd() *cli.Command {
	return &cli.Command{
		Name:      "history",
		Usage:     "List past runs for a ticket",
		ArgsUsage: "[ticket]",
		UsageText: "orc history\n   orc history PROJ-123\n   orc history --prune",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "prune", Usage: "Remove runs beyond the history limit"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
			}

			flagWorkflow := cmd.Root().String("workflow")
			workflowName, configPath, err := resolveWorkflow(projectRoot, flagWorkflow)
			if err != nil {
				return err
			}

			ticket := cmd.Args().First()
			if ticket == "" {
				ticket, err = debug.FindMostRecentTicket(projectRoot, workflowName)
				if err != nil {
					return err
				}
			}
			if err := validateTicketPath(ticket); err != nil {
				return err
			}

			artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflowName, ticket)

			if cmd.Bool("prune") {
				limit := 10
				if configPath != "" {
					if cfg, loadErr := config.Load(configPath, projectRoot); loadErr == nil {
						limit = cfg.HistoryLimit
					}
				}
				if err := state.PruneHistory(artifactsDir, limit); err != nil {
					return fmt.Errorf("pruning history: %w", err)
				}
				fmt.Printf("%s✓ History pruned to %d entries%s\n", ux.Green, limit, ux.Reset)
				return nil
			}

			entries, err := state.ListHistory(artifactsDir)
			if err != nil {
				return fmt.Errorf("listing history: %w", err)
			}
			if len(entries) == 0 {
				fmt.Printf("No history for ticket %s\n", ticket)
				return nil
			}

			fmt.Printf("\n  %sRun history for %s%s (%d entries)\n\n", ux.Bold, ticket, ux.Reset, len(entries))
			fmt.Printf("  %-26s  %-30s  %-10s  %s\n", "Run ID", "Status", "Duration", "Cost")
			fmt.Printf("  %-26s  %-30s  %-10s  %s\n", "------", "------", "--------", "----")
			for _, e := range entries {
				elapsed := "—"
				if e.Elapsed > 0 {
					elapsed = state.FormatDuration(e.Elapsed)
				}
				cost := "—"
				if e.CostUSD > 0 {
					cost = fmt.Sprintf("$%.2f", e.CostUSD)
				}
				status := e.Status
				if e.FailureCategory != "" {
					status += " (" + e.FailureCategory + ")"
				}
				fmt.Printf("  %-26s  %-30s  %-10s  %s\n", e.RunID, status, elapsed, cost)
			}
			fmt.Println()
			return nil
		},
	}
}
