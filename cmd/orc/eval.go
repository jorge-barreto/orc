package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/eval"
	cli "github.com/urfave/cli/v3"
)

func evalCmd() *cli.Command {
	return &cli.Command{
		Name:      "eval",
		Usage:     "Run eval cases to measure workflow quality",
		ArgsUsage: "[case]",
		UsageText: "orc eval\n   orc eval bug-fix\n   orc eval --report\n   orc eval --list\n   orc eval --json",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "report", Usage: "Show score history across runs"},
			&cli.BoolFlag{Name: "list", Usage: "List available eval cases"},
			&cli.BoolFlag{Name: "json", Usage: "Output as structured JSON"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if os.Getenv("CLAUDECODE") != "" {
				return fmt.Errorf("orc cannot run inside Claude Code (CLAUDECODE env var is set). Run from a regular terminal")
			}

			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
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

			flagWorkflow := cmd.Root().String("workflow")
			workflowName, configPath, err := resolveWorkflow(projectRoot, flagWorkflow)
			if err != nil {
				return err
			}

			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			caseName := cmd.Args().First()
			if caseName != "" {
				if caseName != filepath.Base(caseName) || caseName == ".." || caseName == "." {
					return fmt.Errorf("invalid case name %q: must not contain path separators", caseName)
				}
			}

			fingerprint, cases, err := eval.RunEval(ctx, projectRoot, configPath, workflowName, cfg, caseName)
			if err != nil {
				return err
			}

			if cmd.Bool("json") {
				return eval.RenderJSON(os.Stdout, fingerprint, cases)
			}
			eval.RenderScoreReport(os.Stdout, fingerprint, cases)
			return nil
		},
	}
}
