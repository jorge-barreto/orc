package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/runner"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
	cli "github.com/urfave/cli/v3"
)

func testCmd() *cli.Command {
	return &cli.Command{
		Name:      "test",
		Usage:     "Run a single phase in isolation for testing",
		ArgsUsage: "<phase> <ticket>",
		UsageText: "orc test plan KS-42\n   orc test implement KS-42\n   orc test 3 KS-42\n   orc test -w bugfix fix KS-42",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "auto", Usage: "Unattended mode — skip gates, no interactive steering"},
			&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Save raw stream-json output to .stream.jsonl files"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgErr := func(err error) error {
				return &runner.ExitError{Code: runner.ExitConfigError, Err: err}
			}

			// CLAUDECODE guard
			if os.Getenv("CLAUDECODE") != "" {
				return cfgErr(fmt.Errorf("orc cannot run inside Claude Code (CLAUDECODE env var is set). Run from a regular terminal"))
			}

			projectRoot, err := findProjectRoot()
			if err != nil {
				return cfgErr(err)
			}

			args := cmd.Args().Slice()
			flagWorkflow := cmd.Root().String("workflow")

			var phaseRef, ticket string
			switch {
			case len(args) == 3 && flagWorkflow == "":
				if _, found := resolveWorkflowByName(projectRoot, args[0]); found {
					flagWorkflow = args[0]
					phaseRef = args[1]
					ticket = args[2]
				} else {
					return cfgErr(fmt.Errorf("expected: orc test <phase> <ticket>"))
				}
			case len(args) == 2:
				phaseRef = args[0]
				ticket = args[1]
			default:
				return cfgErr(fmt.Errorf("expected: orc test <phase> <ticket>"))
			}

			if err := validateTicketPath(ticket); err != nil {
				return cfgErr(err)
			}

			workflowName, configPath, err := resolveWorkflow(projectRoot, flagWorkflow)
			if err != nil {
				return cfgErr(err)
			}

			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return cfgErr(fmt.Errorf("loading config: %w", err))
			}

			if err := config.ValidateTicket(cfg.TicketPattern, ticket); err != nil {
				return cfgErr(err)
			}

			phaseIdx, err := config.ResolvePhaseRef(phaseRef, cfg.Phases)
			if err != nil {
				return cfgErr(err)
			}

			artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflowName, ticket)
			phase := cfg.Phases[phaseIdx]
			env := &dispatch.Environment{
				ProjectRoot:       projectRoot,
				WorkDir:           projectRoot,
				ArtifactsDir:      artifactsDir,
				Ticket:            ticket,
				Workflow:          workflowName,
				AutoMode:          cmd.Bool("auto"),
				Verbose:           cmd.Bool("verbose"),
				PhaseIndex:        phaseIdx,
				PhaseCount:        len(cfg.Phases),
				DefaultAllowTools: cfg.DefaultAllowTools,
			}
			if len(cfg.Vars) > 0 {
				env.CustomVars = dispatch.ExpandConfigVars(cfg.Vars, env.Vars())
			}

			if err := state.EnsureDir(artifactsDir); err != nil {
				return cfgErr(err)
			}

			checkMissingArtifacts(cfg.Phases, phaseIdx, artifactsDir)

			if err := dispatch.Preflight([]config.Phase{phase}); err != nil {
				return cfgErr(err)
			}

			ux.PhaseHeader(phaseIdx, len(cfg.Phases), phase)

			start := time.Now()
			result, err := dispatch.Dispatch(ctx, phase, env)
			duration := time.Since(start)
			if err != nil {
				return err
			}

			if result.ExitCode == 0 {
				ux.PhaseComplete(phaseIdx, duration)
			} else {
				ux.PhaseFail(phaseIdx, phase.Name, fmt.Sprintf("exit code %d", result.ExitCode))
			}

			if result.ExitCode != 0 {
				return &runner.ExitError{Code: runner.ExitRetryable, Err: fmt.Errorf("phase %q failed with exit code %d", phase.Name, result.ExitCode)}
			}
			return nil
		},
	}
}

// checkMissingArtifacts checks for declared outputs from phases that precede
// the target phase. For each missing file, it prints a warning showing which
// earlier phase normally creates it.
func checkMissingArtifacts(phases []config.Phase, targetIdx int, artifactsDir string) {
	var warnings []string
	for i := 0; i < targetIdx; i++ {
		for _, output := range phases[i].Outputs {
			path := filepath.Join(artifactsDir, output)
			if _, err := os.Stat(path); err != nil {
				warnings = append(warnings, fmt.Sprintf("  %s (normally created by phase %d: %s)", output, i+1, phases[i].Name))
			}
		}
	}
	if len(warnings) > 0 {
		fmt.Fprintf(os.Stderr, "%swarning: missing artifacts from earlier phases:%s\n", ux.Yellow, ux.Reset)
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "%s%s%s\n", ux.Yellow, w, ux.Reset)
		}
		fmt.Fprintln(os.Stderr)
	}
}
