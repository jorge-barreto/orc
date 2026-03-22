package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/debug"
	"github.com/jorge-barreto/orc/internal/report"
	"github.com/jorge-barreto/orc/internal/runner"
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
			cfgErr := func(err error) error {
				return &runner.ExitError{Code: runner.ExitConfigError, Err: err}
			}

			// 1. Find project root
			projectRoot, err := findProjectRoot()
			if err != nil {
				return cfgErr(err)
			}

			// 2. Resolve workflow
			workflowName, configPath, err := resolveWorkflow(projectRoot, cmd.Root().String("workflow"))
			if err != nil {
				return cfgErr(err)
			}

			// 3. Load config
			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return cfgErr(fmt.Errorf("loading config: %w", err))
			}

			// 4. Resolve ticket: auto-discover if not provided
			ticket := cmd.Args().First()
			if ticket == "" {
				ticket, err = debug.FindMostRecentTicket(projectRoot, workflowName)
				if err != nil {
					return cfgErr(err)
				}
			}

			// 5. Validate ticket (both user-provided and auto-discovered)
			if err := validateTicketPath(ticket); err != nil {
				return cfgErr(err)
			}
			if err := config.ValidateTicket(cfg.TicketPattern, ticket); err != nil {
				return cfgErr(err)
			}

			// 6. Compute directories
			artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflowName, ticket)
			auditDir := state.AuditDirForWorkflow(projectRoot, workflowName, ticket)

			// 7. Resolve state directory: live artifacts or latest history entry
			stateDir, err := resolveStateDir(artifactsDir)
			if err != nil {
				return fmt.Errorf("no run found for ticket %s", ticket)
			}

			// 8. Load state
			st, err := state.Load(stateDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			// 9. Build report
			data, err := report.Build(stateDir, auditDir, st, cfg.Phases)
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

// resolveStateDir finds the directory containing state.json for a ticket.
// It checks the live artifacts directory first, then falls back to the
// latest history entry. Returns an error if no state is found anywhere.
func resolveStateDir(artifactsDir string) (string, error) {
	if state.HasState(artifactsDir) {
		return artifactsDir, nil
	}
	histDir, err := state.LatestHistoryDir(artifactsDir)
	if err != nil {
		return "", fmt.Errorf("checking history: %w", err)
	}
	if histDir == "" || !state.HasState(histDir) {
		return "", fmt.Errorf("no state found")
	}
	return histDir, nil
}
