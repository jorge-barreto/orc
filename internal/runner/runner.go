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

// Runner drives the workflow state machine.
type Runner struct {
	Config        *config.Config
	State         *state.State
	Env           *dispatch.Environment
	Dispatcher    dispatch.Dispatcher
	Timing        *state.Timing
	Costs         *state.CostData
	skipped       map[string]bool
	auditDir      string
	dispatchCount map[int]int // tracks how many times each phase has been dispatched
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

// failAndHint sets the failure status, saves state (warning on error),
// flushes timing, prints a resume hint, and returns the given error.
func (r *Runner) failAndHint(status string, exitCode int, err error) error {
	r.State.Status = status
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
	}
	if r.Costs != nil {
		if flushErr := r.Costs.Flush(r.auditDir); flushErr != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to flush costs: %v\n", flushErr)
		}
	}
	ux.ResumeHint(r.State.Ticket)
	return &ExitError{Code: exitCode, Err: err}
}

// printRunSummary prints the run summary table if timing data is available.
func (r *Runner) printRunSummary(failedPhase int) {
	if r.Timing == nil {
		return
	}
	ux.RunSummary(r.Config.Phases, r.Timing, failedPhase, r.skipped)
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
	r.auditDir = state.AuditDir(r.Env.ProjectRoot, r.Env.Ticket)
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
	r.dispatchCount = make(map[int]int)

	total := len(r.Config.Phases)

	for r.State.PhaseIndex < total {
		i := r.State.PhaseIndex
		phase := r.Config.Phases[i]

		// Check for context cancellation
		if ctx.Err() != nil {
			if i > 0 {
				r.printRunSummary(-1)
			}
			return r.failAndHint(state.StatusInterrupted, ExitSignal, ctx.Err())
		}

		// Check run-level cost limit before starting next phase
		if r.Config.MaxCost > 0 && r.Costs.TotalCost() > r.Config.MaxCost {
			r.printRunSummary(i)
			return r.failAndHint(state.StatusFailed, ExitHumanNeeded,
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
				if err != nil {
					return err
				}
				continue
			}
		}

		// Normal dispatch
		ux.PhaseHeader(i, total, phase)
		r.Timing.AddStart(phase.Name)

		// Archive logs/prompts before re-dispatch (iteration > 0)
		if r.dispatchCount[i] > 0 {
			archivePhaseFiles(r.Env.ArtifactsDir, r.auditDir, i, r.dispatchCount[i], phase.Outputs)
		}
		r.dispatchCount[i]++

		r.Env.PhaseIndex = i
		start := time.Now()
		result, err := r.Dispatcher.Dispatch(ctx, phase, r.Env)

		if ctx.Err() != nil {
			r.Timing.AddEnd(phase.Name)
			appendPhaseLog(r.Env.ArtifactsDir, i, fmt.Sprintf("\n[orc] phase interrupted: %v\n", ctx.Err()))
			r.printRunSummary(i)
			return r.failAndHint(state.StatusInterrupted, ExitSignal, ctx.Err())
		}

		// Record cost data for agent phases (cost is incurred regardless of success/failure)
		if phase.Type == "agent" && result != nil {
			r.Costs.Record(phase.Name, i, result.CostUSD, result.InputTokens, result.OutputTokens, result.CacheCreationInputTokens, result.CacheReadInputTokens, result.Turns)
			// Warn if agent completed but reported no token counts (best-effort tracking)
			if result.InputTokens == 0 && result.OutputTokens == 0 {
				fmt.Fprintf(os.Stderr, "  note: no token counts in stream output for phase %q (token tracking is best-effort)\n", phase.Name)
			}
			if flushErr := r.Costs.Flush(r.auditDir); flushErr != nil {
				fmt.Fprintf(os.Stderr, "warning: failed to flush costs: %v\n", flushErr)
			}
			// Check phase-level cost limit
			if phase.MaxCost > 0 {
				phaseCost := r.Costs.PhaseCost(phase.Name)
				if phaseCost > phase.MaxCost {
					r.Timing.AddEnd(phase.Name)
					r.printRunSummary(i)
					return r.failAndHint(state.StatusFailed, ExitHumanNeeded,
						fmt.Errorf("phase %q exceeded cost limit: $%.2f > $%.2f", phase.Name, phaseCost, phase.MaxCost))
				}
			}
		}

		if err != nil || (result != nil && result.ExitCode != 0) {
			r.Timing.AddEnd(phase.Name)
			errMsg := "non-zero exit"
			if result != nil && result.TimedOut {
				errMsg = fmt.Sprintf("timed out after %dm", phase.Timeout)
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
				output := ""
				if result != nil {
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
			exitCode := ExitRetryable
			if phase.Type == "gate" {
				exitCode = ExitHumanNeeded
			}
			r.printRunSummary(i)
			return r.failAndHint(state.StatusFailed, exitCode, fmt.Errorf("phase %q failed", phase.Name))
		}

		// Check declared outputs
		if len(phase.Outputs) > 0 {
			missing := state.CheckOutputs(r.Env.ArtifactsDir, phase.Outputs)
			if len(missing) > 0 && phase.Type == "agent" {
				// Re-invoke agent once for missing outputs
				for _, m := range missing {
					prompt := fmt.Sprintf(
						"You did not produce the expected artifact at %q. Please produce it now.",
						filepath.Join(r.Env.ArtifactsDir, m))
					if _, err := dispatch.RunAgentWithPrompt(ctx, phase, r.Env, prompt); err != nil {
						fmt.Fprintf(os.Stderr, "warning: re-prompt for missing output failed: %v\n", err)
					}
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
				return r.failAndHint(state.StatusFailed, ExitRetryable, fmt.Errorf("phase %q: %s", phase.Name, errMsg))
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
					return r.failAndHint(state.StatusFailed, ExitRetryable, err)
				}
				if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
					return r.failAndHint(state.StatusFailed, ExitRetryable, fmt.Errorf("saving loop counts: %w", err))
				}

				output := ""
				if result != nil {
					output = result.Output
				}
				if err := state.WriteFeedback(r.Env.ArtifactsDir, phase.Name, output); err != nil {
					return err
				}

				r.Timing.AddEnd(phase.Name)
				ux.LoopBack(phase.Name, phase.Loop.Goto, iteration, phase.Loop.Max)

				r.State.SetPhase(gotoIdx)
				if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
					return fmt.Errorf("saving state after loop iteration: %w", err)
				}
				continue
			}

			// iteration >= min AND pass: break out of loop, advance normally
			if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
				return r.failAndHint(state.StatusFailed, ExitRetryable, fmt.Errorf("saving loop counts: %w", err))
			}
			// Archive and clear feedback so downstream phases don't see stale loop feedback
			archiveAndClearFeedback(r.Env.ArtifactsDir, r.auditDir, i, r.dispatchCount[i])
		}

		duration := time.Since(start)
		r.Timing.AddEnd(phase.Name)
		if err := r.Timing.Flush(r.auditDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to flush timing: %v\n", err)
		}
		r.State.Advance()
		r.State.Status = state.StatusRunning
		if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
			return fmt.Errorf("saving state after phase advance: %w", err)
		}
		ux.PhaseComplete(i, duration)
	}

	r.State.Status = state.StatusCompleted
	if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
		return fmt.Errorf("saving final state: %w", err)
	}
	if saveErr := r.State.Save(r.auditDir); saveErr != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save audit state: %v\n", saveErr)
	}
	if err := r.Timing.Flush(r.auditDir); err != nil {
		return fmt.Errorf("flushing timing: %w", err)
	}
	if err := r.Costs.Flush(r.auditDir); err != nil {
		return fmt.Errorf("flushing costs: %w", err)
	}
	r.printRunSummary(-1)
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
				fmt.Printf("\n  Phase %q: loop exhausted after %d iterations, recovery exhausted after %d attempts. Manual intervention needed.\n",
					phase.Name, iteration, phase.Loop.OnExhaust.Max)
				r.printRunSummary(i)
				return false, r.failAndHint(state.StatusFailed, ExitRetryable,
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
				return false, r.failAndHint(state.StatusFailed, ExitRetryable, err)
			}
			if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
				return false, r.failAndHint(state.StatusFailed, ExitRetryable, fmt.Errorf("saving loop counts: %w", err))
			}

			// Write convergence-failed feedback
			header := fmt.Sprintf("Convergence failed after %d iterations (min: %d, max: %d). Last iteration output follows:\n\n",
				iteration, phase.Loop.Min, phase.Loop.Max)
			if err := state.WriteFeedback(r.Env.ArtifactsDir, phase.Name, header+output); err != nil {
				return false, err
			}

			ux.LoopExhausted(phase.Name, iteration)
			ux.LoopBack(phase.Name, phase.Loop.OnExhaust.Goto, exhaustCount, phase.Loop.OnExhaust.Max)

			r.State.SetPhase(gotoIdx)
			if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
				return false, fmt.Errorf("saving state after loop exhaustion: %w", err)
			}
			return true, nil
		}

		// No on-exhaust: hard fail
		if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to save loop counts: %v\n", err)
		}
		fmt.Printf("\n  Phase %q failed after %d iterations. Manual intervention needed.\n",
			phase.Name, iteration)
		r.printRunSummary(i)
		return false, r.failAndHint(state.StatusFailed, ExitRetryable,
			fmt.Errorf("phase %q: failed after %d iterations", phase.Name, iteration))
	}

	// Not exhausted — loop back
	gotoIdx := r.Config.PhaseIndex(phase.Loop.Goto)
	if gotoIdx < 0 {
		return false, r.failAndHint(state.StatusFailed, ExitConfigError,
			fmt.Errorf("phase %q: loop.goto %q not found", phase.Name, phase.Loop.Goto))
	}

	if err := r.prepareBackwardJump(gotoIdx, i, loopCounts); err != nil {
		return false, r.failAndHint(state.StatusFailed, ExitRetryable, err)
	}
	if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
		return false, r.failAndHint(state.StatusFailed, ExitRetryable, fmt.Errorf("saving loop counts: %w", err))
	}
	if err := state.WriteFeedback(r.Env.ArtifactsDir, phase.Name, output); err != nil {
		return false, err
	}

	ux.LoopBack(phase.Name, phase.Loop.Goto, iteration, phase.Loop.Max)

	r.State.SetPhase(gotoIdx)
	if err := r.State.Save(r.Env.ArtifactsDir); err != nil {
		return false, fmt.Errorf("saving state after loop-back: %w", err)
	}
	return true, nil
}

// runLoopCheck executes the loop.check command and returns the exit code and captured output.
func runLoopCheck(ctx context.Context, check string, phase config.Phase, env *dispatch.Environment) (int, string) {
	expanded := dispatch.ExpandVars(check, env.Vars())
	cmd := exec.CommandContext(ctx, "bash", "-c", expanded)
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
		return dispatch.ExpandVars(s, r.Env.Vars())
	}
	ux.FlowDiagram(r.Config, r.Env.CustomVars, expandFn)
}

// runParallel runs two phases concurrently.
func (r *Runner) runParallel(parentCtx context.Context, idx1, idx2, total int, loopCounts map[string]int) error {
	phase1 := r.Config.Phases[idx1]
	phase2 := r.Config.Phases[idx2]

	ux.PhaseHeader(idx1, total, phase1)
	ux.PhaseHeader(idx2, total, phase2)

	r.Timing.AddStart(phase1.Name)
	r.Timing.AddStart(phase2.Name)

	// Check run-level cost limit before starting parallel phases
	if r.Config.MaxCost > 0 && r.Costs.TotalCost() > r.Config.MaxCost {
		r.printRunSummary(idx1)
		return r.failAndHint(state.StatusFailed, ExitHumanNeeded,
			fmt.Errorf("run exceeded cost limit: $%.2f > $%.2f", r.Costs.TotalCost(), r.Config.MaxCost))
	}

	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()

	type phaseResult struct {
		idx    int
		result *dispatch.Result
		err    error
	}

	results := make(chan phaseResult, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	start := time.Now()

	go func() {
		defer wg.Done()
		env1 := r.Env.Clone()
		env1.PhaseIndex = idx1
		res, err := r.Dispatcher.Dispatch(ctx, phase1, env1)
		results <- phaseResult{idx: idx1, result: res, err: err}
	}()

	go func() {
		defer wg.Done()
		env2 := r.Env.Clone()
		env2.PhaseIndex = idx2
		res, err := r.Dispatcher.Dispatch(ctx, phase2, env2)
		results <- phaseResult{idx: idx2, result: res, err: err}
	}()

	// Wait for both to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	var failedIdx int = -1
	for pr := range results {
		phase := r.Config.Phases[pr.idx]
		// Record cost data for agent phases (cost is incurred regardless of success/failure)
		if phase.Type == "agent" && pr.result != nil {
			r.Costs.Record(phase.Name, pr.idx, pr.result.CostUSD, pr.result.InputTokens, pr.result.OutputTokens, pr.result.CacheCreationInputTokens, pr.result.CacheReadInputTokens, pr.result.Turns)
			if pr.result.InputTokens == 0 && pr.result.OutputTokens == 0 {
				fmt.Fprintf(os.Stderr, "  note: no token counts in stream output for phase %q (token tracking is best-effort)\n", phase.Name)
			}
		}
		if pr.err != nil || (pr.result != nil && pr.result.ExitCode != 0) {
			cancel() // cancel the other goroutine
			r.Timing.AddEnd(phase.Name)
			errMsg := "non-zero exit"
			if pr.result != nil && pr.result.TimedOut {
				errMsg = fmt.Sprintf("timed out after %dm", phase.Timeout)
			} else if pr.err != nil {
				errMsg = pr.err.Error()
			}
			appendPhaseLog(r.Env.ArtifactsDir, pr.idx, fmt.Sprintf("\n[orc] phase %q failed: %s\n", phase.Name, errMsg))
			ux.PhaseFail(pr.idx, phase.Name, errMsg)
			if firstErr == nil {
				firstErr = fmt.Errorf("phase %q failed: %s", phase.Name, errMsg)
				failedIdx = pr.idx
			}
		} else {
			r.Timing.AddEnd(phase.Name)
			ux.PhaseComplete(pr.idx, time.Since(start))
		}
	}

	if firstErr != nil {
		if parentCtx.Err() != nil {
			r.printRunSummary(failedIdx)
			return r.failAndHint(state.StatusInterrupted, ExitSignal, parentCtx.Err())
		}
		r.printRunSummary(failedIdx)
		return r.failAndHint(state.StatusFailed, ExitRetryable, firstErr)
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
				return r.failAndHint(state.StatusFailed, ExitHumanNeeded,
					fmt.Errorf("phase %q exceeded cost limit: $%.2f > $%.2f", pi.phase.Name, phaseCost, pi.phase.MaxCost))
			}
		}
	}
	if r.Config.MaxCost > 0 && r.Costs.TotalCost() > r.Config.MaxCost {
		r.printRunSummary(idx1)
		return r.failAndHint(state.StatusFailed, ExitHumanNeeded,
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
				return r.failAndHint(state.StatusFailed, ExitRetryable, fmt.Errorf("phase %q: %s", pi.phase.Name, errMsg))
			}
		}
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
	return nil
}

// archivePhaseFiles copies the current log and prompt files to the audit directory
// before they get overwritten by the next dispatch. iteration is the 1-indexed
// count of prior dispatches (e.g., 1 means this is the first archive).
func archivePhaseFiles(artifactsDir, auditDir string, phaseIdx, iteration int, outputs []string) {
	copyFile(state.LogPath(artifactsDir, phaseIdx), state.AuditLogPath(auditDir, phaseIdx, iteration))
	copyFile(state.PromptPath(artifactsDir, phaseIdx), state.AuditPromptPath(auditDir, phaseIdx, iteration))
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
	expanded := dispatch.ExpandVars(phase.Condition, env.Vars())
	cmd := exec.CommandContext(ctx, "bash", "-c", expanded)
	cmd.Dir = dispatch.PhaseWorkDir(phase, env)
	cmd.Env = dispatch.BuildEnv(env)
	return cmd.Run() == nil
}
