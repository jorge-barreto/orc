package main

import (
	"context"
	"fmt"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/debug"
	cli "github.com/urfave/cli/v3"
)

func debugCmd() *cli.Command {
	return &cli.Command{
		Name:      "debug",
		Usage:     "Analyze a phase execution — what the agent saw and did",
		ArgsUsage: "<phase> [ticket]",
		UsageText: "orc debug plan\n   orc debug plan KS-42\n   orc debug 2\n   orc debug -w bugfix plan KS-42",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// 1. Get phase reference (required first argument)
			phaseRef := cmd.Args().First()
			if phaseRef == "" {
				return fmt.Errorf("phase argument is required (name or 1-indexed number)")
			}

			// 2. Get optional ticket from second argument
			ticket := cmd.Args().Get(1)

			// 3. Find project root
			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
			}

			// 4. Resolve workflow
			flagWorkflow := cmd.Root().String("workflow")
			workflowName, configPath, err := resolveWorkflow(projectRoot, flagWorkflow)
			if err != nil {
				return err
			}

			// 5. Load config
			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			// 6. Resolve phase reference
			phaseIdx, err := config.ResolvePhaseRef(phaseRef, cfg.Phases)
			if err != nil {
				return err
			}

			// 7. Resolve ticket: auto-discover if not provided
			if ticket == "" {
				ticket, err = debug.FindMostRecentTicket(projectRoot, workflowName)
				if err != nil {
					return err
				}
			}

			// 8. Validate ticket (both user-provided and auto-discovered)
			if err := validateTicketPath(ticket); err != nil {
				return err
			}
			if err := config.ValidateTicket(cfg.TicketPattern, ticket); err != nil {
				return err
			}

			// 9. Run analysis
			return debug.Run(projectRoot, cfg, phaseIdx, ticket, workflowName)
		},
	}
}
