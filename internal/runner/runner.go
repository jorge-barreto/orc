package runner

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
)

// Runner drives the workflow state machine.
type Runner struct {
	Config     *config.Config
	State      *state.State
	Env        *dispatch.Environment
	Dispatcher dispatch.Dispatcher
	DryRun     bool
}

// Run executes the workflow from the current state.
func (r *Runner) Run(ctx context.Context) error {
	if err := state.EnsureDir(r.Env.ArtifactsDir); err != nil {
		return err
	}

	loopCounts, err := state.LoadLoopCounts(r.Env.ArtifactsDir)
	if err != nil {
		return fmt.Errorf("loading loop counts: %w", err)
	}

	total := len(r.Config.Phases)

	for r.State.PhaseIndex < total {
		i := r.State.PhaseIndex
		phase := r.Config.Phases[i]

		// Check for context cancellation
		if ctx.Err() != nil {
			r.State.Status = "interrupted"
			r.State.Save(r.Env.ArtifactsDir)
			ux.ResumeHint(r.State.Ticket)
			return ctx.Err()
		}

		// Evaluate condition
		if phase.Condition != "" {
			if !evalCondition(ctx, phase.Condition, r.Env) {
				ux.PhaseSkip(i, phase.Name)
				r.State.Advance()
				r.State.Save(r.Env.ArtifactsDir)
				continue
			}
		}

		// Handle parallel-with
		if phase.ParallelWith != "" {
			partnerIdx := r.Config.PhaseIndex(phase.ParallelWith)
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
		if err := state.RecordStart(r.Env.ArtifactsDir, phase.Name); err != nil {
			return err
		}

		r.Env.PhaseIndex = i
		start := time.Now()
		result, err := r.Dispatcher.Dispatch(ctx, phase, r.Env)

		if err != nil && ctx.Err() != nil {
			// Context cancelled (Ctrl+C)
			r.State.Status = "interrupted"
			r.State.Save(r.Env.ArtifactsDir)
			ux.ResumeHint(r.State.Ticket)
			return ctx.Err()
		}

		if err != nil || (result != nil && result.ExitCode != 0) {
			errMsg := "non-zero exit"
			if err != nil {
				errMsg = err.Error()
			}
			ux.PhaseFail(i, phase.Name, errMsg)

			// Handle on-fail
			if phase.OnFail != nil {
				count := loopCounts[phase.Name] + 1
				if count > phase.OnFail.Max {
					fmt.Printf("\n  Phase %q failed after %d retry loops. Manual intervention needed.\n",
						phase.Name, phase.OnFail.Max)
					r.State.Status = "failed"
					r.State.Save(r.Env.ArtifactsDir)
					ux.ResumeHint(r.State.Ticket)
					return fmt.Errorf("phase %q exceeded max retries (%d)", phase.Name, phase.OnFail.Max)
				}

				loopCounts[phase.Name] = count
				if err := state.SaveLoopCounts(r.Env.ArtifactsDir, loopCounts); err != nil {
					return err
				}

				// Write feedback from failed phase
				output := ""
				if result != nil {
					output = result.Output
				}
				if output == "" {
					output = errMsg
				}
				if err := state.WriteFeedback(r.Env.ArtifactsDir, phase.Name, output); err != nil {
					return err
				}

				gotoIdx := r.Config.PhaseIndex(phase.OnFail.Goto)
				ux.LoopBack(phase.Name, phase.OnFail.Goto, count, phase.OnFail.Max)

				r.State.SetPhase(gotoIdx)
				r.State.Save(r.Env.ArtifactsDir)
				continue
			}

			// No on-fail: stop
			r.State.Status = "failed"
			r.State.Save(r.Env.ArtifactsDir)
			ux.ResumeHint(r.State.Ticket)
			return fmt.Errorf("phase %q failed", phase.Name)
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
					dispatch.RunAgentWithPrompt(ctx, phase, r.Env, prompt)
				}
				missing = state.CheckOutputs(r.Env.ArtifactsDir, phase.Outputs)
			}
			if len(missing) > 0 {
				errMsg := fmt.Sprintf("missing outputs: %v", missing)
				ux.PhaseFail(i, phase.Name, errMsg)
				r.State.Status = "failed"
				r.State.Save(r.Env.ArtifactsDir)
				ux.ResumeHint(r.State.Ticket)
				return fmt.Errorf("phase %q: %s", phase.Name, errMsg)
			}
		}

		duration := time.Since(start)
		state.RecordEnd(r.Env.ArtifactsDir, phase.Name)
		r.State.Advance()
		r.State.Status = "running"
		r.State.Save(r.Env.ArtifactsDir)
		ux.PhaseComplete(i, duration)
	}

	r.State.Status = "completed"
	r.State.Save(r.Env.ArtifactsDir)
	ux.Success(len(r.Config.Phases))
	return nil
}

// DryRunPrint prints the phase plan without executing.
func (r *Runner) DryRunPrint() {
	total := len(r.Config.Phases)
	fmt.Printf("\n%sDry run — %d phases:%s\n\n", ux.Bold, total, ux.Reset)
	for i, p := range r.Config.Phases {
		fmt.Printf("  %s%d.%s %s%s%s (%s)", ux.Cyan, i+1, ux.Reset, ux.Bold, p.Name, ux.Reset, p.Type)
		if p.Description != "" {
			fmt.Printf(" — %s", p.Description)
		}
		fmt.Println()

		switch p.Type {
		case "script":
			expanded := dispatch.ExpandVars(p.Run, r.Env.Vars())
			fmt.Printf("     run: %s\n", expanded)
		case "agent":
			fmt.Printf("     prompt: %s\n", p.Prompt)
			fmt.Printf("     model: %s, timeout: %dm\n", p.Model, p.Timeout)
		case "gate":
			if p.SkipWith != "" {
				fmt.Printf("     skip-with: %s\n", p.SkipWith)
			}
		}

		if len(p.Outputs) > 0 {
			fmt.Printf("     outputs: %v\n", p.Outputs)
		}
		if p.OnFail != nil {
			fmt.Printf("     on-fail: goto %s (max %d)\n", p.OnFail.Goto, p.OnFail.Max)
		}
		if p.Condition != "" {
			fmt.Printf("     condition: %s\n", p.Condition)
		}
		if p.ParallelWith != "" {
			fmt.Printf("     parallel-with: %s\n", p.ParallelWith)
		}
	}
	fmt.Println()
}

// runParallel runs two phases concurrently.
func (r *Runner) runParallel(ctx context.Context, idx1, idx2, total int, loopCounts map[string]int) error {
	phase1 := r.Config.Phases[idx1]
	phase2 := r.Config.Phases[idx2]

	ux.PhaseHeader(idx1, total, phase1)
	ux.PhaseHeader(idx2, total, phase2)

	state.RecordStart(r.Env.ArtifactsDir, phase1.Name)
	state.RecordStart(r.Env.ArtifactsDir, phase2.Name)

	ctx, cancel := context.WithCancel(ctx)
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
		env1 := *r.Env
		env1.PhaseIndex = idx1
		res, err := r.Dispatcher.Dispatch(ctx, phase1, &env1)
		results <- phaseResult{idx: idx1, result: res, err: err}
	}()

	go func() {
		defer wg.Done()
		env2 := *r.Env
		env2.PhaseIndex = idx2
		res, err := r.Dispatcher.Dispatch(ctx, phase2, &env2)
		results <- phaseResult{idx: idx2, result: res, err: err}
	}()

	// Wait for both to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	for pr := range results {
		phase := r.Config.Phases[pr.idx]
		if pr.err != nil || (pr.result != nil && pr.result.ExitCode != 0) {
			cancel() // cancel the other goroutine
			errMsg := "non-zero exit"
			if pr.err != nil {
				errMsg = pr.err.Error()
			}
			ux.PhaseFail(pr.idx, phase.Name, errMsg)
			if firstErr == nil {
				firstErr = fmt.Errorf("phase %q failed: %s", phase.Name, errMsg)
			}
		} else {
			state.RecordEnd(r.Env.ArtifactsDir, phase.Name)
			ux.PhaseComplete(pr.idx, time.Since(start))
		}
	}

	if firstErr != nil {
		r.State.Status = "failed"
		r.State.Save(r.Env.ArtifactsDir)
		ux.ResumeHint(r.State.Ticket)
		return firstErr
	}

	// Advance past both phases — set to the one after the later index
	if idx2 > idx1 {
		r.State.SetPhase(idx2 + 1)
	} else {
		r.State.SetPhase(idx1 + 1)
	}
	r.State.Save(r.Env.ArtifactsDir)
	return nil
}

// evalCondition runs a shell command and returns true if it exits 0.
func evalCondition(ctx context.Context, condition string, env *dispatch.Environment) bool {
	cmd := exec.CommandContext(ctx, "bash", "-c", condition)
	cmd.Dir = env.WorkDir
	cmd.Env = dispatch.BuildEnv(env)
	return cmd.Run() == nil
}
