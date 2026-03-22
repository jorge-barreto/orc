package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/debug"
	"github.com/jorge-barreto/orc/internal/report"
	"github.com/jorge-barreto/orc/internal/state"
	cli "github.com/urfave/cli/v3"
)

func reportCmd() *cli.Command {
	return &cli.Command{
		Name:      "report",
		Usage:     "Generate a summary report of a completed or failed run",
		ArgsUsage: "[ticket]",
		UsageText: "orc report\n   orc report PROJ-123\n   orc report --json\n   orc report PROJ-123 --json",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "json", Usage: "Output as structured JSON"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// 1. Find project root
			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
			}

			// 2. Resolve workflow
			workflowName, configPath, err := resolveWorkflow(projectRoot, cmd.Root().String("workflow"))
			if err != nil {
				return err
			}

			// 3. Load config
			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// 4. Resolve ticket: auto-discover if not provided
			ticket := cmd.Args().First()
			if ticket == "" {
				ticket, err = debug.FindMostRecentTicket(projectRoot, workflowName)
				if err != nil {
					return err
				}
			}

			// 5. Validate ticket (both user-provided and auto-discovered)
			if err := validateTicketPath(ticket); err != nil {
				return err
			}
			if err := config.ValidateTicket(cfg.TicketPattern, ticket); err != nil {
				return err
			}

			// 6. Compute directories
			artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflowName, ticket)
			auditDir := state.AuditDirForWorkflow(projectRoot, workflowName, ticket)

			// 7. Check state exists before loading
			if !state.HasState(artifactsDir) {
				return fmt.Errorf("no run found for ticket %s", ticket)
			}

			// 8. Load state
			st, err := state.Load(artifactsDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			// 9. Build report
			data, err := report.Build(artifactsDir, auditDir, st, cfg.Phases)
			if err != nil {
				return fmt.Errorf("building report: %w", err)
			}

			// 10. Render output
			if cmd.Bool("json") {
				return report.RenderJSON(os.Stdout, data)
			}
			report.RenderMarkdown(os.Stdout, data)
			return nil
		},
	}
}
