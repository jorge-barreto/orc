package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/runner"
	"github.com/jorge-barreto/orc/internal/ux"
	cli "github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
)

// runValidate loads, parses, and validates a config file. It returns the
// validated config or an error. It does not print anything.
func runValidate(configPath, projectRoot string) (*config.Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	if err := config.Validate(&cfg, projectRoot); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func validateCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate",
		Usage: "Validate config without running",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Usage: "Path to config file (default: .orc/config.yaml in project root)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			cfgErr := func(err error) error {
				return &runner.ExitError{Code: runner.ExitConfigError, Err: err}
			}

			var configPath, projectRoot string

			if flagVal := cmd.String("config"); flagVal != "" {
				absPath, err := filepath.Abs(flagVal)
				if err != nil {
					return cfgErr(fmt.Errorf("resolving config path: %w", err))
				}
				configPath = absPath
				cwd, err := os.Getwd()
				if err != nil {
					return cfgErr(err)
				}
				projectRoot = cwd
			} else {
				root, err := findProjectRoot()
				if err != nil {
					return cfgErr(err)
				}
				projectRoot = root
				configPath = filepath.Join(projectRoot, ".orc", "config.yaml")
			}

			cfg, err := runValidate(configPath, projectRoot)
			if err != nil {
				return cfgErr(err)
			}

			printConfigSummary(os.Stdout, cfg, projectRoot)
			return nil
		},
	}
}

// printConfigSummary prints a human-readable summary of a validated config.
func printConfigSummary(w io.Writer, cfg *config.Config, projectRoot string) {
	// Header
	noun := "phases"
	if len(cfg.Phases) == 1 {
		noun = "phase"
	}
	fmt.Fprintf(w, "%s✓ Config valid — %s (%d %s)%s\n", ux.Green, cfg.Name, len(cfg.Phases), noun, ux.Reset)

	if cfg.MaxCost > 0 {
		fmt.Fprintf(w, "  max-cost: $%.2f\n", cfg.MaxCost)
	}

	// Ticket pattern
	if cfg.TicketPattern != "" {
		fmt.Fprintf(w, "  ticket-pattern: %s\n", cfg.TicketPattern)
	}

	// Vars section
	fmt.Fprintf(w, "\n%sVars:%s\n", ux.Bold, ux.Reset)
	fmt.Fprintf(w, "  %-14s %s(built-in)%s\n", "TICKET", ux.Dim, ux.Reset)
	fmt.Fprintf(w, "  %-14s %s(built-in)%s\n", "ARTIFACTS_DIR", ux.Dim, ux.Reset)
	fmt.Fprintf(w, "  %-14s %s(built-in)%s\n", "WORK_DIR", ux.Dim, ux.Reset)
	fmt.Fprintf(w, "  %-14s %s(built-in)%s\n", "PROJECT_ROOT", ux.Dim, ux.Reset)
	fmt.Fprintf(w, "  %s(per-phase: PHASE_INDEX, PHASE_COUNT)%s\n", ux.Dim, ux.Reset)

	if len(cfg.Vars) > 0 {
		builtins := map[string]string{
			"TICKET":       "<ticket>",
			"ARTIFACTS_DIR": "<artifacts>",
			"WORK_DIR":      projectRoot,
			"PROJECT_ROOT":  projectRoot,
		}
		expanded := dispatch.ExpandConfigVars(cfg.Vars, builtins)
		for _, v := range cfg.Vars {
			fmt.Fprintf(w, "  %s = %s %s(from config)%s\n", v.Key, expanded[v.Key], ux.Dim, ux.Reset)
		}
	}

	// Phases section
	fmt.Fprintf(w, "\n%sPhases:%s\n", ux.Bold, ux.Reset)
	for i, p := range cfg.Phases {
		// Main line
		fmt.Fprintf(w, "  %s%d.%s %s%s%s", ux.Cyan, i+1, ux.Reset, ux.Bold, p.Name, ux.Reset)

		switch p.Type {
		case "agent":
			fmt.Fprintf(w, "  agent   model=%s effort=%s timeout=%dm prompt=%s\n", p.Model, p.Effort, p.Timeout, p.Prompt)
		case "script":
			cmd := p.Run
			if len(cmd) > 60 {
				cmd = cmd[:57] + "..."
			}
			fmt.Fprintf(w, "  script  timeout=%dm  run: %s\n", p.Timeout, cmd)
		case "gate":
			fmt.Fprintf(w, "  gate\n")
		}

		// Sub-lines
		indent := strings.Repeat(" ", 18)
		if len(p.Outputs) > 0 {
			fmt.Fprintf(w, "%soutputs: [%s]\n", indent, strings.Join(p.Outputs, ", "))
		}
		if p.Loop != nil {
			fmt.Fprintf(w, "%sloop: goto %s (min %d, max %d)\n", indent, p.Loop.Goto, p.Loop.Min, p.Loop.Max)
			if p.Loop.OnExhaust != nil {
				fmt.Fprintf(w, "%son-exhaust: goto %s (max %d)\n", indent, p.Loop.OnExhaust.Goto, p.Loop.OnExhaust.Max)
			}
		}
		if p.ParallelWith != "" {
			fmt.Fprintf(w, "%sparallel-with: %s\n", indent, p.ParallelWith)
		}
		if p.Condition != "" {
			fmt.Fprintf(w, "%scondition: %s\n", indent, p.Condition)
		}
		if p.Cwd != "" {
			fmt.Fprintf(w, "%scwd: %s\n", indent, p.Cwd)
		}
		if p.MaxCost > 0 {
			fmt.Fprintf(w, "%smax-cost: $%.2f\n", indent, p.MaxCost)
		}
	}
}
