package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/doctor"
	"github.com/jorge-barreto/orc/internal/docs"
	"github.com/jorge-barreto/orc/internal/runner"
	"github.com/jorge-barreto/orc/internal/scaffold"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
	cli "github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:        "orc",
		Usage:       "Deterministic agent orchestrator",
		Description: "Run 'orc docs' for documentation on config syntax, variables, phases, and more.",
		Commands: []*cli.Command{
			initCmd(),
			runCmd(),
			statusCmd(),
			doctorCmd(),
			docsCmd(),
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%serror:%s %v\n", ux.Red, ux.Reset, err)
		os.Exit(1)
	}
}

func runCmd() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "Run the workflow for a ticket",
		ArgsUsage: "<ticket>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "auto", Usage: "Skip human gates"},
			&cli.IntFlag{Name: "retry", Usage: "Retry from phase N (1-indexed)"},
			&cli.IntFlag{Name: "from", Usage: "Start from phase N (1-indexed)"},
			&cli.BoolFlag{Name: "dry-run", Usage: "Print phase plan without executing"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			// CLAUDECODE guard
			if os.Getenv("CLAUDECODE") != "" {
				return fmt.Errorf("orc cannot run inside Claude Code (CLAUDECODE env var is set). Run from a regular terminal")
			}

			ticket := cmd.Args().First()
			if ticket == "" {
				return fmt.Errorf("ticket argument is required")
			}

			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
			}

			configPath := filepath.Join(projectRoot, ".orc", "config.yaml")
			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			if err := config.ValidateTicket(cfg.TicketPattern, ticket); err != nil {
				return err
			}

			artifactsDir := filepath.Join(projectRoot, ".orc", "artifacts")

			env := &dispatch.Environment{
				ProjectRoot:  projectRoot,
				WorkDir:      projectRoot,
				ArtifactsDir: artifactsDir,
				Ticket:       ticket,
				AutoMode:     cmd.Bool("auto"),
				PhaseCount:   len(cfg.Phases),
			}

			if len(cfg.Vars) > 0 {
				env.CustomVars = dispatch.ExpandConfigVars(cfg.Vars, env.Vars())
			}

			// Load or create state
			st, err := state.Load(artifactsDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}
			st.Ticket = ticket
			st.Status = state.StatusRunning

			// Handle --retry and --from (mutually exclusive)
			retry := cmd.Int("retry")
			from := cmd.Int("from")
			if retry > 0 && from > 0 {
				return fmt.Errorf("--retry and --from are mutually exclusive")
			}
			if retry > 0 {
				if int(retry) > len(cfg.Phases) {
					return fmt.Errorf("--retry %d exceeds phase count (%d)", retry, len(cfg.Phases))
				}
				st.SetPhase(int(retry) - 1)
			}
			if from > 0 {
				if int(from) > len(cfg.Phases) {
					return fmt.Errorf("--from %d exceeds phase count (%d)", from, len(cfg.Phases))
				}
				st.SetPhase(int(from) - 1)
			}

			// Reset loop counts when resuming from a specific phase
			if retry > 0 || from > 0 {
				if err := state.EnsureDir(artifactsDir); err != nil {
					return err
				}
				if err := state.SaveLoopCounts(artifactsDir, make(map[string]int)); err != nil {
					return fmt.Errorf("resetting loop counts: %w", err)
				}
			}

			if err := dispatch.Preflight(cfg.Phases); err != nil {
				return err
			}

			r := &runner.Runner{
				Config:     cfg,
				State:      st,
				Env:        env,
				Dispatcher: &dispatch.DefaultDispatcher{},
			}

			// Handle --dry-run
			if cmd.Bool("dry-run") {
				r.DryRunPrint()
				return nil
			}

			// Ensure artifacts directory exists and save initial state
			if err := state.EnsureDir(artifactsDir); err != nil {
				return err
			}
			if err := st.Save(artifactsDir); err != nil {
				return err
			}

			// Set up signal handling
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()

			return r.Run(ctx)
		},
	}
}

func statusCmd() *cli.Command {
	return &cli.Command{
		Name:      "status",
		Usage:     "Show workflow status for a ticket",
		ArgsUsage: "<ticket>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			ticket := cmd.Args().First()
			if ticket == "" {
				return fmt.Errorf("ticket argument is required")
			}

			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
			}

			configPath := filepath.Join(projectRoot, ".orc", "config.yaml")
			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			artifactsDir := filepath.Join(projectRoot, ".orc", "artifacts")
			st, err := state.Load(artifactsDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			if st.Ticket != "" && st.Ticket != ticket {
				return fmt.Errorf("state is for ticket %q, not %q", st.Ticket, ticket)
			}

			ux.RenderStatus(cfg, st, artifactsDir)
			return nil
		},
	}
}

func doctorCmd() *cli.Command {
	return &cli.Command{
		Name:  "doctor",
		Usage: "Diagnose a failed workflow run using AI",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
			}

			configPath := filepath.Join(projectRoot, ".orc", "config.yaml")
			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			artifactsDir := filepath.Join(projectRoot, ".orc", "artifacts")
			st, err := state.Load(artifactsDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			return doctor.Run(ctx, projectRoot, artifactsDir, cfg, st)
		},
	}
}

func initCmd() *cli.Command {
	return &cli.Command{
		Name:  "init",
		Usage: "Initialize a new .orc/ directory with example config",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			return scaffold.Init(ctx, dir)
		},
	}
}

func docsCmd() *cli.Command {
	return &cli.Command{
		Name:      "docs",
		Usage:     "Show documentation",
		ArgsUsage: "[topic]",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			name := cmd.Args().First()
			if name == "" {
				fmt.Print("\nAvailable topics:\n\n")
				for _, t := range docs.All() {
					fmt.Printf("  %-14s %s\n", t.Name, t.Summary)
				}
				fmt.Println("\nRun 'orc docs <topic>' to read a topic.")
				return nil
			}
			t, err := docs.Get(name)
			if err != nil {
				return err
			}
			fmt.Print(t.Content)
			return nil
		},
	}
}

// findProjectRoot walks up from cwd looking for .orc/config.yaml.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		configPath := filepath.Join(dir, ".orc", "config.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .orc/config.yaml found (searched from cwd to root)")
		}
		dir = parent
	}
}
