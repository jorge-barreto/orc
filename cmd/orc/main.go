package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
			&cli.StringFlag{Name: "workflow", Aliases: []string{"w"}, Usage: "Select a named workflow from .orc/workflows/"},
		},
		Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
			if cmd.Bool("no-color") || os.Getenv("NO_COLOR") != "" || !ux.IsTerminal(os.Stdout) {
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
			historyCmd(),
			statsCmd(),
			evalCmd(),
			reportCmd(),
			doctorCmd(),
			docsCmd(),
			improveCmd(),
			testCmd(),
			debugCmd(),
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
		UsageText: "orc run PROJ-123\n   orc run PROJ-123 --auto --verbose\n   orc run PROJ-123 --retry implement\n   orc run PROJ-123 --resume\n   orc run PROJ-123 --step",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "auto", Usage: "Unattended mode — skip gates, no interactive steering"},
			&cli.StringFlag{Name: "retry", Usage: "Retry from phase number or name"},
			&cli.StringFlag{Name: "from", Usage: "Start from phase number or name"},
			&cli.BoolFlag{Name: "dry-run", Usage: "Print phase plan without executing"},
			&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Save raw stream-json output to .stream.jsonl files"},
			&cli.BoolFlag{Name: "resume", Usage: "Resume an interrupted agent phase using saved session"},
			&cli.BoolFlag{Name: "step", Usage: "Step-through mode — pause after each phase for inspection"},
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

			flagWorkflow := cmd.Root().String("workflow")
			args := cmd.Args().Slice()
			if len(args) == 0 {
				return cfgErr(fmt.Errorf("ticket argument is required"))
			}

			// Positional disambiguation: if first arg matches a workflow name and
			// there's a second arg, treat first as workflow name.
			var ticket string
			if len(args) >= 2 && flagWorkflow == "" {
				if _, found := resolveWorkflowByName(projectRoot, args[0]); found {
					flagWorkflow = args[0]
					ticket = args[1]
				} else {
					ticket = args[0]
				}
			} else {
				ticket = args[0]
			}

			if ticket == "" {
				return cfgErr(fmt.Errorf("ticket argument is required"))
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

			artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflowName, ticket)

			env := &dispatch.Environment{
				ProjectRoot:       projectRoot,
				WorkDir:           projectRoot,
				ArtifactsDir:      artifactsDir,
				Ticket:            ticket,
				Workflow:          workflowName,
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
			st.SetTicket(ticket)
			st.SetWorkflow(workflowName)
			st.SetStatus(state.StatusRunning)

			// Handle --retry, --from, and --resume (mutually exclusive)
			retryVal := cmd.String("retry")
			fromVal := cmd.String("from")
			resumeFlag := cmd.Bool("resume")
			if retryVal != "" && fromVal != "" {
				return cfgErr(fmt.Errorf("--retry and --from are mutually exclusive"))
			}
			if resumeFlag && (retryVal != "" || fromVal != "") {
				return cfgErr(fmt.Errorf("--resume is mutually exclusive with --retry and --from"))
			}
			if retryVal != "" {
				idx, err := config.ResolvePhaseRef(retryVal, cfg.Phases)
				if err != nil {
					return cfgErr(fmt.Errorf("--retry: %w", err))
				}
				st.SetPhase(idx)
			}
			if fromVal != "" {
				idx, err := config.ResolvePhaseRef(fromVal, cfg.Phases)
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

			// Handle --resume: validate and thread session ID
			if resumeFlag {
				if st.GetSessionID() == "" {
					return cfgErr(fmt.Errorf("no interrupted agent session to resume (use --retry to restart the phase)"))
				}
				env.ResumeSessionID = st.GetSessionID()
			}

			stepMode := cmd.Bool("step")
			if stepMode && cmd.Bool("auto") {
				return cfgErr(fmt.Errorf("--step and --auto are mutually exclusive (step-through requires interactive input)"))
			}

			if err := dispatch.Preflight(cfg.Phases); err != nil {
				return cfgErr(err)
			}

			r := &runner.Runner{
				Config:       cfg,
				State:        st,
				Env:          env,
				Dispatcher:   &dispatch.DefaultDispatcher{},
				StepMode:     stepMode,
				HistoryLimit: cfg.HistoryLimit,
			}

			// Handle --dry-run
			if cmd.Bool("dry-run") {
				r.DryRunPrint()
				return nil
			}

			// Archive stale artifacts from a prior run before saving fresh state.
			// Must happen before st.Save() overwrites the on-disk state.
			// Only fires for genuinely stale state, not --resume/--retry/--from.
			if !resumeFlag && retryVal == "" && fromVal == "" && state.HasState(artifactsDir) {
				existing, existErr := state.Load(artifactsDir)
				if existErr == nil {
					if shouldArchiveStale(existing.GetStatus()) {
						if _, archiveErr := state.ArchiveRun(artifactsDir); archiveErr != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to archive stale run: %v\n", archiveErr)
						}
						state.PruneHistory(artifactsDir, cfg.HistoryLimit)
					}
				}
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
		Usage:     "Cancel a ticket and archive artifacts to history (use --purge to remove everything)",
		ArgsUsage: "<ticket>",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "force", Usage: "Cancel even if a run appears active"},
			&cli.BoolFlag{Name: "purge", Usage: "Remove all artifacts including history"},
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

			flagWorkflow := cmd.Root().String("workflow")
			workflowName, configPath, err := resolveWorkflow(projectRoot, flagWorkflow)
			if err != nil {
				return err
			}

			artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflowName, ticket)

			// Check if artifacts directory exists
			if _, err := os.Stat(artifactsDir); os.IsNotExist(err) {
				fmt.Printf("Nothing to cancel for ticket %s (no artifacts found).\n", ticket)
				return nil
			}

			// Check if there's a current run state to cancel
			if !state.HasState(artifactsDir) {
				if cmd.Bool("purge") {
					// Still honor --purge even without current state (cleans up history/)
					// Rotate audit dir first so cost/timing data is preserved
					auditDir := state.AuditDirForWorkflow(projectRoot, workflowName, ticket)
					if _, err := os.Stat(auditDir); err == nil {
						ts := time.Now().Format("060102-150405")
						rotated := filepath.Join(state.AuditBaseDirForWorkflow(projectRoot, workflowName), fmt.Sprintf("%s-%s", ticket, ts))
						if err := os.Rename(auditDir, rotated); err != nil {
							fmt.Fprintf(os.Stderr, "warning: failed to rotate audit dir: %v\n", err)
						}
					}
					if err := os.RemoveAll(artifactsDir); err != nil {
						return fmt.Errorf("removing artifacts: %w", err)
					}
					fmt.Printf("%s✓ Cancelled ticket %s — all artifacts purged%s\n", ux.Green, ticket, ux.Reset)
					return nil
				}
				fmt.Printf("Nothing to cancel for ticket %s (no active run found).\n", ticket)
				return nil
			}

			// Load state to validate ticket and check status
			st, err := state.Load(artifactsDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			if st.GetTicket() != "" && st.GetTicket() != ticket {
				return fmt.Errorf("state is for ticket %q, not %q", st.GetTicket(), ticket)
			}

			if st.GetStatus() == state.StatusRunning && !cmd.Bool("force") {
				return fmt.Errorf("ticket %s appears to be running — Ctrl+C the process first, or use --force", ticket)
			}

			archiveOK := true
			if cmd.Bool("purge") {
				if err := os.RemoveAll(artifactsDir); err != nil {
					return fmt.Errorf("removing artifacts: %w", err)
				}
			} else {
				if _, err := state.ArchiveRun(artifactsDir); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to archive run to history: %v\n", err)
					archiveOK = false
				}
				limit := 10
				if configPath != "" {
					if cfg, loadErr := config.Load(configPath, projectRoot); loadErr == nil {
						limit = cfg.HistoryLimit
					}
				}
				if pruneErr := state.PruneHistory(artifactsDir, limit); pruneErr != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to prune history: %v\n", pruneErr)
				}
			}

			// Rotate audit dir so it's preserved but distinguishable from future runs
			auditDir := state.AuditDirForWorkflow(projectRoot, workflowName, ticket)
			if _, err := os.Stat(auditDir); err == nil {
				ts := time.Now().Format("060102-150405")
				rotated := filepath.Join(state.AuditBaseDirForWorkflow(projectRoot, workflowName), fmt.Sprintf("%s-%s", ticket, ts))
				if err := os.Rename(auditDir, rotated); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to rotate audit dir: %v\n", err)
				}
			}

			if cmd.Bool("purge") {
				fmt.Printf("%s✓ Cancelled ticket %s — all artifacts purged%s\n", ux.Green, ticket, ux.Reset)
			} else if archiveOK {
				fmt.Printf("%s✓ Cancelled ticket %s — artifacts archived to history%s\n", ux.Green, ticket, ux.Reset)
			} else {
				fmt.Printf("%s✓ Cancelled ticket %s — artifacts left in place (archive failed)%s\n", ux.Green, ticket, ux.Reset)
			}
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

			ticket := cmd.Args().First()

			// No argument: show all tickets across all workflows
			if ticket == "" {
				// All-tickets view: use empty config so phase display is generic
				// ("phase N") rather than showing wrong phase names for tickets
				// from non-default workflows. Full phase names are available
				// via `orc status <ticket>`.
				cfg := &config.Config{}

				baseDir := filepath.Join(projectRoot, ".orc", "artifacts")
				baseAuditDir := state.AuditBaseDir(projectRoot)
				tickets, err := state.ListTickets(baseDir, baseAuditDir)
				if err != nil {
					return fmt.Errorf("listing tickets: %w", err)
				}
				ux.RenderStatusAll(cfg, tickets)
				return nil
			}

			// Single ticket view
			if err := validateTicketPath(ticket); err != nil {
				return err
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

			artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflowName, ticket)
			stateDir, err := state.ResolveStateDir(artifactsDir)
			if err != nil {
				return fmt.Errorf("no run found for ticket %s", ticket)
			}
			st, err := state.Load(stateDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			auditDir := state.AuditDirForWorkflow(projectRoot, workflowName, ticket)
			ux.RenderStatus(cfg, st, stateDir, auditDir)
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

			flagWorkflow := cmd.Root().String("workflow")
			workflowName, configPath, err := resolveWorkflow(projectRoot, flagWorkflow)
			if err != nil {
				return err
			}

			cfg, err := config.Load(configPath, projectRoot)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			artifactsDir := state.ArtifactsDirForWorkflow(projectRoot, workflowName, ticket)
			auditDir := state.AuditDirForWorkflow(projectRoot, workflowName, ticket)
			stateDir, err := state.ResolveStateDir(artifactsDir)
			if err != nil {
				return fmt.Errorf("no run found for ticket %s", ticket)
			}
			st, err := state.Load(stateDir)
			if err != nil {
				return fmt.Errorf("loading state: %w", err)
			}

			return doctor.Run(ctx, auditDir, stateDir, cfg, st)
		},
	}
}

func initCmd() *cli.Command {
	return &cli.Command{
		Name:      "init",
		Usage:     "Initialize a new .orc/ directory with AI-generated or recipe-based workflow config",
		ArgsUsage: "[description]",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "recipe", Usage: "Scaffold from a recipe (simple, standard, full-pipeline, review-loop)"},
			&cli.BoolFlag{Name: "list-recipes", Usage: "Show available recipes"},
			&cli.StringFlag{Name: "add-workflow", Usage: "Add a named workflow to an existing .orc/ project"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.Bool("list-recipes") {
				scaffold.ListRecipes()
				return nil
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			if wfName := cmd.String("add-workflow"); wfName != "" {
				return scaffold.InitWorkflow(dir, wfName, cmd.String("recipe"))
			}
			if recipe := cmd.String("recipe"); recipe != "" {
				return scaffold.InitRecipe(dir, recipe)
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

// shouldArchiveStale reports whether a prior run with the given status should be
// archived before starting a fresh run. All known statuses and unknown statuses
// return true — archive rather than silently discard.
func shouldArchiveStale(status string) bool {
	switch status {
	case state.StatusCompleted, state.StatusRunning, state.StatusFailed, state.StatusInterrupted:
		return true
	default:
		return true // safe default: archive unknown states rather than silently discard
	}
}

// validateTicketPath rejects ticket values that would escape the artifacts directory.
func validateTicketPath(ticket string) error {
	if ticket != filepath.Base(ticket) || ticket == ".." || ticket == "." {
		return fmt.Errorf("invalid ticket %q: must not contain path separators", ticket)
	}
	return nil
}

// findProjectRoot walks up from cwd looking for .orc/config.yaml or .orc/workflows/.
func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		// Check for config.yaml (standard)
		configPath := filepath.Join(dir, ".orc", "config.yaml")
		if _, err := os.Stat(configPath); err == nil {
			return dir, nil
		}
		// Check for workflows/ directory (multi-workflow without config.yaml)
		workflowsDir := filepath.Join(dir, ".orc", "workflows")
		if info, err := os.Stat(workflowsDir); err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no .orc/ project found (searched from cwd to root)")
		}
		dir = parent
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// discoverWorkflows returns workflow names from .orc/workflows/*.yaml/*.yml.
// Returns nil if the directory doesn't exist (single-config mode).
func discoverWorkflows(projectRoot string) []string {
	workflowsDir := filepath.Join(projectRoot, ".orc", "workflows")
	entries, err := os.ReadDir(workflowsDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			names = append(names, strings.TrimSuffix(strings.TrimSuffix(name, ".yaml"), ".yml"))
		}
	}
	return names
}

// resolveWorkflow determines which workflow to use.
// Returns (workflowName, configPath, error).
// workflowName is empty for single-config flat layout.
func resolveWorkflow(projectRoot, flagWorkflow string) (workflowName, configPath string, err error) {
	configYAML := filepath.Join(projectRoot, ".orc", "config.yaml")
	hasConfig := fileExists(configYAML)
	workflows := discoverWorkflows(projectRoot)

	if len(workflows) == 0 {
		if flagWorkflow != "" {
			return "", "", fmt.Errorf("--workflow specified but no .orc/workflows/ directory found")
		}
		if !hasConfig {
			return "", "", fmt.Errorf("no .orc/config.yaml or .orc/workflows/ found")
		}
		return "", configYAML, nil
	}

	// Multi-workflow mode
	if flagWorkflow != "" {
		if flagWorkflow != filepath.Base(flagWorkflow) || flagWorkflow == ".." || flagWorkflow == "." {
			return "", "", fmt.Errorf("invalid workflow name %q: must not contain path separators", flagWorkflow)
		}
		path := filepath.Join(projectRoot, ".orc", "workflows", flagWorkflow+".yaml")
		if !fileExists(path) {
			path = filepath.Join(projectRoot, ".orc", "workflows", flagWorkflow+".yml")
			if !fileExists(path) {
				return "", "", fmt.Errorf("workflow %q not found — available: %s", flagWorkflow, formatWorkflowList(hasConfig, workflows))
			}
		}
		return flagWorkflow, path, nil
	}

	// No explicit workflow — resolve default
	if !hasConfig && len(workflows) == 1 {
		name := workflows[0]
		path := filepath.Join(projectRoot, ".orc", "workflows", name+".yaml")
		if !fileExists(path) {
			path = filepath.Join(projectRoot, ".orc", "workflows", name+".yml")
		}
		return name, path, nil
	}

	if hasConfig {
		return "default", configYAML, nil
	}

	return "", "", fmt.Errorf("multiple workflows found, specify one with -w: %s", strings.Join(workflows, ", "))
}

// resolveWorkflowByName looks up a specific workflow name and returns its config path.
func resolveWorkflowByName(projectRoot, name string) (string, bool) {
	if name != filepath.Base(name) || name == ".." || name == "." {
		return "", false
	}
	path := filepath.Join(projectRoot, ".orc", "workflows", name+".yaml")
	if fileExists(path) {
		return path, true
	}
	path = filepath.Join(projectRoot, ".orc", "workflows", name+".yml")
	if fileExists(path) {
		return path, true
	}
	return "", false
}

func formatWorkflowList(hasConfig bool, workflows []string) string {
	var all []string
	if hasConfig {
		all = append(all, "default (config.yaml)")
	}
	all = append(all, workflows...)
	return strings.Join(all, ", ")
}
