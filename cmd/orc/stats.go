package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jorge-barreto/orc/internal/runner"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/stats"
	cli "github.com/urfave/cli/v3"
)

func statsCmd() *cli.Command {
	return &cli.Command{
		Name:      "stats",
		Usage:     "Show aggregate metrics across runs",
		ArgsUsage: "[ticket]",
		UsageText: "orc stats\n   orc stats KS-42\n   orc stats --last 20\n   orc stats --json",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "last", Usage: "Limit to last N runs (default: all)"},
			&cli.BoolFlag{Name: "json", Usage: "Output as structured JSON"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgErr := func(err error) error {
				return &runner.ExitError{Code: runner.ExitConfigError, Err: err}
			}

			projectRoot, err := findProjectRoot()
			if err != nil {
				return cfgErr(err)
			}

			flagWorkflow := cmd.Root().String("workflow")
			workflowName, _, err := resolveWorkflow(projectRoot, flagWorkflow)
			if err != nil {
				return cfgErr(err)
			}

			ticket := cmd.Args().First()
			if ticket != "" {
				if err := validateTicketPath(ticket); err != nil {
					return cfgErr(err)
				}
			}

			auditBaseDir := state.AuditBaseDirForWorkflow(projectRoot, workflowName)

			runs, err := stats.CollectRuns(auditBaseDir)
			if err != nil {
				return fmt.Errorf("collecting audit data: %w", err)
			}

			last := int(cmd.Int("last"))
			runs = stats.FilterRuns(runs, ticket, last)

			if len(runs) == 0 {
				if ticket != "" {
					fmt.Printf("No audited runs found for ticket %s\n", ticket)
				} else {
					fmt.Println("No audited runs found.")
				}
				return nil
			}

			result := stats.Aggregate(runs)

			if cmd.Bool("json") {
				return stats.RenderJSON(os.Stdout, result)
			}
			stats.RenderText(os.Stdout, result)
			return nil
		},
	}
}
