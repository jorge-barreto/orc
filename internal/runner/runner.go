package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

// errStepRewind is returned by runParallel when the user chooses to rewind
// in step-through mode. The caller uses this to distinguish rewind from
// normal completion, ensuring post-success logic is not executed on rewind.
var errStepRewind = errors.New("step rewind")

// Runner drives the workflow state machine.
type Runner struct {
	Config       *config.Config
	State        *state.State
	Env          *dispatch.Environment
	Dispatcher   dispatch.Dispatcher
	Timing       *state.Timing
	Costs        *state.CostData
	StepMode     bool
	HistoryLimit int
	StepPromptFn func(artifactsDir string, phaseIdx int, phaseName string) ux.StepAction
	RePromptFn   func(ctx context.Context, phase config.Phase, env *dispatch.Environment, prompt, sessionID string) (*dispatch.Result, error)
	skipped      map[string]bool
	auditDir     string
	baseCommit   string
	attemptCount map[int]int // tracks phase attempts (includes pre-run hook failures where dispatch was skipped)
}

// appendPhaseLog appends a message to the phase log file.
// Errors are silently ignored — logging should not break the run.
func appendPhaseLog(artifactsDir string, phaseIdx int, msg string) {
	f, err := os.OpenFile(state.LogPath(artifactsDir, phaseIdx), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprint(f, msg)
}

// writePhaseMetadata writes structured metadata for a completed phase.
// Errors are logged as warnings — metadata should not break the run.
func writePhaseMetadata(artifactsDir string, phaseIdx int, meta *state.PhaseMetadata) {
	if err := state.SaveMetadata(state.MetaPath(artifactsDir, phaseIdx), meta); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write phase metadata: %v\n", err)
	}
}

// buildPhaseMetadata constructs a PhaseMetadata from phase config and dispatch result.
func buildPhaseMetadata(phase config.Phase, phaseIdx int, result *dispatch.Result, start time.Time, end time.Time) *state.PhaseMetadata {
	meta := &state.PhaseMetadata{
		PhaseName:    phase.Name,
		PhaseType:    phase.Type,
		PhaseIndex:   phaseIdx,
		StartTime:    start,
		EndTime:      end,
		DurationSecs: end.Sub(start).Seconds(),
	}
	if phase.Type == "agent" {
		meta.Model = phase.Model
		meta.Effort = phase.Effort
	}
	if result != nil {
		meta.ExitCode = result.ExitCode
		meta.TimedOut = result.TimedOut
		meta.SessionID = result.SessionID
		meta.CostUSD = result.CostUSD
		meta.InputTokens = result.InputTokens
		meta.OutputTokens = result.OutputTokens
		meta.ToolsUsed = result.ToolsUsed
		meta.ToolsDenied = result.ToolsDenied
	}
	return meta
}

// failAndHint sets the failure status, saves state (warning on error),
// flushes timing, prints a resume hint, and returns the given error.
func (r *Runner) failAndHint(status string, exitCode int, err error) error {
	r.State.SetStatus(status)
	if saveErr := r.State.Save(r.Env.ArtifactsDir); saveErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save state: %v\n", saveErr)
	}
	if r.auditDir != "" {
		if saveErr := r.State.Save(r.auditDir); saveErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save audit state: %v\n", saveErr)
		}
	}
	if r.Timing != nil {
		if flushErr := r.Timing.Flush(r.auditDir); flushErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to flush timing: %v\n", flushErr)
		}
		if flushErr := r.Timing.Flush(r.Env.ArtifactsDir); flushErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to flush timing to artifacts: %v\n", flushErr)
		}
	}
	if r.Costs != nil {
		if flushErr := r.Costs.Flush(r.auditDir); flushErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to flush costs: %v\n", flushErr)
		}
		if flushErr := r.Costs.Flush(r.Env.ArtifactsDir); flushErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to flush costs to artifacts: %v\n", flushErr)
		}
	}
	ux.ResumeHint(r.State.GetTicket(), r.State.GetSessionID() != "")
	failedPhase := ""
	idx := r.State.GetPhaseIndex()
	if idx < len(r.Config.Phases) {
		failedPhase = r.Config.Phases[idx].Name
	}
	r.writeRunResult(exitCode, failedPhase)
	return &ExitError{Code: exitCode, Err: err}
}

// failWithCategory sets the failure category and detail, then delegates to failAndHint.
func (r *Runner) failWithCategory(status string, exitCode int, category, detail string, err error) error {
	r.State.SetFailure(category, detail)
	return r.failAndHint(status, exitCode, err)
}

// printRunSummary prints the run summary table if timing data is available.
func (r *Runner) printRunSummary(failedPhase int) {
	if r.Timing == nil {
		return
	}
	ux.RunSummary(r.Config.Phases, r.Timing, failedPhase, r.skipped)
}

// captureBaseCommit returns the current HEAD short hash, or empty string on failure.
func captureBaseCommit(projectRoot string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// buildRunResult constructs the RunResult struct, including collecting commits via git log.
func (r *Runner) buildRunResult(exitCode int, failedPhase string) *state.RunResult {
	var failedPhasePtr *string
	if failedPhase != "" {
		failedPhasePtr = &failedPhase
	}

	totalDuration := 0.0
	if r.Timing != nil {
		totalDuration = r.Timing.TotalElapsed().Seconds()
	}

	totalCost := 0.0
	if r.Costs != nil {
		totalCost = r.Costs.TotalCost()
	}

	result := &state.RunResult{
		Ticket:               r.Env.Ticket,
		Workflow:             r.Env.Workflow,
		Status:               r.State.GetStatus(),
		ExitCode:             exitCode,
		FailedPhase:          failedPhasePtr,
		PhasesCompleted:      r.State.GetPhaseIndex(),
		PhasesTotal:          len(r.Config.Phases),
		TotalCostUSD:         totalCost,
		TotalDurationSeconds: totalDuration,
		Commits:              state.CollectCommits(r.Env.ProjectRoot, r.baseCommit),
		ArtifactsDir:         r.Env.ArtifactsDir,
	}
	result.Phases = r.buildPhaseResults(failedPhase)
	return result
}

// writeRunResult builds and writes run-result.json to the audit and artifacts directories.
// Returns the built result for optional reuse (e.g., restoring after archive).
func (r *Runner) writeRunResult(exitCode int, failedPhase string) *state.RunResult {
	result := r.buildRunResult(exitCode, failedPhase)
	if r.auditDir != "" {
		if err := state.WriteRunResult(r.auditDir, result); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to write run-result.json to audit: %v\n", err)
		}
	}
	if err := state.WriteRunResult(r.Env.ArtifactsDir, result); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to write run-result.json: %v\n", err)
	}
	return result
}

// buildPhaseResults assembles per-phase results from timing, cost, and status data.
func (r *Runner) buildPhaseResults(failedPhase string) []state.PhaseResult {
	phaseIndex := r.State.GetPhaseIndex()
	results := make([]state.PhaseResult, 0, len(r.Config.Phases))

	// Build duration map: sum all timing entries per phase (loops may repeat a phase)
	durations := make(map[string]float64)
	if r.Timing != nil {
		for _, e := range r.Timing.Entries() {
			if !e.End.IsZero() {
				durations[e.Phase] += e.End.Sub(e.Start).Seconds()
			}
		}
	}

	for i, phase := range r.Config.Phases {
		var status string
		switch {
		case r.skipped != nil && r.skipped[phase.Name]:
			status = "skipped"
		case failedPhase == phase.Name:
			status = "failed"
		case i < phaseIndex:
			status = "completed"
		default:
			status = "pending"
		}

		costUSD := 0.0
		if r.Costs != nil {
			costUSD = r.Costs.PhaseCost(phase.Name)
		}

		results = append(results, state.PhaseResult{
			Name:            phase.Name,
			Status:          status,
			DurationSeconds: durations[phase.Name],
			CostUSD:         costUSD,
		})
	}
	return results
}

// Run executes the workflow from the current state.
func (r *Runner) Run(ctx context.Context) error {
	setupErr := func(err error) error {
		return &ExitError{Code: ExitConfigError, Err: err}
	}

	if err := state.EnsureDir(r.Env.ArtifactsDir); err != nil {
		return setupErr(err)
	}

	// Initialize audit dir for costs, timing, and log archives
	r.auditDir = state.AuditDirForWorkflow(r.Env.ProjectRoot, r.Env.Workflow, r.Env.Ticket)
	if err := os.MkdirAll(r.auditDir, 0755); err != nil {
		return setupErr(fmt.Errorf("creating audit dir: %w", err))
	}

	loopCounts, err := state.LoadLoopCounts(r.Env.ArtifactsDir)
	if err != nil {
		return setupErr(fmt.Errorf("loading loop counts: %w", err))
	}

	timing, err := state.LoadTiming(r.auditDir)
	if err != nil {
		return setupErr(fmt.Errorf("loading timing: %w", err))
	}
	r.Timing = timing

	costs, err := state.LoadCosts(r.auditDir)
	if err != nil {
		return setupErr(fmt.Errorf("loading costs: %w", err))
	}
	r.Costs = costs
	r.skipped = make(map[string]bool)

	attemptCounts, err := state.LoadAttemptCounts(r.auditDir)
	if err != nil {
		return setupErr(fmt.Errorf("loading attempt counts: %w", err))
	}
	r.attemptCount = attemptCounts
	r.baseCommit = captureBaseCommit(r.Env.ProjectRoot)

	total := len(r.Config.Phases)

mainLoop:
	for r.State.GetPhaseIndex() < total {
		i := r.State.GetPhaseIndex()
		phase := r.Config.Phases[i]

		// Check for context cancellation
		if ctx.Err() != nil {
			if i > 0 {
				r.printRunSummary(-1)
			}
			return r.failWithCategory(state.StatusInterrupted, ExitInterrupted, state.FailCategoryInterrupted, ctx.Err().Error(), ctx.Err())
		}

		// Check run-level cost limit before starting next phase
		if r.Config.MaxCost > 0 && r.Costs.TotalCost() > r.Config.MaxCost {
			r.printRunSummary(i)
			return r.failWithCategory(state.StatusFailed, ExitCostLimit, state.FailCategoryCostOverrun,
				fmt.Sprintf("run cost $%.2f exceeded limit $%.2f", r.Costs.TotalCost(), r.Config.MaxCost),
				fmt.Errorf("run exceeded cost limit: $%.2f > $%.2f", r.Costs.TotalCost(), r.Config.MaxCost))
		}

		// Evaluate condition
		if phase.Condition != "" {
			if !evalCondition(ctx, phase, r.Env) {
				ux.PhaseSkip(i, phase.Name)
				r.skipped[phase.Name] = true
				r.State.Advance()
				if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
					return fmt.Errorf("saving state after skip: %w", err)
				}
				continue
			}
		}

		// Handle parallel-with
		if phase.ParallelWith != "" {
			partnerIdx := r.Config.PhaseIndex(phase.ParallelWith)
			if partnerIdx < 0 {
				return r.failAndHint(state.StatusFailed, ExitConfigError, fmt.Errorf("phase %q: parallel-with %q not found", phase.Name, phase.ParallelWith))
			}
			if partnerIdx > i {
				err := r.runParallel(ctx, i, partnerIdx, total, loopCounts)
				if err == errStepRewind {
					continue
				}
				if err != nil {
					return err
				}
				continue
			}
		}

		// Normal dispatch
		ux.PhaseHeader(i, total, phase)
		start := time.Now()
		r.Timing.AddStartAt(phase.Name, start)

		r.Env.PhaseIndex = i
		var result *dispatch.Result
		var err error
		switch phase.Type {
		case "workflow":
			result, err = dispatch.DispatchWithHooks(ctx, phase, r.Env, r.dispatchWorkflow)
		case "branch":
			result, err = dispatch.DispatchWithHooks(ctx, phase, r.Env, r.dispatchBranch)
		default:
			result, err = r.dispatchWithHooks(ctx, phase, r.Env)
		}

		// Persist session ID immediately so it survives interruption.
		// Must happen before any error handling — if the process dies
		// during error handling, the session ID is still on disk.
		if phase.Type == "agent" && result != nil && result.SessionID != "" {
			r.State.SetSessionID(result.SessionID)
			if saveErr := r.State.Save(r.Env.ArtifactsDir); saveErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to save session ID: %v\n", saveErr)
			}
		}

		// Clear resume session ID after first dispatch (don't resume again on loop iterations)
		r.Env.ResumeSessionID = ""

		// Handle rate-limit: policy-driven (on-rate-limit: wait|exit; default exit).
		if result != nil && result.RateLimited {
			// Always record partial cost first, so the audit trail captures
			// whatever was spent before rejection regardless of policy.
			if phase.Type == "agent" && result != nil {
				r.Costs.Record(phase.Name, i, result.CostUSD, result.InputTokens, result.OutputTokens, result.CacheCreationInputTokens, result.CacheReadInputTokens, result.Turns)
				if flushErr := r.Costs.Flush(r.auditDir); flushErr != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to flush costs: %v\n", flushErr)
				}
			}

			// Resolve policy: phase overrides config; config defaults to exit.
			policy := phase.OnRateLimit
			if policy == "" {
				policy = r.Config.OnRateLimit
			}
			if policy == "" {
				policy = "exit"
			}

			// Interactive mode (!--auto) can't usefully wait regardless of policy.
			// It always exits — but with the new ExitRateLimit code for observability.
			if !r.Env.AutoMode {
				r.Timing.AddEnd(phase.Name)
				resetTime := time.Unix(result.RateLimitResetAt, 0)
				ux.RateLimitHint(resetTime)
				r.printRunSummary(i)
				return r.failWithCategory(state.StatusFailed, ExitRateLimit, state.FailCategoryRateLimit,
					fmt.Sprintf("usage limit reached, resets at %s", resetTime.Format("15:04")),
					fmt.Errorf("phase %q: rate limited", phase.Name))
			}

			if policy == "exit" {
				r.Timing.AddEnd(phase.Name)
				resetTime := time.Unix(result.RateLimitResetAt, 0)
				ux.RateLimitHint(resetTime)
				r.printRunSummary(i)
				return r.failWithCategory(state.StatusFailed, ExitRateLimit, state.FailCategoryRateLimit,
					fmt.Sprintf("usage limit reached, resets at %s (on-rate-limit: exit)", resetTime.Format("15:04")),
					fmt.Errorf("phase %q: rate limited (on-rate-limit: exit)", phase.Name))
			}

			// policy == "wait": check phase cost budget before waiting.
			if phase.MaxCost > 0 && r.Costs != nil {
				phaseCost := r.Costs.PhaseCost(phase.Name)
				if phaseCost > phase.MaxCost {
					r.Timing.AddEnd(phase.Name)
					r.printRunSummary(i)
					return r.failWithCategory(state.StatusFailed, ExitCostLimit, state.FailCategoryCostOverrun,
						fmt.Sprintf("phase %q cost $%.2f exceeded limit $%.2f (rate-limited, not waiting)", phase.Name, phaseCost, phase.MaxCost),
						fmt.Errorf("phase %q exceeded cost limit while rate-limited: $%.2f > $%.2f", phase.Name, phaseCost, phase.MaxCost))
				}
			}

			// Validate resetsAt before waiting.
			if result.RateLimitResetAt <= 0 {
				r.Timing.AddEnd(phase.Name)
				r.printRunSummary(i)
				return r.failWithCategory(state.StatusFailed, ExitRateLimit, state.FailCategoryRateLimit,
					"rate limited but resetsAt missing or invalid — cannot wait",
					fmt.Errorf("phase %q: rate limited with invalid resetsAt", phase.Name))
			}

			// Wait for rate limit to reset.
			if waitErr := r.waitForRateLimit(ctx, phase.Name, result.RateLimitResetAt); waitErr != nil {
				r.printRunSummary(i)
				return r.failWithCategory(state.StatusInterrupted, ExitInterrupted, state.FailCategoryInterrupted, waitErr.Error(), waitErr)
			}

			// Set up resume with the session ID from the rate-limited run.
			r.Env.ResumeSessionID = result.SessionID
			continue
		}

		// Write structured metadata before archiving
		end := time.Now()
		writePhaseMetadata(r.Env.ArtifactsDir, i, buildPhaseMetadata(phase, i, result, start, end))

		// Archive every attempt to audit (before any error/interrupt handling)
		r.attemptCount[i]++
		archivePhaseFiles(r.Env.ArtifactsDir, r.auditDir, i, r.attemptCount[i], phase.Outputs)
		if saveErr := state.SaveAttemptCounts(r.auditDir, r.attemptCount); saveErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save attempt counts: %v\n", saveErr)
		}

		if ctx.Err() != nil {
			r.Timing.AddEnd(phase.Name)
			appendPhaseLog(r.Env.ArtifactsDir, i, fmt.Sprintf("\n[orc] phase interrupted: %v\n", ctx.Err()))
			r.printRunSummary(i)
			return r.failWithCategory(state.StatusInterrupted, ExitInterrupted, state.FailCategoryInterrupted, ctx.Err().Error(), ctx.Err())
		}

		// Record cost data for agent/workflow/branch phases (cost is incurred regardless of success/failure)
		if (phase.Type == "agent" || phase.Type == "workflow" || phase.Type == "branch") && result != nil {
			r.Costs.Record(phase.Name, i, result.CostUSD, result.InputTokens, result.OutputTokens, result.CacheCreationInputTokens, result.CacheReadInputTokens, result.Turns)
			// Warn if agent completed but reported no token counts (best-effort tracking).
			// Skip this warning on cost-overrun kill — the result event never arrives
			// when we terminate the stream early.
			if result.InputTokens == 0 && result.OutputTokens == 0 && !result.CostOverrun {
				fmt.Fprintf(os.Stderr, "  note: no token counts in stream output for phase %q (token tracking is best-effort)\n", phase.Name)
			}
			if flushErr := r.Costs.Flush(r.auditDir); flushErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to flush costs: %v\n", flushErr)
			}
			// In-flight cost monitor tripped: subprocess was killed mid-stream.
			// Fail fast — after a kill the result event may never arrive, so
			// the recorded cost could be zero and the post-hoc check below
			// would miss the overrun.
			if result.CostOverrun {
				r.Timing.AddEnd(phase.Name)
				r.printRunSummary(i)
				return r.failWithCategory(state.StatusFailed, ExitCostLimit, state.FailCategoryCostOverrun,
					fmt.Sprintf("phase %q killed mid-stream — cost estimate exceeded limit $%.4f", phase.Name, phase.MaxCost),
					fmt.Errorf("phase %q cost estimate exceeded limit $%.4f (killed mid-stream)", phase.Name, phase.MaxCost))
			}
			// Check phase-level cost limit (post-hoc safety net)
			if phase.MaxCost > 0 {
				phaseCost := r.Costs.PhaseCost(phase.Name)
				if phaseCost > phase.MaxCost {
					r.Timing.AddEnd(phase.Name)
					r.printRunSummary(i)
					return r.failWithCategory(state.StatusFailed, ExitCostLimit, state.FailCategoryCostOverrun,
						fmt.Sprintf("phase %q cost $%.2f exceeded limit $%.2f", phase.Name, phaseCost, phase.MaxCost),
						fmt.Errorf("phase %q exceeded cost limit: $%.2f > $%.2f", phase.Name, phaseCost, phase.MaxCost))
				}
			}
		}

		if err != nil || (result != nil && result.ExitCode != 0) {
			r.Timing.AddEnd(phase.Name)
			errMsg := fmt.Sprintf("%s exited with non-zero status", phase.Type)
			if result != nil && result.TimedOut {
				errMsg = fmt.Sprintf("timed out after %dm — consider increasing 'timeout' in config (current: %d)", phase.Timeout, phase.Timeout)
			} else if err != nil {
				errMsg = err.Error()
			}
			appendPhaseLog(r.Env.ArtifactsDir, i, fmt.Sprintf("\n[orc] phase %q failed: %s\n", phase.Name, errMsg))
			ux.PhaseFail(i, phase.Name, errMsg)
			if phase.Type == "agent" {
				fmt.Fprintf(os.Stderr, "  hint: if the agent couldn't perform actions, check your .claude/settings.local.json permissions\n")
			}

			// Handle loop
			if phase.Loop != nil {
				output := state.ReadDeclaredOutputs(r.Env.ArtifactsDir, phase.Outputs)
				if output == "" && result != nil {
					output = result.Output
				}
				if output == "" {
					output = errMsg
				}
				shouldContinue, loopErr := r.handleLoopFailure(i, phase, loopCounts, output)
				if loopErr != nil {
					return loopErr
				}
				if shouldContinue {
					continue
				}
			}

			// No loop: stop
			exitCode := ExitPhaseFailure
			category := state.FailCategoryScriptFailure
			if result != nil && result.TimedOut {
				exitCode = ExitTimeout
				category = state.FailCategoryTimeout
			} else if phase.Type == "agent" {
				category = state.FailCategoryAgentError
			} else if phase.Type == "gate" {
				category = state.FailCategoryGateRejection
			} else if phase.Type == "workflow" || phase.Type == "branch" {
				category = state.FailCategoryWorkflowError
			}
			r.printRunSummary(i)
			return r.failWithCategory(state.StatusFailed, exitCode, category, errMsg, fmt.Errorf("phase %q failed", phase.Name))
		}

		// Check declared outputs
		if len(phase.Outputs) > 0 {
			missing := state.CheckOutputs(r.Env.ArtifactsDir, phase.Outputs)
			if len(missing) > 0 && phase.Type == "agent" {
				// Resume the agent session once for missing outputs
				var paths []string
				for _, m := range missing {
					paths = append(paths, filepath.Join(r.Env.ArtifactsDir, m))
				}
				prompt := fmt.Sprintf(
					"The following expected output files are missing:\n%s\nPlease produce them now.",
					strings.Join(paths, "\n"))
				sessionID := ""
				if result != nil {
					sessionID = result.SessionID
				}
				rePromptFn := r.RePromptFn
				if rePromptFn == nil {
					rePromptFn = dispatch.RunAgentWithPrompt
				}
				reStart := time.Now()
				reResult, reErr := rePromptFn(ctx, phase, r.Env, prompt, sessionID)
				reEnd := time.Now()
				if reErr != nil {
					fmt.Fprintf(os.Stderr, "warning: re-prompt for missing outputs failed: %v\n", reErr)
				}
				if reResult != nil && r.Costs != nil {
					r.Costs.Record(phase.Name, i, reResult.CostUSD, reResult.InputTokens, reResult.OutputTokens, reResult.CacheCreationInputTokens, reResult.CacheReadInputTokens, reResult.Turns)
					if flushErr := r.Costs.Flush(r.auditDir); flushErr != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to flush costs: %v\n", flushErr)
					}
				}
				// Write metadata for re-prompt dispatch
				if reResult != nil {
					writePhaseMetadata(r.Env.ArtifactsDir, i, buildPhaseMetadata(phase, i, reResult, reStart, reEnd))
				}
				r.attemptCount[i]++
				archivePhaseFiles(r.Env.ArtifactsDir, r.auditDir, i, r.attemptCount[i], phase.Outputs)
				if saveErr := state.SaveAttemptCounts(r.auditDir, r.attemptCount); saveErr != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to save attempt counts: %v\n", saveErr)
				}
				missing = state.CheckOutputs(r.Env.ArtifactsDir, phase.Outputs)
			}
			if len(missing) > 0 {
				errMsg := fmt.Sprintf("missing outputs: %v", missing)
				ux.PhaseFail(i, phase.Name, errMsg)
				if phase.Type == "agent" {
					fmt.Fprintf(os.Stderr, "  hint: if the agent couldn't perform actions, check your .claude/settings.local.json permissions\n")
				}
				r.Timing.AddEnd(phase.Name)
				r.printRunSummary(i)
				return r.failWithCategory(state.StatusFailed, ExitPhaseFailure, state.FailCategoryOutputMissing, errMsg,
					fmt.Errorf("phase %q: %s", phase.Name, errMsg))
			}
		}

		// Run loop.check if present (after phase success, before loop min enforcement)
		if phase.Loop != nil && phase.Loop.Check != "" {
			checkCode, checkOutput := runLoopCheck(ctx, phase.Loop.Check, phase, r.Env)
			if checkCode != 0 {
				// Check failed — treat as loop failure
				r.Timing.AddEnd(phase.Name)
				checkMsg := fmt.Sprintf("loop.check failed (exit %d)", checkCode)
				appendPhaseLog(r.Env.ArtifactsDir, i, fmt.Sprintf("\n[orc] %s: %s\n%s", phase.Name, checkMsg, checkOutput))
				ux.PhaseFail(i, phase.Name, checkMsg)

				feedback := state.ReadDeclaredOutputs(r.Env.ArtifactsDir, phase.Outputs)
				shouldContinue, loopErr := r.handleLoopFailure(i, phase, loopCounts, feedback)
				if loopErr != nil {
					return loopErr
				}
				if shouldContinue {
					continue
				}
			}
			// check passed — fall through to min enforcement below
		}

		// Handle loop on success — enforce min iterations
		if phase.Loop != nil {
			iteration := loopCounts[phase.Name] + 1
			loopCounts[phase.Name] = iteration

			if iteration < phase.Loop.Min {
				// min not reached — forced loop-back with success output as feedback
				gotoIdx := r.Config.PhaseIndex(phase.Loop.Goto)
				if gotoIdx < 0 {
					return r.failAndHint(state.StatusFailed, ExitConfigError,
						fmt.Errorf("phase %q: loop.goto %q not found", phase.Name, phase.Loop.Goto))
				}

				if err := r.prepareBackwardJump(gotoIdx, i, loopCounts); err != nil {
					return r.failAndHint(state.StatusFailed, ExitPhaseFailure, err)
				}
				if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
					return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving loop counts: %w", err))
				}

				output := ""
				if result != nil {
					output = result.Output
				}
				if err := state.WriteFeedback(r.Env.ArtifactsDir, phase.Name, output); err != nil {
					return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("writing feedback: %w", err))
				}

				r.Timing.AddEnd(phase.Name)
				ux.LoopBack(phase.Name, phase.Loop.Goto, iteration, phase.Loop.Max)

				r.State.SetPhase(gotoIdx)
				if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
					return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving state after loop iteration: %w", err))
				}
				continue
			}

			// iteration >= min AND pass: break out of loop, advance normally
			if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
				return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving loop counts: %w", err))
			}
			// Archive and clear feedback so downstream phases don't see stale loop feedback
			archiveAndClearFeedback(r.Env.ArtifactsDir, r.auditDir, i, r.attemptCount[i])
		}

		duration := time.Since(start)
		r.Timing.AddEnd(phase.Name)
		if err := r.Timing.Flush(r.auditDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to flush timing: %v\n", err)
		}
		r.State.Advance()
		r.State.SetStatus(state.StatusRunning)
		if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
			return fmt.Errorf("saving state after phase advance: %w", err)
		}
		ux.PhaseComplete(i, phase.Name, duration)

		// Step-through pause
		if r.StepMode {
			promptFn := r.StepPromptFn
			if promptFn == nil {
				promptFn = ux.StepPrompt
			}
			for {
				action := promptFn(r.Env.ArtifactsDir, i, phase.Name)
				switch action.Type {
				case "abort":
					r.printRunSummary(-1)
					return r.failWithCategory(state.StatusInterrupted, ExitInterrupted, state.FailCategoryInterrupted,
						"aborted by user in step-through mode",
						fmt.Errorf("aborted by user in step-through mode"))
				case "rewind":
					idx, err := config.ResolvePhaseRef(action.Target, r.Config.Phases)
					if err != nil {
						fmt.Fprintf(os.Stderr, "  invalid rewind target: %v\n", err)
						continue
					}
					if idx >= r.State.GetPhaseIndex() {
						fmt.Fprintf(os.Stderr, "  rewind target %q is not before the current phase — rewind only jumps backward\n", action.Target)
						continue
					}
					if err := r.prepareBackwardJump(idx, r.State.GetPhaseIndex(), loopCounts); err != nil {
						return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("preparing rewind to phase %d: %w", idx, err))
					}
					if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
						return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving loop counts after rewind: %w", err))
					}
					r.State.SetPhase(idx)
					if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
						return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving state after rewind: %w", err))
					}
					continue mainLoop
				case "continue":
					// fall through to next phase
				}
				break
			}
		}
	}

	r.State.SetStatus(state.StatusCompleted)
	if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
		return r.failWithCategory(state.StatusFailed, ExitInfraError, state.FailCategoryStateSave, err.Error(), fmt.Errorf("saving final state: %w", err))
	}
	if saveErr := r.State.Save(r.auditDir); saveErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save audit state: %v\n", saveErr)
	}
	if flushErr := r.Timing.Flush(r.auditDir); flushErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to flush timing to audit: %v\n", flushErr)
	}
	if flushErr := r.Costs.Flush(r.auditDir); flushErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to flush costs to audit: %v\n", flushErr)
	}
	// Flush timing and costs to artifacts dir so they are included in the archive
	if flushErr := r.Timing.Flush(r.Env.ArtifactsDir); flushErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to flush timing to artifacts: %v\n", flushErr)
	}
	if flushErr := r.Costs.Flush(r.Env.ArtifactsDir); flushErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to flush costs to artifacts: %v\n", flushErr)
	}
	runResult := r.writeRunResult(ExitSuccess, "")
	r.printRunSummary(-1)
	// Archive run to history
	if runID, archiveErr := state.ArchiveRun(r.Env.ArtifactsDir); archiveErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to archive run: %v\n", archiveErr)
	} else if !ux.QuietMode {
		fmt.Printf("  %sRun archived:%s %s\n", ux.Dim, ux.Reset, runID)
	}
	// Restore run-result.json after archive so it remains accessible in the current artifacts dir.
	// Reuse the cached result — no need to re-collect commits or write to audit dir again.
	if err := state.WriteRunResult(r.Env.ArtifactsDir, runResult); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to restore run-result.json: %v\n", err)
	}
	if pruneErr := state.PruneHistory(r.Env.ArtifactsDir, r.HistoryLimit); pruneErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to prune history: %v\n", pruneErr)
	}
	return nil
}

// prepareBackwardJump resets state for phases that will be re-executed after a backward jump.
// It clears loop counters for phases in [gotoIdx, currentIdx) and removes stale feedback.
// The jumping phase's own counter (at currentIdx) is NOT touched — the caller manages it.
// Must be called BEFORE SaveLoopCounts and WriteFeedback so the save includes resets
// and the new feedback isn't immediately cleared.
func (r *Runner) prepareBackwardJump(gotoIdx, currentIdx int, loopCounts map[string]int) error {
	for j := gotoIdx; j < currentIdx; j++ {
		name := r.Config.Phases[j].Name
		delete(loopCounts, name)
		delete(loopCounts, name+":exhaust")
		delete(r.skipped, name)
	}
	return state.ClearFeedback(r.Env.ArtifactsDir)
}

// handleLoopFailure processes a loop iteration failure (from phase failure or check failure).
// output is the content to write as feedback. Returns true if the main loop should continue.
func (r *Runner) handleLoopFailure(i int, phase config.Phase, loopCounts map[string]int, output string) (bool, error) {
	iteration := loopCounts[phase.Name] + 1
	loopCounts[phase.Name] = iteration

	if iteration >= phase.Loop.Max {
		// Loop exhausted
		if phase.Loop.OnExhaust != nil {
			exhaustKey := phase.Name + ":exhaust"
			exhaustCount := loopCounts[exhaustKey] + 1
			if exhaustCount > phase.Loop.OnExhaust.Max {
				if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
					fmt.Fprintf(os.Stderr, "warning: failed to save loop counts: %v\n", err)
				}
				if !ux.QuietMode {
					fmt.Printf("\n  Phase %q: loop exhausted after %d iterations, recovery exhausted after %d attempts. Manual intervention needed.\n",
						phase.Name, iteration, phase.Loop.OnExhaust.Max)
				}
				r.printRunSummary(i)
				return false, r.failWithCategory(state.StatusFailed, ExitPhaseFailure, state.FailCategoryLoopExhaustion,
					fmt.Sprintf("phase %q: loop exhausted (%d iterations) and recovery exhausted (%d attempts)", phase.Name, iteration, phase.Loop.OnExhaust.Max),
					fmt.Errorf("phase %q: loop exhausted (%d iterations) and recovery exhausted (%d attempts)", phase.Name, iteration, phase.Loop.OnExhaust.Max))
			}
			loopCounts[exhaustKey] = exhaustCount
			delete(loopCounts, phase.Name) // reset loop counter

			gotoIdx := r.Config.PhaseIndex(phase.Loop.OnExhaust.Goto)
			if gotoIdx < 0 {
				return false, r.failAndHint(state.StatusFailed, ExitConfigError,
					fmt.Errorf("phase %q: loop.on-exhaust.goto %q not found", phase.Name, phase.Loop.OnExhaust.Goto))
			}

			if err := r.prepareBackwardJump(gotoIdx, i, loopCounts); err != nil {
				return false, r.failAndHint(state.StatusFailed, ExitPhaseFailure, err)
			}
			if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
				return false, r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving loop counts: %w", err))
			}

			// Write convergence-failed feedback
			header := fmt.Sprintf("Convergence failed after %d iterations (min: %d, max: %d). Last iteration output follows:\n\n",
				iteration, phase.Loop.Min, phase.Loop.Max)
			if err := state.WriteFeedback(r.Env.ArtifactsDir, phase.Name, header+output); err != nil {
				return false, r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("writing feedback: %w", err))
			}

			ux.LoopExhausted(phase.Name, iteration)
			ux.LoopBack(phase.Name, phase.Loop.OnExhaust.Goto, exhaustCount, phase.Loop.OnExhaust.Max)

			r.State.SetPhase(gotoIdx)
			if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
				return false, r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving state after loop exhaustion: %w", err))
			}
			return true, nil
		}

		// No on-exhaust: hard fail
		if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save loop counts: %v\n", err)
		}
		if !ux.QuietMode {
			fmt.Printf("\n  Phase %q failed after %d iterations. Manual intervention needed.\n",
				phase.Name, iteration)
		}
		r.printRunSummary(i)
		return false, r.failWithCategory(state.StatusFailed, ExitPhaseFailure, state.FailCategoryLoopExhaustion,
			fmt.Sprintf("phase %q: failed after %d iterations", phase.Name, iteration),
			fmt.Errorf("phase %q: failed after %d iterations", phase.Name, iteration))
	}

	// Not exhausted — loop back
	gotoIdx := r.Config.PhaseIndex(phase.Loop.Goto)
	if gotoIdx < 0 {
		return false, r.failAndHint(state.StatusFailed, ExitConfigError,
			fmt.Errorf("phase %q: loop.goto %q not found", phase.Name, phase.Loop.Goto))
	}

	if err := r.prepareBackwardJump(gotoIdx, i, loopCounts); err != nil {
		return false, r.failAndHint(state.StatusFailed, ExitPhaseFailure, err)
	}
	if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
		return false, r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving loop counts: %w", err))
	}
	if err := state.WriteFeedback(r.Env.ArtifactsDir, phase.Name, output); err != nil {
		return false, r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("writing feedback: %w", err))
	}

	ux.LoopBack(phase.Name, phase.Loop.Goto, iteration, phase.Loop.Max)

	r.State.SetPhase(gotoIdx)
	if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
		return false, r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving state after loop-back: %w", err))
	}
	return true, nil
}

// waitForRateLimit blocks until the rate limit resets (+ 60s buffer), printing
// heartbeat messages every 60s. Returns nil on successful wait, or ctx.Err()
// if the context is cancelled during the wait. Timing for the phase is paused
// (AddEnd called here; the main loop's AddStartAt at re-entry handles resume).
func (r *Runner) waitForRateLimit(ctx context.Context, phaseName string, resetAt int64) error {
	resetTime := time.Unix(resetAt, 0)
	waitUntil := resetTime.Add(60 * time.Second)
	ux.RateLimitWait(resetTime)

	// Pause phase timing during the wait.
	// Do NOT call AddStartAt at the end — the main loop adds a fresh start
	// entry at line 344 when it re-enters on continue.
	r.Timing.AddEnd(phaseName)
	if flushErr := r.Timing.Flush(r.auditDir); flushErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to flush timing: %v\n", flushErr)
	}

	const heartbeatInterval = 60 * time.Second

	remaining := time.Until(waitUntil)
	for remaining > 0 {
		sleepDur := heartbeatInterval
		if remaining < sleepDur {
			sleepDur = remaining
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleepDur):
		}

		remaining -= sleepDur
		if remaining > 0 {
			ux.RateLimitHeartbeat(remaining)
		}
	}

	return nil
}

// runLoopCheck executes the loop.check command and returns the exit code and captured output.
func runLoopCheck(ctx context.Context, check string, phase config.Phase, env *dispatch.Environment) (int, string) {
	cmd := exec.CommandContext(ctx, "bash", "-c", check)
	cmd.Dir = dispatch.PhaseWorkDir(phase, env)
	cmd.Env = dispatch.BuildEnv(env)

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), buf.String()
		}
		return 1, buf.String()
	}
	return 0, buf.String()
}

// DryRunPrint prints the phase plan without executing.
func (r *Runner) DryRunPrint() {
	expandFn := func(s string) string {
		return dispatch.ExpandVars(s, r.Env.DryRunVars())
	}
	ux.FlowDiagram(r.Config, r.Env.CustomVars, expandFn)
}

// runParallel runs two phases concurrently.
func (r *Runner) runParallel(parentCtx context.Context, idx1, idx2, total int, loopCounts map[string]int) error {
	phase1 := r.Config.Phases[idx1]
	phase2 := r.Config.Phases[idx2]

	ux.PhaseHeader(idx1, total, phase1)
	ux.PhaseHeader(idx2, total, phase2)

	// Check run-level cost limit before starting parallel phases
	if r.Config.MaxCost > 0 && r.Costs.TotalCost() > r.Config.MaxCost {
		r.printRunSummary(idx1)
		return r.failWithCategory(state.StatusFailed, ExitCostLimit, state.FailCategoryCostOverrun,
			fmt.Sprintf("run cost $%.2f exceeded limit $%.2f", r.Costs.TotalCost(), r.Config.MaxCost),
			fmt.Errorf("run exceeded cost limit: $%.2f > $%.2f", r.Costs.TotalCost(), r.Config.MaxCost))
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	type phaseResult struct {
		idx       int
		result    *dispatch.Result
		err       error
		startTime time.Time
		endTime   time.Time
	}

	results := make(chan phaseResult, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		env1 := r.Env.Clone()
		env1.PhaseIndex = idx1
		phaseStart := time.Now()
		r.Timing.AddStartAt(phase1.Name, phaseStart)
		res, err := r.dispatchWithHooks(ctx, phase1, env1)
		phaseEnd := time.Now()
		results <- phaseResult{idx: idx1, result: res, err: err, startTime: phaseStart, endTime: phaseEnd}
	}()

	go func() {
		defer wg.Done()
		env2 := r.Env.Clone()
		env2.PhaseIndex = idx2
		phaseStart := time.Now()
		r.Timing.AddStartAt(phase2.Name, phaseStart)
		res, err := r.dispatchWithHooks(ctx, phase2, env2)
		phaseEnd := time.Now()
		results <- phaseResult{idx: idx2, result: res, err: err, startTime: phaseStart, endTime: phaseEnd}
	}()

	// Wait for both to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	var failedIdx int = -1
	var firstTimedOut bool
	// INVARIANT: attemptCount is mutated here in the sequential channel-drain
	// loop, NOT inside the dispatch goroutines above. This is critical for
	// thread safety — the map has no mutex. Moving r.attemptCount[pr.idx]++
	// into a goroutine would introduce a data race. The test
	// TestRun_ParallelAttemptCountInvariant pins this invariant.
	for pr := range results {
		phase := r.Config.Phases[pr.idx]
		// Write metadata before archiving
		writePhaseMetadata(r.Env.ArtifactsDir, pr.idx, buildPhaseMetadata(phase, pr.idx, pr.result, pr.startTime, pr.endTime))
		// Archive every parallel attempt to audit
		r.attemptCount[pr.idx]++
		archivePhaseFiles(r.Env.ArtifactsDir, r.auditDir, pr.idx, r.attemptCount[pr.idx], phase.Outputs)
		// Record cost data for agent phases (cost is incurred regardless of success/failure)
		if phase.Type == "agent" && pr.result != nil {
			r.Costs.Record(phase.Name, pr.idx, pr.result.CostUSD, pr.result.InputTokens, pr.result.OutputTokens, pr.result.CacheCreationInputTokens, pr.result.CacheReadInputTokens, pr.result.Turns)
			if pr.result.InputTokens == 0 && pr.result.OutputTokens == 0 {
				fmt.Fprintf(os.Stderr, "  note: no token counts in stream output for phase %q (token tracking is best-effort)\n", phase.Name)
			}
		}
		if pr.err != nil || (pr.result != nil && pr.result.ExitCode != 0) {
			cancel() // cancel the other goroutine
			r.Timing.AddEndAt(phase.Name, pr.endTime)
			errMsg := fmt.Sprintf("%s exited with non-zero status", phase.Type)
			if pr.result != nil && pr.result.TimedOut {
				errMsg = fmt.Sprintf("timed out after %dm — consider increasing 'timeout' in config (current: %d)", phase.Timeout, phase.Timeout)
			} else if pr.err != nil {
				errMsg = pr.err.Error()
			}
			appendPhaseLog(r.Env.ArtifactsDir, pr.idx, fmt.Sprintf("\n[orc] phase %q failed: %s\n", phase.Name, errMsg))
			ux.PhaseFail(pr.idx, phase.Name, errMsg)
			if phase.Type == "agent" && pr.result != nil && pr.result.SessionID != "" {
				fmt.Fprintf(os.Stderr, "warning: session ID from parallel phase %q not persisted — resume is not supported for parallel agents\n", phase.Name)
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("phase %q failed: %s", phase.Name, errMsg)
				failedIdx = pr.idx
				firstTimedOut = pr.result != nil && pr.result.TimedOut
			}
		} else {
			r.Timing.AddEndAt(phase.Name, pr.endTime)
			if phase.Type == "agent" && pr.result != nil && pr.result.SessionID != "" {
				fmt.Fprintf(os.Stderr, "warning: session ID from parallel phase %q not persisted — resume is not supported for parallel agents\n", phase.Name)
			}
			ux.PhaseComplete(pr.idx, phase.Name, pr.endTime.Sub(pr.startTime))
		}
	}
	if saveErr := state.SaveAttemptCounts(r.auditDir, r.attemptCount); saveErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save attempt counts: %v\n", saveErr)
	}

	if firstErr != nil {
		if parentCtx.Err() != nil {
			r.printRunSummary(failedIdx)
			return r.failWithCategory(state.StatusInterrupted, ExitInterrupted, state.FailCategoryInterrupted, parentCtx.Err().Error(), parentCtx.Err())
		}
		r.printRunSummary(failedIdx)
		failedPhase := r.Config.Phases[failedIdx]
		category := state.FailCategoryScriptFailure
		if failedPhase.Type == "agent" {
			category = state.FailCategoryAgentError
		} else if failedPhase.Type == "gate" {
			category = state.FailCategoryGateRejection
		}
		exitCode := ExitPhaseFailure
		if firstTimedOut {
			exitCode = ExitTimeout
			category = state.FailCategoryTimeout
		}
		return r.failWithCategory(state.StatusFailed, exitCode, category, firstErr.Error(), firstErr)
	}

	// Flush costs and check cost limits after parallel phases complete
	if err := r.Costs.Flush(r.auditDir); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to flush costs: %v\n", err)
	}
	for _, pi := range []struct {
		idx   int
		phase config.Phase
	}{{idx1, phase1}, {idx2, phase2}} {
		if pi.phase.MaxCost > 0 && pi.phase.Type == "agent" {
			phaseCost := r.Costs.PhaseCost(pi.phase.Name)
			if phaseCost > pi.phase.MaxCost {
				r.printRunSummary(pi.idx)
				return r.failWithCategory(state.StatusFailed, ExitCostLimit, state.FailCategoryCostOverrun,
					fmt.Sprintf("phase %q cost $%.2f exceeded limit $%.2f", pi.phase.Name, phaseCost, pi.phase.MaxCost),
					fmt.Errorf("phase %q exceeded cost limit: $%.2f > $%.2f", pi.phase.Name, phaseCost, pi.phase.MaxCost))
			}
		}
	}
	if r.Config.MaxCost > 0 && r.Costs.TotalCost() > r.Config.MaxCost {
		r.printRunSummary(idx1)
		return r.failWithCategory(state.StatusFailed, ExitCostLimit, state.FailCategoryCostOverrun,
			fmt.Sprintf("run cost $%.2f exceeded limit $%.2f", r.Costs.TotalCost(), r.Config.MaxCost),
			fmt.Errorf("run exceeded cost limit: $%.2f > $%.2f", r.Costs.TotalCost(), r.Config.MaxCost))
	}

	// Check declared outputs for both phases
	for _, pi := range []struct {
		idx   int
		phase config.Phase
	}{{idx1, phase1}, {idx2, phase2}} {
		if len(pi.phase.Outputs) > 0 {
			missing := state.CheckOutputs(r.Env.ArtifactsDir, pi.phase.Outputs)
			if len(missing) > 0 {
				errMsg := fmt.Sprintf("missing outputs: %v", missing)
				ux.PhaseFail(pi.idx, pi.phase.Name, errMsg)
				return r.failWithCategory(state.StatusFailed, ExitPhaseFailure, state.FailCategoryOutputMissing, errMsg,
					fmt.Errorf("phase %q: %s", pi.phase.Name, errMsg))
			}
		}
	}

	// Mark intermediate phases (between the two parallel partners) as skipped.
	// These phases are never dispatched — jumping past them without marking
	// them would cause buildPhaseResults to report them as "completed".
	lo, hi := idx1, idx2
	if lo > hi {
		lo, hi = hi, lo
	}
	for mid := lo + 1; mid < hi; mid++ {
		r.skipped[r.Config.Phases[mid].Name] = true
	}

	// Advance past both phases — set to the one after the later index
	if idx2 > idx1 {
		r.State.SetPhase(idx2 + 1)
	} else {
		r.State.SetPhase(idx1 + 1)
	}
	if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
		return fmt.Errorf("saving state after parallel advance: %w", err)
	}
	if err := r.Timing.Flush(r.auditDir); err != nil {
		return fmt.Errorf("flushing timing after parallel: %w", err)
	}
	if err := r.Costs.Flush(r.auditDir); err != nil {
		return fmt.Errorf("flushing costs after parallel: %w", err)
	}
	if r.StepMode {
		promptFn := r.StepPromptFn
		if promptFn == nil {
			promptFn = ux.StepPrompt
		}
		for {
			action := promptFn(r.Env.ArtifactsDir, idx1, phase1.Name+" + "+phase2.Name)
			switch action.Type {
			case "abort":
				r.printRunSummary(-1)
				return r.failWithCategory(state.StatusInterrupted, ExitInterrupted, state.FailCategoryInterrupted,
					"aborted by user in step-through mode",
					fmt.Errorf("aborted by user in step-through mode"))
			case "rewind":
				idx, err := config.ResolvePhaseRef(action.Target, r.Config.Phases)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  invalid rewind target: %v\n", err)
					continue
				}
				if idx >= r.State.GetPhaseIndex() {
					fmt.Fprintf(os.Stderr, "  rewind target %q is not before the current phase — rewind only jumps backward\n", action.Target)
					continue
				}
				if err := r.prepareBackwardJump(idx, r.State.GetPhaseIndex(), loopCounts); err != nil {
					return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("preparing rewind to phase %d: %w", idx, err))
				}
				if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
					return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving loop counts after rewind: %w", err))
				}
				r.State.SetPhase(idx)
				if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
					return r.failAndHint(state.StatusFailed, ExitPhaseFailure, fmt.Errorf("saving state after rewind: %w", err))
				}
				return errStepRewind
			case "continue":
				// fall through
			}
			break
		}
	}
	return nil
}

// archivePhaseFiles copies the current log, prompt, and stream log files to the
// audit directory. Called after every dispatch so audit has a complete record.
// iteration is 1-indexed (1 = first dispatch).
func archivePhaseFiles(artifactsDir, auditDir string, phaseIdx, iteration int, outputs []string) {
	copyFile(state.LogPath(artifactsDir, phaseIdx), state.AuditLogPath(auditDir, phaseIdx, iteration))
	copyFile(state.PromptPath(artifactsDir, phaseIdx), state.AuditPromptPath(auditDir, phaseIdx, iteration))
	copyFile(state.StreamLogPath(artifactsDir, phaseIdx), state.AuditStreamLogPath(auditDir, phaseIdx, iteration))
	copyFile(state.MetaPath(artifactsDir, phaseIdx), state.AuditMetaPath(auditDir, phaseIdx, iteration))
	for _, o := range outputs {
		copyFile(filepath.Join(artifactsDir, o), state.AuditOutputPath(auditDir, phaseIdx, iteration, o))
	}
}

// archiveAndClearFeedback copies all feedback files to the audit directory, then
// removes them so downstream phases don't see stale loop feedback.
// Reads the directory once to avoid redundant syscalls.
// Errors are silently ignored — archiving should not break the run.
func archiveAndClearFeedback(artifactsDir, auditDir string, phaseIdx, iteration int) {
	feedbackDir := filepath.Join(artifactsDir, "feedback")
	entries, err := os.ReadDir(feedbackDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		fromPhase := strings.TrimSuffix(strings.TrimPrefix(name, "from-"), ".md")
		src := filepath.Join(feedbackDir, name)
		dst := state.AuditFeedbackPath(auditDir, phaseIdx, iteration, fromPhase)
		copyFile(src, dst)
		os.Remove(src)
	}
}

// copyFile copies src to dst, creating parent directories as needed.
// Errors are silently ignored — archiving should not break the run.
func copyFile(src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return
	}
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer out.Close()
	io.Copy(out, in)
}

// evalCondition runs a shell command and returns true if it exits 0.
func evalCondition(ctx context.Context, phase config.Phase, env *dispatch.Environment) bool {
	cmd := exec.CommandContext(ctx, "bash", "-c", phase.Condition)
	cmd.Dir = dispatch.PhaseWorkDir(phase, env)
	cmd.Env = dispatch.BuildEnv(env)
	return cmd.Run() == nil
}

func (r *Runner) dispatchWithHooks(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
	return dispatch.DispatchWithHooks(ctx, phase, env, r.Dispatcher.Dispatch)
}

// dispatchWorkflow is a DispatchFunc that runs a child workflow inline.
func (r *Runner) dispatchWorkflow(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
	return r.runSubWorkflow(ctx, phase.WorkflowRef)
}

// dispatchBranch is a DispatchFunc that runs a check script, matches the output
// to a branch key, and runs the corresponding child workflow.
func (r *Runner) dispatchBranch(ctx context.Context, phase config.Phase, env *dispatch.Environment) (*dispatch.Result, error) {
	stdout, exitCode, err := evalCheckOutput(ctx, phase.Check, phase, env)
	if err != nil {
		return nil, fmt.Errorf("branch %q: check command error: %w", phase.Name, err)
	}
	if exitCode != 0 {
		return &dispatch.Result{ExitCode: exitCode, Output: stdout}, nil
	}

	key := strings.TrimSpace(stdout)
	workflow, ok := phase.Branches[key]
	if !ok {
		if phase.Default != "" {
			workflow = phase.Default
		} else {
			return &dispatch.Result{
				ExitCode: 1,
				Output:   fmt.Sprintf("check returned %q — no matching branch and no default", key),
			}, nil
		}
	}
	ux.BranchSelected(phase.Name, key, workflow)
	return r.runSubWorkflow(ctx, workflow)
}

// runSubWorkflow loads and runs a child workflow inline, merging costs back into the parent.
func (r *Runner) runSubWorkflow(ctx context.Context, workflowName string) (*dispatch.Result, error) {
	childCfg, err := config.LoadWorkflow(r.Env.ProjectRoot, workflowName)
	if err != nil {
		return nil, fmt.Errorf("loading workflow %q: %w", workflowName, err)
	}

	childArtifacts := state.ArtifactsDirForWorkflow(r.Env.ProjectRoot, workflowName, r.Env.Ticket)
	childState, err := state.Load(childArtifacts)
	if err != nil {
		return nil, fmt.Errorf("loading state for workflow %q: %w", workflowName, err)
	}
	childState.SetTicket(r.Env.Ticket)
	childState.SetWorkflow(workflowName)

	childEnv := r.Env.Clone()
	childEnv.ArtifactsDir = childArtifacts
	childEnv.Workflow = workflowName
	childEnv.PhaseCount = len(childCfg.Phases)
	childEnv.ResumeSessionID = ""
	// Merge parent custom vars with child config vars (child vars win on conflict).
	if len(childCfg.Vars) > 0 {
		builtins := childEnv.Vars()
		childEnv.CustomVars = dispatch.ExpandConfigVars(childCfg.Vars, builtins)
	}

	ux.SubWorkflowStart(workflowName)

	child := &Runner{
		Config:       childCfg,
		State:        childState,
		Env:          childEnv,
		Dispatcher:   r.Dispatcher,
		StepMode:     r.StepMode,
		HistoryLimit: childCfg.HistoryLimit,
	}

	childErr := child.Run(ctx)

	// Merge child costs into parent.
	if r.Costs != nil && child.Costs != nil {
		r.Costs.Merge(child.Costs, workflowName)
	}

	ux.SubWorkflowEnd(workflowName)

	// Synthesize a Result for the parent's phase handling.
	result := &dispatch.Result{ExitCode: 0}
	if child.Costs != nil {
		result.CostUSD = child.Costs.TotalCost()
	}
	if childErr != nil {
		result.ExitCode = 1
		result.Output = childErr.Error()
	}
	return result, nil
}

// evalCheckOutput runs a shell command and returns its stdout, exit code, and any exec error.
// Modeled on runLoopCheck but separates stdout from stderr.
func evalCheckOutput(ctx context.Context, check string, phase config.Phase, env *dispatch.Environment) (string, int, error) {
	cmd := exec.CommandContext(ctx, "bash", "-c", check)
	cmd.Dir = dispatch.PhaseWorkDir(phase, env)
	cmd.Env = dispatch.BuildEnv(env)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return stdout.String(), exitErr.ExitCode(), nil
		}
		return "", 1, err
	}
	return stdout.String(), 0, nil
}
