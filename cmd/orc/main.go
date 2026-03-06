package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/docs"
	"github.com/jorge-barreto/orc/internal/doctor"
	"github.com/jorge-barreto/orc/internal/improve"
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
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "no-color", Usage: "Disable colored output"},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			if cmd.Bool("no-color") || os.Getenv("NO_COLOR") != "" || !ux.IsTerminal(os.Stdout.Fd()) {
				ux.DisableColor()
			}
			return ctx, nil
		},
		Commands: []*cli.Command{
			initCmd(),
			runCmd(),
			validateCmd(),
			flowCmd(),
			cancelCmd(),
			statusCmd(),
			doctorCmd(),
			docsCmd(),
			improveCmd(),
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%serror:%s %v\n", ux.Red, ux.Reset, err)
		os.Exit(runner.ExitCodeFrom(err))
	}
}

func runCmd() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "Run the workflow for a ticket",
		ArgsUsage: "<ticket>",
		UsageText: "orc run PROJ-123\n   orc run PROJ-123 --auto --verbose\n   orc run PROJ-123 --retry implement",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "auto", Usage: "Unattended mode — skip gates, no interactive steering"},
			&cli.StringFlag{Name: "retry", Usage: "Retry from phase number or name"},
			&cli.StringFlag{Name: "from", Usage: "Start from phase number or name"},
			&cli.BoolFlag{Name: "dry-run", Usage: "Print phase plan without executing"},
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

			ticket := cmd.Args().First()
			if ticket == "" {
				return cfgErr(fmt.Errorf("ticket argument is required"))
			}
			if err := validateTicketPath(ticket); err != nil {
				return cfgErr(err)
			}

			projectRoot, err := findProjectRoot()
			if err != nil {
				return cfgErr(err)
			}

			configPath := filepath.Join(projectRoot, ".orc", "config.yaml")
			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return cfgErr(fmt.Errorf("loading config: %w", err))
			}

			if err := config.ValidateTicket(cfg.TicketPattern, ticket); err != nil {
				return cfgErr(err)
			}

			artifactsDir := filepath.Join(projectRoot, ".orc", "artifacts", ticket)

			env := &dispatch.Environment{
				ProjectRoot:       projectRoot,
				WorkDir:           projectRoot,
				ArtifactsDir:      artifactsDir,
				Ticket:            ticket,
				AutoMode:          cmd.Bool("auto"),
				Verbose:           cmd.Bool("verbose"),
				PhaseCount:        len(cfg.Phases),
				DefaultAllowTools: cfg.DefaultAllowTools,
			}

			if len(cfg.Vars) > 0 {
				env.CustomVars = dispatch.ExpandConfigVars(cfg.Vars, env.Vars())
			}

			// Load or create state
			st, err := state.Load(artifactsDir)
			if err != nil {
				return cfgErr(fmt.Errorf("loading state: %w", err))
			}
			st.Ticket = ticket
			st.Status = state.StatusRunning

			// Handle --retry and --from (mutually exclusive)
			retryVal := cmd.String("retry")
			fromVal := cmd.String("from")
			if retryVal != "" && fromVal != "" {
				return cfgErr(fmt.Errorf("--retry and --from are mutually exclusive"))
			}
			if retryVal != "" {
				idx, err := resolvePhaseRef(retryVal, cfg.Phases)
				if err != nil {
					return cfgErr(fmt.Errorf("--retry: %w", err))
				}
				st.SetPhase(idx)
			}
			if fromVal != "" {
				idx, err := resolvePhaseRef(fromVal, cfg.Phases)
				if err != nil {
					return cfgErr(fmt.Errorf("--from: %w", err))
				}
				st.SetPhase(idx)
			}

			// Reset loop counts when resuming from a specific phase
			if retryVal != "" || fromVal != "" {
				if err := state.EnsureDir(artifactsDir); err != nil {
					return cfgErr(err)
				}
				if err := state.SaveLoopCounts(artifactsDir, make(map[string]int)); err != nil {
					return cfgErr(fmt.Errorf("resetting loop counts: %w", err))
				}
			}

			if err := dispatch.Preflight(cfg.Phases); err != nil {
				return cfgErr(err)
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
				return cfgErr(err)
			}
			if err := st.Save(artifactsDir); err != nil {
				return cfgErr(err)
			}

			// Set up signal handling
			ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
			defer stop()

			return r.Run(ctx)
		},
	}
}

func cancelCmd() *cli.Command {
	return &cli.Command{
		Name:      "cancel",
		Usage:     "Cancel a ticket and remove all artifacts",
		ArgsUsage: "<ticket>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "Cancel even if a run appears active"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			ticket := cmd.Args().First()
			if ticket == "" {
				return fmt.Errorf("ticket argument is required")
			}
			if err := validateTicketPath(ticket); err != nil {
				return err
			}

			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
			}

			artifactsDir := filepath.Join(projectRoot, ".orc", "artifacts", ticket)

			// Check if artifacts directory exists
			if _, err := os.Stat(artifactsDir); os.IsNotExist(err) {
				fmt.Printf("Nothing to cancel for ticket %s (no artifacts found).\n", ticket)
				return nil
			}

			// Load state to validate ticket and check status
			st, err := state.Load(artifactsDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			if st.Ticket != "" && st.Ticket != ticket {
				return fmt.Errorf("state is for ticket %q, not %q", st.Ticket, ticket)
			}

			if st.Status == state.StatusRunning && !cmd.Bool("force") {
				return fmt.Errorf("ticket %s appears to be running — Ctrl+C the process first, or use --force", ticket)
			}

			if err := os.RemoveAll(artifactsDir); err != nil {
				return fmt.Errorf("removing artifacts: %w", err)
			}

			// Rotate audit dir so it's preserved but distinguishable from future runs
			auditDir := state.AuditDir(projectRoot, ticket)
			if _, err := os.Stat(auditDir); err == nil {
				ts := time.Now().Format("060102-150405")
				rotated := filepath.Join(state.AuditBaseDir(projectRoot), fmt.Sprintf("%s-%s", ticket, ts))
				if err := os.Rename(auditDir, rotated); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to rotate audit dir: %v\n", err)
				}
			}

			fmt.Printf("%s✓ Cancelled ticket %s — artifacts removed%s\n", ux.Green, ticket, ux.Reset)
			return nil
		},
	}
}

func statusCmd() *cli.Command {
	return &cli.Command{
		Name:      "status",
		Usage:     "Show workflow status (all tickets, or one ticket)",
		ArgsUsage: "[ticket]",
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

			ticket := cmd.Args().First()

			// No argument: show all tickets
			if ticket == "" {
				baseDir := filepath.Join(projectRoot, ".orc", "artifacts")
				baseAuditDir := state.AuditBaseDir(projectRoot)
				tickets, err := state.ListTickets(baseDir, baseAuditDir)
				if err != nil {
					return fmt.Errorf("listing tickets: %w", err)
				}
				ux.RenderStatusAll(cfg, tickets)
				return nil
			}

			// With argument: single-ticket detail view
			if err := validateTicketPath(ticket); err != nil {
				return err
			}

			artifactsDir := filepath.Join(projectRoot, ".orc", "artifacts", ticket)
			if !state.HasState(artifactsDir) {
				return fmt.Errorf("no run found for ticket %s", ticket)
			}
			st, err := state.Load(artifactsDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			auditDir := state.AuditDir(projectRoot, ticket)
			ux.RenderStatus(cfg, st, artifactsDir, auditDir)
			return nil
		},
	}
}

func doctorCmd() *cli.Command {
	return &cli.Command{
		Name:      "doctor",
		Usage:     "Diagnose a failed workflow run using AI",
		ArgsUsage: "<ticket>",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			ticket := cmd.Args().First()
			if ticket == "" {
				return fmt.Errorf("ticket argument is required")
			}
			if err := validateTicketPath(ticket); err != nil {
				return err
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

			artifactsDir := filepath.Join(projectRoot, ".orc", "artifacts", ticket)
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
		Name:      "init",
		Usage:     "Initialize a new .orc/ directory with AI-generated workflow config",
		ArgsUsage: "[description]",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			userPrompt := cmd.Args().First()
			return scaffold.Init(ctx, dir, userPrompt)
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

func improveCmd() *cli.Command {
	return &cli.Command{
		Name:      "improve",
		Usage:     "Refine workflow config with AI assistance",
		ArgsUsage: "[instruction]",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			projectRoot, err := findProjectRoot()
			if err != nil {
				return err
			}
			instruction := cmd.Args().First()
			if instruction == "" {
				return improve.Interactive(projectRoot)
			}
			return improve.OneShot(ctx, projectRoot, instruction)
		},
	}
}

// validateTicketPath rejects ticket values that would escape the artifacts directory.
func validateTicketPath(ticket string) error {
	if ticket != filepath.Base(ticket) || ticket == ".." || ticket == "." {
		return fmt.Errorf("invalid ticket %q: must not contain path separators", ticket)
	}
	return nil
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

// resolvePhaseRef resolves a --from/--retry value to a 0-based phase index.
// It accepts a 1-indexed number (e.g., "3") or a phase name (e.g., "implement").
// Numbers take precedence over names — a phase literally named "3" would be
// resolved as numeric index 3, not as a name lookup.
// Returns the 0-based index, or an error if the value is invalid.
func resolvePhaseRef(value string, phases []config.Phase) (int, error) {
	// Try as a number first (numbers take precedence over names)
	if n, err := strconv.Atoi(value); err == nil {
		if n < 1 || n > len(phases) {
			return 0, fmt.Errorf("%d is out of range (1-%d)", n, len(phases))
		}
		return n - 1, nil
	}

	// Try as a phase name
	for i, p := range phases {
		if p.Name == value {
			return i, nil
		}
	}

	// Unknown name — list available phases
	names := make([]string, len(phases))
	for i, p := range phases {
		names[i] = p.Name
	}
	return 0, fmt.Errorf("unknown phase %q — available phases: %s", value, strings.Join(names, ", "))
}
