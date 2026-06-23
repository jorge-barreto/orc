package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/eval"
	"github.com/jorge-barreto/orc/internal/runner"
	cli "github.com/urfave/cli/v3"
)

func evalCmd() *cli.Command {
	return &cli.Command{
		Name:      "eval",
		Usage:     "Run eval cases to measure workflow quality",
		ArgsUsage: "[case] [run-id]",
		UsageText: "orc eval\n   orc eval bug-fix\n   orc eval bug-fix --regrade\n   orc eval bug-fix --regrade <run-id>\n   orc eval --report\n   orc eval --list\n   orc eval --json",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "report", Usage: "Show score history across runs"},
			&cli.BoolFlag{Name: "list", Usage: "List available eval cases"},
			&cli.BoolFlag{Name: "json", Usage: "Output as structured JSON"},
			&cli.BoolFlag{Name: "regrade", Usage: "Re-grade a saved run against the current rubric without re-running; pass an optional run id as a second argument (defaults to the latest run)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgErr := func(err error) error {
				return &runner.ExitError{Code: runner.ExitConfigError, Err: err}
			}

			if os.Getenv("CLAUDECODE") != "" {
				return cfgErr(fmt.Errorf("orc cannot run inside Claude Code (CLAUDECODE env var is set). Run from a regular terminal"))
			}

			projectRoot, err := findProjectRoot()
			if err != nil {
				return cfgErr(err)
			}

			if cmd.Bool("list") && cmd.Bool("report") {
				return cfgErr(fmt.Errorf("--list and --report are mutually exclusive"))
			}
			// --regrade with --list/--report would silently ignore the regrade
			// (those flags early-return below). Reject so the contradiction is loud.
			if cmd.Bool("regrade") && (cmd.Bool("list") || cmd.Bool("report")) {
				return cfgErr(fmt.Errorf("--regrade cannot be combined with --list or --report"))
			}

			// --list and --report early-return BEFORE resolveWorkflow() —
			// they are workflow-agnostic and must work in multi-workflow projects
			// without -w flag.
			if cmd.Bool("list") {
				if cmd.Bool("json") {
					return eval.RenderCaseListJSON(os.Stdout, projectRoot)
				}
				return eval.RenderCaseList(os.Stdout, projectRoot)
			}
			if cmd.Bool("report") {
				h, err := eval.LoadHistory(projectRoot)
				if err != nil {
					return err
				}
				if cmd.Bool("json") {
					return eval.RenderHistoryJSON(os.Stdout, h)
				}
				eval.RenderHistoryReport(os.Stdout, h)
				return nil
			}

			ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()

			flagWorkflow := cmd.Root().String("workflow")
			workflowName, configPath, err := resolveWorkflow(projectRoot, flagWorkflow)
			if err != nil {
				return cfgErr(err)
			}

			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return cfgErr(fmt.Errorf("loading config: %w", err))
			}

			caseName := cmd.Args().First()
			if caseName != "" {
				if caseName != filepath.Base(caseName) || caseName == ".." || caseName == "." {
					return cfgErr(fmt.Errorf("invalid case name %q: must not contain path separators", caseName))
				}
			}

			if cmd.Bool("regrade") {
				if caseName == "" {
					return cfgErr(fmt.Errorf("--regrade requires a case name: orc eval <case> --regrade [run-id]"))
				}
				runID := cmd.Args().Get(1)
				if runID != "" && (runID != filepath.Base(runID) || runID == ".." || runID == ".") {
					return cfgErr(fmt.Errorf("invalid run id %q: must not contain path separators", runID))
				}
				fingerprint, cases, err := eval.RegradeEval(ctx, projectRoot, configPath, workflowName, cfg, caseName, runID)
				if err != nil {
					// RegradeEval failures are all config/setup errors (missing
					// fixture/spec/rubric, no saved runs, run not found, invalid
					// run id) — exit ExitConfigError (3) per the documented contract.
					return cfgErr(fmt.Errorf("re-grading eval: %w", err))
				}
				if cmd.Bool("json") {
					return eval.RenderJSON(os.Stdout, fingerprint, cases)
				}
				eval.RenderScoreReport(os.Stdout, fingerprint, cases)
				return nil
			}

			fingerprint, cases, err := eval.RunEval(ctx, projectRoot, configPath, workflowName, cfg, caseName)
			if err != nil {
				return fmt.Errorf("running eval: %w", err)
			}

			if cmd.Bool("json") {
				return eval.RenderJSON(os.Stdout, fingerprint, cases)
			}
			eval.RenderScoreReport(os.Stdout, fingerprint, cases)
			return nil
		},
	}
}
