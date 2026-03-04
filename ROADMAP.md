# orc Product Roadmap

## Vision

orc becomes a **deterministic, auditable AI code delivery engine** that JBD deploys on client codebases. The product isn't "AI writes code" — it's "every ticket follows a guaranteed process with adversarial quality assurance, full cost visibility, and auditable artifacts." The config and prompts are the IP. The engine is the plumbing.

## Strategic Context

**Where orc is today:** A working CLI that drives multi-phase AI workflows as a state machine. Two real-world deployments — kitchen-scheduler (9 phases, 3 retry loops, Jira integration, worktree isolation, self-review) and orc's own dogfooding config (5 phases). 27 commits, 189 tests, MIT license. Hardwired to Claude CLI.

**Where orc needs to be:** A product jb can deploy on any client codebase within an afternoon. Set up the `.orc/` directory, tune the prompts for the domain, and hand the client a workflow that enforces plan → implement → review → quality → ship on every ticket. Cost-tracked, reported, reproducible.

**Audience progression:**
1. jb on his own projects (now — validated)
2. jb on client projects (next week — needs cost tracking, reporting, config speed)
3. Other engineers (month 2 — needs docs, distribution, polish)
4. Clients self-serving with JBD-maintained configs (quarter 2 — needs audit, multi-project) [[JBD = JorgeBarreto.Dev]]

## Design Principles

1. **Script phases are the extensibility mechanism.** orc should NOT build integrations for Jira, GitHub, git, or test frameworks. These belong in script phases. The kitchen-scheduler proves this works.

2. **Files on disk are the communication layer.** Phases communicate through artifact files. No hidden state, no in-memory handoffs. Everything is inspectable.

3. **The config is the product.** orc's engine is plumbing. The value is in the phase definitions, prompt templates, and review criteria that JBD tunes per client.

4. **Composability over features.** Simple primitives that combine well beat special-purpose features. `on-fail` + `loop` + `condition` + `parallel-with` cover an enormous design space.

5. **Validate fast, fail loud.** Bad configs should fail at parse time, not 20 minutes into a run. Every new feature needs validation rules.

---

## Wave 0: Foundation

**Theme:** Quick wins that unblock everything else. All items are independent, small, and immediately useful.

**Validation checkpoint:** After Wave 0, jb can run the kitchen-scheduler workflow with a cleaner config (less `cwd` repetition), see what each run costs (or token counts if on a subscription plan), wrap orc in a Ralph loop with proper exit codes, run multiple tickets concurrently without artifact collision, and see a clear summary table at the end of every run showing where time was spent.

---

### R-001: Close Issue #1 — Feedback injection is implemented

Commit `1892763` ("Add gate feedback collection and auto-inject feedback into agent prompts") implemented the core of Issue #1. The agent executor reads all files from `artifacts/feedback/` and appends them to the rendered prompt. Gate phases also write their feedback input to the feedback directory.

**What to do:**
- Verify the implementation works end-to-end (run a workflow with an on-fail loop, confirm feedback appears in the rendered prompt at `artifacts/prompts/`)
- Close Issue #1 on GitHub with a reference to the commit

**Acceptance criteria:**
- Issue #1 is closed
- A brief comment on the issue confirms the commit that implemented it
- End-to-end verification: run a workflow with an on-fail loop, confirm feedback from the failed phase appears in the rendered prompt saved at `artifacts/prompts/`

**Priority:** P0
**Effort:** Small
**Dependencies:** None

---

### R-002: Top-level phase defaults — model, cwd, effort

Three fields that every phase in a project typically shares. Currently each phase repeats `model: opus` and `cwd: $WORKTREE`. A top-level default eliminates the repetition and makes configs easier to read and maintain.

**Config schema change:**
```yaml
name: prepdesk
ticket-pattern: 'KS-\d+'
model: opus           # default for all agent phases
cwd: $WORKTREE        # default for all script/agent phases
effort: high          # default for all agent phases

vars:
  WORKTREE: $PROJECT_ROOT/.worktrees/$TICKET

phases:
  - name: plan
    type: agent
    prompt: .orc/phases/plan.md
    # inherits model: opus, cwd: $WORKTREE, effort: high

  - name: implement
    type: agent
    prompt: .orc/phases/implement.md
    model: sonnet    # override for this phase

  - name: quality
    type: script
    run: bash .orc/scripts/quality.sh
    # inherits cwd: $WORKTREE
```

**Implementation approach:**
- Add `Model`, `Cwd`, `Effort` fields to `config.Config` struct
- In `validate.go`, validate top-level fields with the same rules as per-phase fields
- During validation, phases without explicit values inherit from config-level (before applying type-specific defaults like model=opus)
- Per-phase values always override top-level values
- `orc doctor` reads the top-level model from config.yaml (if present) and uses it for the diagnosis claude call instead of hardcoding opus. `orc init` continues to use opus (no config exists yet to read from)
- Update `internal/docs/content.go` with the new fields

**Acceptance criteria:**
- Top-level `model`, `cwd`, `effort` fields are parsed and validated
- Per-phase values override top-level defaults
- Existing configs without top-level fields work unchanged (backward compatible)
- `orc docs config` documents the new fields
- Kitchen-scheduler config can drop per-phase `model` and `cwd` repetition
- Tests cover: top-level only, per-phase override, no top-level (defaults apply), invalid top-level values rejected

**Priority:** P0
**Effort:** Small
**Dependencies:** None
**Subsumes:** GitHub Issue #5

---

### R-003: Exit code semantics

Currently `orc run` returns 0 on success and 1 on any error. For Ralph-wrapping and CI/CD integration, richer exit codes are needed.

**Exit codes:**
- `0` — Workflow completed successfully. All phases passed.
- `1` — Retryable failure. An on-fail loop exceeded its max, an agent phase failed, a timeout was hit. A fresh `orc run` might succeed (especially with Ralph wrapping, where fresh context helps).
- `2` — Human intervention needed. A gate was denied, or a phase produced an unrecoverable error that won't be fixed by retrying. Don't retry automatically.
- `3` — Configuration or setup error. Config invalid, prompt file missing, required binary not found. Fix the config before retrying.

**Implementation approach:**
- Define exit code constants in a new file or in `cmd/orc/main.go`
- `runner.Run()` currently returns plain `error` values wrapped with `%w`. Define typed errors (or sentinel errors) that carry exit code information, so `main.go` can switch on error type to determine the exit code
- `main.go` currently has a single `os.Exit(1)` on any error (line ~39). Replace with a type switch on the returned error to select the appropriate exit code
- Signal interrupts (Ctrl+C, SIGTERM) should return a distinct exit code — conventionally 130 for SIGINT — rather than mapping to exit 1

**Acceptance criteria:**
- `orc run` returns 0, 1, 2, or 3 depending on failure type
- Signal interrupts (SIGINT/SIGTERM) return exit code 130 (conventional for SIGINT)
- Exit codes are documented in `orc docs runner`
- A Ralph wrapper script can check `$?` and decide whether to retry
- Tests verify exit codes for: success, agent failure, gate denial, config error, loop max exceeded, signal interrupt

**Priority:** P0
**Effort:** Small
**Dependencies:** None

---

### R-004: Cost and token tracking from stream output

The stream parser (`internal/dispatch/stream.go`) already extracts `cost_usd` from the `result` message in Claude's stream-json output into `StreamResult.CostUSD`. But `RunAgent()` only returns the text output — the cost value is extracted and then discarded. Save it to artifacts for reporting. We must remember that there will be no true costs until we move to api/sdk mode -- for the majority of the dev livecycle of orc, we'll be on a subscription plan.

**What to track:**
- `cost_usd` — API cost per agent turn (may be 0 or absent on subscription plans)
- `input_tokens`, `output_tokens` — Token counts per turn (extract from result payload if available)
- Aggregate per phase (sum across turns for attended mode with multiple resumes)
- Aggregate per run (sum across phases)

**Storage:** `artifacts/costs.json`
```json
{
  "phases": [
    {
      "name": "plan",
      "phase_index": 0,
      "cost_usd": 0.42,
      "input_tokens": 15000,
      "output_tokens": 8000,
      "turns": 1
    },
    {
      "name": "implement",
      "phase_index": 2,
      "cost_usd": 1.87,
      "input_tokens": 45000,
      "output_tokens": 22000,
      "turns": 3
    }
  ],
  "total_cost_usd": 2.29,
  "total_input_tokens": 60000,
  "total_output_tokens": 30000
}
```

**Implementation approach:**
- Extend `dispatch.Result` (currently just `ExitCode` + `Output` in `internal/dispatch/dispatch.go`) to carry cost and token fields
- In `stream.go`, add `InputTokens` and `OutputTokens` fields to the `resultPayload` struct (currently only has `CostUSD`, `SessionID`, `PermissionDenials`) and extract them from the Claude stream-json `result` payload if present. Note: determine actual JSON field names from Claude CLI stream-json output (likely `input_tokens` and `output_tokens` based on Anthropic API conventions — verify with `claude -p --output-format stream-json`)
- In `runner.go`, after each agent phase completes, append to costs.json (using the same atomic write pattern)
- Gracefully handle missing cost data (subscription mode) — show token counts if cost is 0
- Note: rich cost display in `orc status` is handled by R-015. R-004's scope is data collection and persistence

**Acceptance criteria:**
- costs.json is created in artifacts/ after any agent phase runs
- Per-phase and total costs are tracked
- Missing cost data (subscription mode) doesn't cause errors — costs.json is still written with `cost_usd: 0`
- Token counts are tracked when available in the stream-json `result` payload. If token count fields are not present (verify actual field names with `claude -p --output-format stream-json`), costs.json is written with `input_tokens: 0` and `output_tokens: 0`, and a warning is logged. Token field extraction is best-effort in v1
- Note: `orc status` cost display is in R-015's scope. R-004 delivers the data file that R-015 reads

**Priority:** P0
**Effort:** Medium
**Dependencies:** None

---

### R-030: Per-ticket artifact isolation

Currently all runs share a single `.orc/artifacts/` directory. If you `orc run KS-42` and `orc run KS-43` simultaneously — or even sequentially without canceling — state.json, timing.json, costs.json, and feedback files collide. For client deployment with multiple tickets in flight, this is a data corruption risk.

**Design:**
```
.orc/artifacts/
  KS-42/
    state.json
    timing.json
    costs.json
    logs/
    prompts/
    feedback/
  KS-43/
    state.json
    ...
```

The ticket ID becomes the artifact namespace. All artifact paths gain a ticket prefix. `$ARTIFACTS_DIR` resolves to `.orc/artifacts/<ticket>/` instead of `.orc/artifacts/`.

**Implementation approach (scoped v1 for Wave 0):**
- `main.go` sets `env.ArtifactsDir` to `.orc/artifacts/<ticket>/` (currently hardcoded to `.orc/artifacts/`)
- `state.EnsureDir()` creates the ticket-scoped directory
- `orc status <ticket>` and `orc cancel <ticket>` update their `artifactsDir` construction to `.orc/artifacts/<ticket>/` (same one-line path change as `orc run` — currently both hardcode `.orc/artifacts/` in `main.go`)
- History (R-014) nests under the ticket dir: `.orc/artifacts/KS-42/history/`
- **Deferred to follow-up:** `orc status` multi-ticket listing (when no ticket arg is given), `orc cancel` migration warning for old flat `.orc/artifacts/state.json`, and multi-ticket status display. The basic ticket-scoped path construction for `orc status <ticket>` and `orc cancel <ticket>` is included in v1.

**Acceptance criteria:**
- Two concurrent `orc run` invocations with different tickets do not interfere
- `$ARTIFACTS_DIR` resolves to the ticket-scoped path in all phase types
- `orc status <ticket>` and `orc cancel <ticket>` read from/operate on `.orc/artifacts/<ticket>/`
- Existing single-ticket workflows work unchanged (ticket is required, as it is today)
- No artifact files are written outside the ticket directory

**Priority:** P0
**Effort:** Small
**Dependencies:** None

---

### R-005: `orc validate` command

Validate a config without running it. Currently validation happens inside `orc run` after loading the config. Extract to a standalone command for fast feedback during config authoring.

**Usage:**
```bash
orc validate                    # validate .orc/config.yaml in current project
orc validate --config path/to/config.yaml  # validate a specific file
```

**What it validates:**
- Everything `config.Load()` → `config.Validate()` already validates (structure, types, references, file existence — see `internal/config/validate.go`)
- Additionally: expand all variables and print them (so the user can see what `$WORKTREE` resolves to)
- Print a summary: phase count, phase types, loop relationships, parallel pairs

**Implementation approach:**
- New `validateCmd()` in `main.go`
- Calls `findProjectRoot()` + `config.Load()` (or loads from --config path)
- On success, prints a summary of the config (similar to dry-run but without the full prompt rendering)
- On failure, prints all validation errors

**Acceptance criteria:**
- `orc validate` exits 0 on valid config, non-zero on invalid
- Each validation error specifies: the field or phase that failed, why it's invalid, and the expected format or valid options
- Variable expansion is shown in output (each `$VAR` and its resolved value)
- Phase summary shows types, loops, parallel relationships

**Priority:** P1
**Effort:** Small
**Dependencies:** None

---

### R-035: Run summary table

When a run finishes (success or failure), print a timeline table showing every phase that executed, how many times it ran, how long it took, and whether it passed or failed. Currently `ux.Success()` prints "All N phases complete" with no detail. For a multi-phase workflow with retry loops, that's not enough — you want to see at a glance where the time went and which phases looped.

**Output format:**
```
  Run complete — 7 phases, 4m 32s

  #  Phase          Type    Runs  Duration  Result
  1  plan           agent      1     1m 12s  pass
  2  review-plan    agent      1       38s   pass
  3  plan-check     script     1        0s   pass
  4  implement      agent      3     2m 05s  pass
  5  test           script     3       22s   pass
  6  review         agent      1       12s   pass
  7  review-check   script     1        0s   pass

  Total agent time: 4m 07s
  Total script time: 22s
```

On failure, the table still prints (up to the phase that failed), with the failing phase marked `FAIL`.

**Data sources:**
- `timing.json` — already tracks per-phase start/end times via `state.Timing` (loaded in `runner.go`)
- `loop-counts.json` — already tracks retry counts via `state.LoadLoopCounts()` (loop count + 1 = total runs for that phase)
- Phase type comes from `config.Phases[i].Type`
- Pass/fail is known from the runner's execution path

**Implementation approach:**
- Add `ux.RunSummary(phases []config.Phase, timing *state.Timing, loopCounts map[string]int, failedPhase int)` function in `internal/ux/output.go`
- Called from `runner.Run()` at end of run (both success and failure paths) — replaces or augments the existing `ux.Success()` call
- Compute run count per phase: `loopCounts[phase.Name] + 1` (0 retries = 1 run). For phases that were skipped (condition was false), show "skip" in the Result column
- Compute duration from timing entries: `end - start` for each phase. For phases that ran multiple times, the timing only captures the last execution's start/end — sum is not available in v1. Show the last execution's duration with a note
- For phases that ran but have no timing entry (e.g., script phases that complete instantly), show `<1s`
- Column alignment: fixed-width columns, right-align numeric columns
- Cost column is deferred to when R-004 is done — the table structure supports adding it later

**Acceptance criteria:**
- Every completed run (success or failure) prints a summary table to stdout
- Table shows: phase number, name, type, run count, duration, result (pass/fail/skip)
- Duration is human-readable (Xs, Xm Ys format)
- Failed phase is clearly marked
- Skipped phases (condition=false) show "skip"
- Table aligns correctly for phase names up to 20 characters
- Tests verify table formatting for: all-pass run, run with retries, run with failure, run with skipped phases

**Priority:** P0
**Effort:** Small
**Dependencies:** None (uses existing timing.json and loop-counts.json)

---

### R-039: loop.check feedback uses declared outputs, not check stdout

**Bug:** When `loop.check` fails, the runner writes the check command's stdout as feedback via `handleLoopFailure`. But check commands like `test -f $ARTIFACTS_DIR/review-pass.txt` are pure pass/fail signals — they produce no output. So `feedback/from-<phase>.md` is written with empty content, and the feedback injection in `RenderAndSavePrompt` skips it (empty files are filtered). The downstream agent gets no feedback and re-implements blindly.

**Root cause:** `runner.go:254-262` — `runLoopCheck` returns `checkOutput` (the check command's stdout), which is passed to `handleLoopFailure` as the feedback content. For signal-type checks, this is always empty.

**The semantic model:** The check command is a gate (pass/fail). The phase's declared outputs (e.g., `review-findings.md`) are the content. Feedback should always come from the declared outputs, never from the check's stdout.

**Fix:**
- Add a helper `readDeclaredOutputs(artifactsDir string, outputs []string) string` that reads and concatenates the phase's declared output artifact files
- In the `loop.check` failure block, pass `readDeclaredOutputs(...)` to `handleLoopFailure` instead of `checkOutput`
- This flows naturally through the existing on-exhaust path too (the `output` parameter in `handleLoopFailure` will already contain the declared outputs)

**Key files:**
- `internal/runner/runner.go` — lines 252-268 (loop.check failure block), `handleLoopFailure`
- `internal/state/artifacts.go` — `WriteFeedback`, `ReadAllFeedback` (these are fine, the bug is upstream)

**Acceptance criteria:**
- When `loop.check` fails, `feedback/from-<phase>.md` contains the content of the phase's declared output artifacts (not the check command's stdout)
- Feedback injection in `RenderAndSavePrompt` delivers this content to the next agent automatically
- Existing tests pass; new test verifies declared outputs are used as feedback on check failure

**Priority:** P0 (blocks all loop.check workflows from converging)
**Effort:** Small
**Dependencies:** None

---

## Wave 1: Convergent Quality

**Theme:** The core product differentiator. The convergent review loop is what makes orc more than "run these phases in order" — it's "iterate until adversarial quality checks pass, with a guaranteed minimum number of review cycles."

**Validation checkpoint:** After Wave 1, jb can configure a workflow where every ticket gets at least 3 independent review passes before the PR is created, iterate on configs quickly with `orc improve`, and set cost limits to prevent runaway agents. This is the quality assurance and config iteration story for client deployment.

---

### R-006: Convergent review loop — the `loop` field

A new config field that enables deliberate iteration between phases, distinct from `on-fail` error recovery.

**Key distinction from `on-fail`:**
- `on-fail` is reactive: "if this phase fails, jump back and retry." It handles errors.
- `loop` is proactive: "iterate between these phases at least N times." It enforces quality.
- They can coexist: `loop` handles inner convergence, `on-fail` handles outer recovery when convergence fails.

**Config syntax:**
```yaml
- name: implement
  type: agent
  prompt: .orc/phases/implement.md
  model: opus

- name: review
  type: agent
  prompt: .orc/phases/review.md
  model: opus
  outputs:
    - review-findings.md
  loop:
    goto: implement
    min: 3
    max: 5
```

**Semantics:**
- `loop.goto` — Name of an earlier phase to jump back to. Same constraint as `on-fail.goto`: must reference a phase that appears earlier in the config.
- `loop.min` — Minimum number of iterations through the loop before it can break on a passing phase. Default: 1 (which means "break on first pass," equivalent to on-fail semantics). Setting min > 1 forces multiple passes.
- `loop.max` — Maximum iterations. Required. When exceeded, the loop fails. If `on-fail` is also present, the on-fail handler triggers. Otherwise, the workflow stops.
- **Iteration counting:** An iteration is one execution of the loop-owning phase. The counter starts at 1 on the first execution. On every subsequent loop-back, it increments.
- **Break condition:** iteration >= min AND phase exit code is 0 (success).
- **Loop-back condition:** phase exit code != 0 (failure at any iteration), OR iteration < min (forced extra pass).
- **Feedback on loop-back:** The phase's output is written to `artifacts/feedback/from-<phase>.md` on every loop-back, whether the phase passed or failed. When looping back on a pass (iteration < min), the output still contains the review findings, which the implement agent can use.

**Composition with on-fail:**
```yaml
- name: plan
  type: agent
  prompt: .orc/phases/plan.md

- name: implement
  type: agent
  prompt: .orc/phases/implement.md

- name: review
  type: agent
  prompt: .orc/phases/review.md
  loop:
    goto: implement
    min: 3
    max: 5
  on-fail:
    goto: plan
    max: 2
```

Inner loop: implement ↔ review (converge on implementation quality).
Outer recovery: if the loop exceeds max (5 iterations without convergence), on-fail triggers and jumps back to plan. Plan re-runs with feedback about why implementation couldn't converge. Then implement ↔ review loop restarts with a fresh plan.

**When on-fail triggers from loop failure:**
- The loop counter resets (fresh convergence attempt with new plan)
- The on-fail counter increments
- If on-fail also exceeds max, the workflow stops
- **Feedback content:** The feedback file for the on-fail target phase contains the LAST iteration's output prefixed with a header: `Convergence failed after N iterations (min: M, max: X). Last iteration output follows:`. This gives the target phase both the signal (convergence failed) and the most recent context to inform the retry.

**Validation rules:**
- `loop.goto` must reference an earlier phase (no forward jumps)
- `loop.min` must be >= 1 (default 1)
- `loop.max` must be >= loop.min
- Neither phase in a `parallel-with` pair can have a `loop` field (the parallel runner doesn't handle loop semantics)
- `loop` CAN coexist with `on-fail` (they serve different purposes)
- If both `loop` and `on-fail` are present, `loop.goto` and `on-fail.goto` CAN point to the same or different phases
- v1 constraint: only one phase per config can have a `loop` field. Relaxed in future if needed.
- **Loop-back target (resolved):** v1 uses the full-loop approach (`goto→phase`). When iteration < min, the loop jumps back to `loop.goto` (e.g., implement), not just re-runs the review phase. This is simpler to implement (reuses the on-fail jump mechanism), provides the implement agent with review feedback on each pass, and can be revisited if testing shows review-only passes produce better results.

**Implementation approach:**
- Add `Loop *Loop` field to `config.Phase` struct with `Goto`, `Min`, `Max` fields
- Validation in `validate.go`: all the rules above
- Runner changes in `runner.go`: Loop handling requires modification in **two** code paths.
  (1) **Error path** (inside the `if err != nil || result.ExitCode != 0` block, ~line 123): Check for `loop` field before checking `on-fail`. If loop exists and iteration < max, write feedback and loop back to `loop.goto`. If iteration >= max, trigger on-fail if present, else fail.
  (2) **Success path** (after the output check at ~line 200, before phase advance at ~line 207): If loop exists and iteration < min, write feedback and loop back to `loop.goto` (forced extra pass — the phase passed but hasn't met the minimum iteration count). If iteration >= min, break the loop and advance normally.
- **Counter key migration (breaking change to internal format):** The existing on-fail implementation uses `loopCounts[phase.Name]` as a plain key (e.g., `{"review": 2}` — see runner.go ~line 136-143). Adding loop counters requires migrating to a namespaced key format: `loopCounts[phase.Name+":on-fail"]` for on-fail counters, `loopCounts[phase.Name+":loop"]` for loop counters. The flat key format (`"phase:type"`) is preferred over nested objects because it preserves the existing `map[string]int` type and `state.LoadLoopCounts`/`SaveLoopCounts` functions unchanged. All existing `loopCounts[phase.Name]` references in runner.go must be updated to `loopCounts[phase.Name+":on-fail"]` simultaneously. No migration of existing loop-counts.json files is needed — loop-counts.json is transient (reset on `--retry`/`--from`) and only exists during active runs.
- When loop triggers on-fail, reset the loop counter for that phase.
- Feedback writing: reuse existing `state.WriteFeedback()` for both pass and fail loop-backs.
- Dry-run: show loop relationships in the phase plan.

**Acceptance criteria:**
- `loop` field is parsed, validated, and executed correctly
- Loop breaks when: iteration >= min AND phase passes
- Loop continues when: phase fails OR iteration < min
- Loop failure (iteration >= max) triggers on-fail if present
- Loop counter resets when on-fail triggers
- Feedback is written on every loop-back (pass or fail)
- `orc status` shows loop iteration count (minimal raw count display; polished presentation is R-015's scope)
- Dry-run shows loop relationships
- Tests cover: basic loop, min enforcement, max enforcement, loop+on-fail composition, loop counter reset, feedback on pass loop-back

**Priority:** P0
**Effort:** Medium
**Dependencies:** None (requires feedback injection from commit `1892763` to be working; R-001 verifies this)

---

### R-007: Review prompt templates

Generic, battle-tested review prompts that work across codebases. These are the starting point for the adversarial review loop. Projects customize them, but the generic versions should catch common issues.

**Templates to create:**

1. **`review-code.md`** — General code review prompt
   - Read the PR diff (or recent commits)
   - Check for: logic errors, missing error handling, security issues (OWASP top 10), test coverage gaps, hardcoded values, missing input validation
   - Classify findings as blocking vs. non-blocking
   - Write findings to `$ARTIFACTS_DIR/review-findings.md`
   - Write `$ARTIFACTS_DIR/review-pass.txt` only if no blocking issues
   - Exit 0 if no blocking issues, exit 1 if blocking issues found

2. **`review-security.md`** — Security-focused review
   - OWASP Top 10 checklist
   - Dependency vulnerability scan
   - Secrets detection (hardcoded API keys, passwords)
   - Input validation on all external boundaries
   - SQL injection, XSS, command injection checks

3. **`review-architecture.md`** — Architecture review
   - Does the implementation match the plan?
   - Are there unnecessary abstractions or missing abstractions?
   - Is the code consistent with existing codebase patterns?
   - Are there circular dependencies or layering violations?

**Where they live:** `.orc/templates/` in the orc repo, copied by `orc init --recipe` (see R-012).

**Prompt design principles (from kitchen-scheduler experience):**
- Prompts must be self-contained — the agent has no prior context
- Reference specific artifact files (`$ARTIFACTS_DIR/plan.md`, `$ARTIFACTS_DIR/review-findings.md`)
- Include explicit exit code instructions ("exit 1 if blocking issues found")
- Handle retry loops ("check `$ARTIFACTS_DIR/feedback/` for previous findings")
- Be specific about output format (structured markdown with file:line references)

**Acceptance criteria:**
- At least 2 review prompt templates exist and produce structured output (blocking/non-blocking findings with file:line references) when run against the orc codebase via `claude -p` with appropriate variable substitution (or via `orc run` in a test config that includes only the review phase). Integration with `orc test` (R-013) is verified as part of R-013's acceptance criteria
- Templates use orc variable substitution correctly (`$ARTIFACTS_DIR`, `$TICKET`, feedback injection)
- Templates handle first-run and retry-loop scenarios (check `$ARTIFACTS_DIR/feedback/` for previous findings)
- Templates are included in `orc init` recipe options (R-012)

**Priority:** P1
**Effort:** Medium (mostly content work, needs testing)
**Dependencies:** None (benefits from R-006 — loop field enables minimum-iteration enforcement; prompts work with existing on-fail mechanism in the meantime)

---

### R-009: `orc improve` — AI-assisted workflow refinement (Issue #4)

Already designed in PLAN.md. Two modes: one-shot (apply a specific change to the config) and interactive (chat with Claude about the workflow).

**One-shot mode:**
```bash
orc improve "add a lint phase parallel with tests"
orc improve "change the review prompt to also check for accessibility"
orc improve "split the implement phase into backend and frontend"
```

Reads current config + prompts, sends to Claude with schema docs + instruction, parses file-block output, validates, writes changed files.

**Interactive mode:**
```bash
orc improve
```

Builds context (schema + current config + prompts), launches Claude in interactive mode with context pre-loaded. The user chats about their workflow, Claude edits `.orc/` files directly.

**Implementation:** Follow the existing PLAN.md design. Key architectural decisions: file-block parsing approach for one-shot output, validation-before-write step (parsed config is validated via `config.Load()` before writing to disk), and `FilteredEnv()` extraction to `dispatch` package (shared by scaffold, doctor, improve). Key files:
- `internal/improve/improve.go` — OneShot and Interactive functions
- `internal/improve/prompt.go` — Prompt builders
- `internal/improve/improve_test.go` — Tests
- Extract `FilteredEnv()` to `dispatch` package (shared by scaffold, doctor, improve)

**Acceptance criteria:**
- `orc improve "instruction"` modifies config/prompts and prints what changed
- `orc improve` launches interactive Claude session with workflow context
- Invalid config changes are rejected before writing
- Files outside `.orc/` are ignored
- Tests cover: happy path (valid instruction modifies config), prompt building includes schema docs + current config + current prompts, invalid config changes are rejected before writing to disk, files outside `.orc/` are not written

**Priority:** P0
**Effort:** Medium
**Dependencies:** None (design is done in PLAN.md)

---

### R-031: Cost limits — per-phase and per-run budgets

R-004 tracks costs after the fact. Cost limits prevent runaway agents before they burn through the budget. A bad prompt in a retry loop could spend $50+ before anyone notices. For client deployment, this is the scariest failure mode.

**Config:**
```yaml
max-cost: 10.00          # per-run budget in USD

phases:
  - name: implement
    type: agent
    prompt: .orc/phases/implement.md
    max-cost: 5.00       # per-phase budget (overrides run-level for this phase)
```

**Behavior:**
- The runner tracks cumulative cost across phases (from R-004's costs.json data)
- Before each agent phase starts, check if the run budget would be exceeded based on cost so far
- After each agent phase completes, the phase's accumulated cost is checked against its per-phase limit. If exceeded, the workflow stops before the next phase starts
- When a cost limit is hit: exit code 2 (human intervention needed, per R-003), clear error message ("Phase 'implement' exceeded cost limit: $5.12 > $5.00"), state saved as failed
- Cost limits are optional — omitting them means no limit (current behavior)

**Implementation approach:**
- Add `MaxCost` to `config.Config` (run-level) and `config.Phase` (phase-level)
- Validation: `max-cost` must be > 0 if present
- Runner checks cumulative cost before dispatching each agent phase
- v1: check cost after each phase completes, not mid-stream. Mid-stream limiting (killing a phase while it runs) is a future enhancement — it requires a callback/channel mechanism in the stream parser and token-count-based cost estimation, since `cost_usd` is only available in the final `result` event

**Acceptance criteria:**
- Per-run cost limit: before each phase starts, cumulative cost is checked; if the budget is already exceeded, the workflow stops before the next phase starts with exit code 2
- Per-phase cost limit: after each phase completes, the phase's cost is checked; if the phase exceeded its budget, the workflow stops before the next phase starts with exit code 2 and a clear error message (e.g., "Phase 'implement' exceeded cost limit: $5.12 > $5.00"). Note: mid-stream cost limiting (killing a phase while it runs) is a future enhancement
- Cost limit violation produces exit code 2 with a clear error message
- Missing cost limits work unchanged (no limit enforced)
- Tests cover: run limit exceeded, phase limit exceeded, no limit set

**Priority:** P1
**Effort:** Medium
**Dependencies:** R-004 (cost tracking provides the data)

---

## Wave 2: Iteration Speed

**Theme:** Make it fast to create, test, and iterate on workflows. jb needs to set up orc on a new project quickly, tweak configs without full runs, and debug prompt issues efficiently.

**Validation checkpoint:** After Wave 2, jb can set up orc on a new client project in under 30 minutes: `orc init --recipe full-pipeline`, tweak the prompts, `orc validate`, `orc run TICKET-1 --dry-run`, then go.

---

### R-010: `--from` accepts phase names

Currently `--from` takes a 1-indexed phase number. This is error-prone — you have to count phases to find the right number. Allow phase names.

**Usage:**
```bash
orc run KS-42 --from implement     # start from the "implement" phase
orc run KS-42 --from 4             # still works with numbers
orc run KS-42 --retry implement    # same for --retry
```

**Implementation approach:**
- In `main.go`, check if the --from/--retry value is a number. If not, look it up by name in the config phases list.
- Error if the name doesn't match any phase.

**Acceptance criteria:**
- `--from` and `--retry` accept both numbers and phase names
- Unknown phase names produce a clear error listing available phases
- Existing numeric usage continues to work

**Priority:** P2
**Effort:** Small
**Dependencies:** None

---

### R-011: Enhanced dry-run with flow visualization

Current dry-run prints each phase with its config. Add a flow diagram showing the execution path, including loop relationships, on-fail jumps, parallel pairs, and conditions.

**Example output:**
```
Workflow: prepdesk (9 phases)

  1. setup [script]
  │
  2. plan [agent/opus]
  │   outputs: plan.md
  │
  3. approve [gate]
  │
  ┌─▶ 4. implement [agent/opus]
  │   │
  │   5. quality [script]
  │   │   on-fail ──┘ (max 3)
  │   │
  │   6. push-pr [script]
  │   │   outputs: pr.txt
  │   │
  │   7. ci-check [script]
  │   │   on-fail ──┘ (max 2)
  │   │
  │   8. self-review [agent/opus]
  │   │
  │   9. review-gate [script]
  │       on-fail ──┘ (max 2)
  │
  ✓ complete
```

With `loop` field (R-006):
```
  ┌─▶ 4. implement [agent/opus]
  │   │
  │   5. review [agent/opus]
  │       loop ──┘ (min 3, max 5)
  │       on-fail → 2. plan (max 2)
```

**Implementation approach:**
- New function in `ux/output.go` that takes config phases and renders the flow
- Call it from `DryRunPrint` in the runner (or replace the current dry-run output)
- Use box-drawing characters for the flow lines

**Acceptance criteria:**
- Dry-run shows a visual flow diagram
- on-fail jumps, loop relationships, parallel pairs, and conditions are visible
- Phase types, models, outputs, and timeouts are shown
- Works correctly for simple (3-phase) and complex (9-phase) configs

**Priority:** P2
**Effort:** Medium
**Dependencies:** None (benefits from R-006 — loop relationships shown when available)

---

### R-012: Prompt recipes for `orc init`

`orc init` currently generates a config via AI or falls back to a 3-phase template (plan + implement + review gate). Add recipe selection so users can scaffold proven workflow patterns.

**Recipes:**

1. **`simple`** (current fallback) — plan → implement → review gate. Good for exploratory work.

2. **`standard`** — plan → gate → implement → test (on-fail→implement) → review gate. The basic quality-assured pipeline.

3. **`full-pipeline`** — setup → plan → gate → implement → quality (on-fail→implement) → push-pr → self-review → review-gate (on-fail→implement). The kitchen-scheduler pattern, generalized.

4. **`review-loop`** — plan → implement → review (loop→implement, min 3, max 5). The convergent review loop pattern. Requires R-006.

**Usage:**
```bash
orc init                          # AI-generated (current behavior)
orc init --recipe standard        # scaffold from recipe
orc init --recipe full-pipeline   # scaffold the full pipeline
orc init --list-recipes           # show available recipes
```

**Implementation approach:**
- Recipes are Go templates (or raw strings) in the scaffold package
- Each recipe includes config.yaml and prompt files
- `--recipe` flag bypasses AI generation and directly writes the template
- Variables in templates are replaced with project-specific values (detected from the project, e.g., test command from Makefile/package.json)

**Acceptance criteria:**
- `orc init --recipe <name>` scaffolds a working config with prompt files
- `orc init --list-recipes` shows available recipes with descriptions
- Recipes produce valid configs (verified by config.Load in tests)
- At least 4 recipes available (simple, standard, full-pipeline, review-loop)

**Priority:** P1
**Effort:** Medium
**Dependencies:** R-006 (for review-loop recipe), R-007 (for review prompts)

---

### R-037: `orc init` accepts a prompt argument

`orc init` currently uses a fixed system prompt to analyze the project and generate a workflow. Allow users to pass a natural-language description as a positional argument to guide the AI generation toward a specific workflow shape.

**Usage:**
```bash
orc init "this is a documentation drafting project. it should be a draft->critique loop"
orc init "microservice with integration tests. heavy on bash scripts, no gates"
orc init "monorepo with 3 packages. each needs its own test phase"
```

**Implementation approach:**
- Accept an optional positional argument after `init`
- Append the user's description to the existing AI generation prompt (in `initPromptSuffix` or as a new section)
- The user prompt is additive — it supplements the project context, not replaces it
- If `--recipe` is also passed, the user prompt is ignored (recipes are deterministic)
- Falls back to current behavior when no argument is given

**Acceptance criteria:**
- `orc init "description"` passes the description to the AI and influences the generated config
- The description appears in the prompt sent to the agent (verifiable via tests or dry-run)
- No argument = current behavior unchanged
- `--recipe` takes precedence over a prompt argument

**Priority:** P2
**Effort:** Small
**Dependencies:** None

---

### R-013: `orc test` — single-phase execution

Run a single phase in isolation for testing prompts and scripts without running the entire workflow. Essential for prompt debugging.

**Usage:**
```bash
orc test plan KS-42              # run just the "plan" phase for ticket KS-42
orc test implement KS-42         # run just "implement"
orc test quality KS-42           # run just the quality script
```

**Behavior:**
- Sets up the environment (variables, artifacts dir) as if the full workflow were running
- Runs only the specified phase
- Does NOT modify state.json or advance the workflow
- Saves the phase output to a temporary location (or artifacts/) for inspection
- Useful for: testing a new prompt, debugging a script, verifying a review template

**Implementation approach:**
- New `testCmd()` in `main.go`
- Load config, find the named phase, set up dispatch environment (reuse `dispatch.Environment` setup from `runCmd()`)
- Call `dispatch.Dispatch()` for just that phase
- Print the result without modifying state (do not call `state.Save()`)

**Acceptance criteria:**
- `orc test <phase> <ticket>` runs a single phase
- Environment is set up correctly (all variables available)
- State is not modified
- Phase output is visible (stdout for scripts, log file for agents)
- Missing artifacts from prior phases produce a warning listing which files are absent and which earlier phases normally create them

**Priority:** P2
**Effort:** Small
**Dependencies:** None

---

### R-032: Step-through mode — `orc run --step`

Pause between phases to inspect artifacts and decide whether to continue, rewind, or abort. This is the debugging workflow for prompt iteration: run a phase, look at what it produced, decide if the prompt needs tweaking before continuing.

**Usage:**
```bash
orc run KS-42 --step
```

**Behavior at each pause:**
```
  ✓ Phase 2 complete (4m 30s)

  Artifacts written:
    plan.md (4.2 KB)

  [c]ontinue  [r]ewind to phase  [a]bort  [i]nspect artifact > _
```

- **continue** — proceed to the next phase
- **rewind N** or **rewind <name>** — jump back to a specific phase (same as `--from` but mid-run)
- **abort** — stop the run, save state as interrupted
- **inspect <file>** — print an artifact file to the terminal for quick review

**Implementation approach:**
- Add a `--step` flag to `orc run`
- After each phase completes successfully, call a new `ux.StepPrompt()` function that reads user input
- Rewind resets `state.PhaseIndex` and continues the loop (same mechanism as on-fail jump)
- Inspect reads from `$ARTIFACTS_DIR` and prints to stdout
- `--step` is incompatible with `--headless` (validation rejects the combination)

**Acceptance criteria:**
- `orc run --step` pauses after each phase with an interactive prompt
- Continue, rewind, abort, and inspect commands work
- Rewind preserves artifacts from completed phases
- State is saved correctly on abort
- `--step` + `--headless` produces a validation error
- `--step` + `--auto` produces a validation error (step-through is inherently interactive)

**Priority:** P1
**Effort:** Medium
**Dependencies:** None

---

### R-033: `orc debug` — phase execution analysis

When a prompt produces bad results, you need to understand why. `orc debug` shows what the agent saw, what it did, and where it went off track — without manually reading raw log files.

**Usage:**
```bash
orc debug plan                    # analyze the most recent "plan" phase execution
orc debug plan KS-42              # analyze a specific ticket's phase
orc debug 2                       # analyze phase by index
```

**Output:**
```
Phase: plan (agent/opus)
Duration: 4m 30s | Cost: $0.42 | Tokens: 15K in / 8K out

Rendered prompt: artifacts/prompts/phase-1.md (3.2 KB)
  Variables: TICKET=KS-42, ARTIFACTS_DIR=.orc/artifacts/KS-42, ...

Tool calls (12):
  1. Read CLAUDE.md
  2. Read src/handler.go
  3. Glob **/*_test.go
  4. Read src/handler_test.go
  ...
  11. Write artifacts/plan.md
  12. TaskUpdate {status: completed}

Artifacts written:
  plan.md (4.2 KB)

Feedback injected: none

Exit code: 0
```

**Implementation approach:**
- Parse the phase log file (`logs/phase-N.log`) and metadata (`logs/phase-N.meta.json` from R-019 if available)
- Extract tool call sequence from the stream-json log (tool names and key arguments are already captured by `toolUseSummary` in `stream.go`)
- To enable this without R-019: the stream parser already writes text output to the log file. Extend it to also write tool-use events as `[tool] Name(summary)` lines in the log, which `orc debug` can parse
- Show the rendered prompt path, injected feedback, variable values, and the tool call timeline
- Falls back gracefully when metadata isn't available (pre-R-019 runs)

**Acceptance criteria:**
- `orc debug <phase>` shows a structured summary of a phase execution
- Tool call sequence is displayed with names and key arguments
- Rendered prompt path, variables, and feedback injection are shown
- Works with current log format (no dependency on R-019, but richer with it)
- Missing log files produce a clear error

**Priority:** P2
**Effort:** Medium
**Dependencies:** None (richer output with R-019)

---

## Wave 3: Observability & History

**Theme:** Know what orc did, what it cost, and how it performed over time. Required for client deployment — stakeholders need visibility.

**Validation checkpoint:** After Wave 3, jb can show a client a run report with timing, costs, and phase outcomes. He can also look at historical runs to identify patterns (which phases are slowest, which loops trigger most often).

---

### R-008: `orc report` command — basic version

Generate a readable summary of a completed (or failed) run from the artifacts directory. This is what jb shows stakeholders: "Here's what orc did for this ticket."

**Usage:**
```bash
orc report                # report for current/most recent run
orc report PROJ-123       # report for specific ticket
orc report --json         # structured JSON output for tooling
```

**Report contents (markdown output):**
```markdown
# Run Report: KS-42

**Status:** Completed
**Duration:** 23m 45s
**Cost:** $2.29 (90,000 tokens)

## Phase Summary

| # | Phase | Type | Duration | Cost | Result |
|---|-------|------|----------|------|--------|
| 1 | setup | script | 12s | — | Pass |
| 2 | plan | agent | 4m 30s | $0.42 | Pass |
| 3 | approve | gate | 2m 15s | — | Approved |
| 4 | implement | agent | 8m 22s | $0.95 | Pass |
| 5 | quality | script | 1m 05s | — | Pass |
| 6 | push-pr | script | 8s | — | Pass |
| 7 | ci-check | script | 5m 30s | — | Pass |
| 8 | self-review | agent | 3m 15s | $0.72 | Pass |
| 9 | review-gate | script | 1s | — | Pass |

## Loop Activity

No retry loops triggered.

## Artifacts

- plan.md (4.2 KB)
- ticket.txt (1.1 KB)
- pr.txt (3 bytes)
- review-findings.md (2.8 KB)
- review-pass.txt (5 bytes)
```

**Implementation approach:**
- New `reportCmd()` in `main.go`
- Read artifacts: state.json, timing.json, costs.json, loop-counts.json
- Read artifact file listing
- Format as markdown table (default) or JSON (--json flag)
- For failed runs, include the failure phase, error output, and feedback files

**Acceptance criteria:**
- `orc report` produces a readable markdown summary
- Includes timing, cost (when available), phase outcomes, loop activity, and artifact listing
- `--json` flag produces machine-parseable output
- Works for completed, failed, and interrupted runs
- Missing data (no costs.json, no timing.json) produces a report with "—" placeholders in those columns instead of errors
- **Future enhancement:** After R-014 (run history) ships, extend `orc report <run-id>` to read from `history/<run-id>/`

**Priority:** P1
**Effort:** Medium
**Dependencies:** None (benefits from R-004 — cost data populates the cost column; without it, "—" is shown)

---

### R-014: Run history — persist across runs

Currently, `orc run` overwrites artifacts from the previous run. Run history preserves completed runs so you can look back.

**Design:**
- On workflow completion (or failure), copy the current ticket's artifact directory to `artifacts/<ticket>/history/<run-id>/` where `<run-id>` is a timestamp or short UUID. This nests under the ticket directory introduced by R-030.
- A new run starts with a fresh set of artifact files in the ticket directory (history subdirectory is preserved).
- `orc status` shows the current run. `orc history` shows past runs for a ticket.
- On `orc cancel`, the current artifacts are moved to history (not deleted entirely) unless `--purge` is passed.
- History directory is under `.orc/artifacts/<ticket>/` and excluded by `.gitignore`.

**Storage structure:**
```
.orc/
  artifacts/
    KS-42/                      # ticket-scoped (from R-030)
      state.json                # current run
      timing.json
      costs.json
      ...
      history/                  # past runs for this ticket
        2026-02-28T14:30:00/
          state.json
          timing.json
          costs.json
          ...
        2026-02-27T09:15:00/
          ...
    KS-43/
      ...
```

**Limits:**
- Keep the last N runs (configurable, default 10) to prevent unbounded disk usage
- `orc history --prune` to manually clean old runs

**Acceptance criteria:**
- Completed, failed, and interrupted runs are archived to history/
- New runs start with fresh artifacts
- `orc history` (or `orc history KS-42`) lists past runs for a ticket with status, date, duration
- `artifacts/<ticket>/history/<run-id>/` directory structure is compatible with R-008's future `orc report <run-id>` feature (artifacts are stored in the same layout as the ticket's working artifact directory)
- History is pruned at a configurable limit (top-level config field `history-limit`, default 10)
- Existing behavior (no history dir) works unchanged

**Priority:** P1
**Effort:** Medium
**Dependencies:** R-030 (history nests under ticket-scoped artifact directory; R-008 can be enhanced to support `orc report <run-id>` on historical runs after R-014 ships)

---

### R-015: Enhanced `orc status`

Improve the status display with cost data, loop iteration counts, and estimated progress.

**Current status shows:** ticket, state, completed phases with durations, remaining phases with types, artifacts listing.

**Enhanced status adds:**
- Cost data per phase (from costs.json)
- Loop iteration counts (from loop-counts.json)
- Total run cost and duration so far
- A progress bar or percentage
- For running workflows: which phase is currently executing, how long it's been running

**Implementation approach:**
- Read costs.json in `ux.RenderStatus()`
- Read loop-counts.json for iteration info
- Add elapsed time for the current phase (compare timing start with now)

**Acceptance criteria:**
- `orc status` shows cost data when available
- Loop iterations are displayed for phases with loops
- Currently-running phase shows elapsed time
- Total cost and duration are shown

**Priority:** P2
**Effort:** Small
**Dependencies:** None (benefits from R-004 for cost data, R-006 for loop data — displays what's available, shows "—" when data is absent)

---

### R-034: Eval framework — `orc eval`

Systematic prompt quality testing. Define test fixtures with known inputs and scoring rubrics, run the workflow against them, and measure output quality. This is what makes prompt iteration empirical instead of vibes-based.

**Fixture structure:**
```
.orc/evals/
  happy-path/
    fixture.yaml           # git ref, ticket, variables
    rubric.yaml            # scoring criteria
  no-tests/
    fixture.yaml
    rubric.yaml
  vague-ticket/
    fixture.yaml
    rubric.yaml
```

**Fixture definition:**
```yaml
# .orc/evals/happy-path/fixture.yaml
ref: abc123f               # git commit to check out (the starting state)
ticket: EVAL-001           # ticket ID for this eval case
vars:                      # additional variables
  JIRA_TICKET: "KS-42"
description: "Clean codebase with clear ticket, tests present"
```

**Rubric definition:**
```yaml
# .orc/evals/happy-path/rubric.yaml
criteria:
  - name: compiles
    check: "make build"
    expect: "exit 0"
    weight: 5

  - name: tests-pass
    check: "make test"
    expect: "exit 0"
    weight: 5

  - name: has-plan
    check: "test -f $ARTIFACTS_DIR/plan.md"
    expect: "exit 0"
    weight: 2

  - name: review-passes
    check: "test -f $ARTIFACTS_DIR/review-pass.txt"
    expect: "exit 0"
    weight: 4

  - name: quality
    judge: true
    prompt: .orc/evals/happy-path/judge.md
    expect: ">= 7"
    weight: 2
```

Criteria are either script checks (objective — `check` runs a shell command, `expect: "exit 0"` means exit code 0 = pass, non-zero = fail) or agent-as-judge (subjective — `judge: true` invokes `claude -p` with the judge prompt, which must output a numeric score 1-10, `expect: ">= 7"` means score >= 7 = pass). Weights determine relative importance in the composite score.

**Usage:**
```bash
orc eval                          # run all eval cases
orc eval happy-path               # run a specific case
orc eval --report                 # show score history across runs
```

**How it works:**
1. For each eval case: create a git worktree at the fixture ref
2. Run the full workflow (`orc run`) in the worktree
3. After completion (or failure), evaluate each rubric criterion
4. Compute weighted score (0-100)
5. Append scores to `.orc/eval-history.json` with timestamp and prompt hash
6. Clean up worktree
7. Print score report

**Score history:**
```json
{
  "runs": [
    {
      "timestamp": "2026-03-01T15:30:00Z",
      "prompt_hash": "abc123",
      "cases": {
        "happy-path": {"score": 85, "details": {"compiles": 1.0, "tests-pass": 1.0, "quality": 0.7}},
        "vague-ticket": {"score": 62, "details": {"compiles": 1.0, "tests-pass": 0.5, "quality": 0.4}}
      }
    }
  ]
}
```

**Implementation approach:**
- New `internal/eval/` package
- New `evalCmd()` in `main.go`
- Worktree creation uses `git worktree add` (same pattern as kitchen-scheduler's setup script)
- Script criteria: run via `exec.CommandContext("bash", "-c", check)`, pass/fail based on exit code
- Judge criteria: invoke `claude -p` with the judge prompt + workflow output. The judge prompt must instruct the agent to output a line matching `SCORE: <N>` (e.g., `SCORE: 7`). orc extracts the score by regex-matching the last `SCORE: \d+` line in stdout
- Prompt hash: hash all `.orc/phases/*.md` files to fingerprint the prompt version

**Acceptance criteria:**
- `orc eval` runs all eval cases and produces a score report
- Each case runs in an isolated git worktree
- Script criteria produce pass/fail scores
- Judge criteria produce numeric scores (1-10, normalized to 0-1)
- Scores are persisted to eval-history.json with prompt hash
- `orc eval --report` shows score trends across prompt versions
- Eval cases are discoverable (`orc eval --list`)

**Priority:** P2
**Effort:** Large
**Dependencies:** None (benefits from R-014 for history, R-004 for cost tracking of eval runs)

---

## Wave 4: Client Readiness

**Theme:** Polish and operational features needed before deploying orc on a client codebase. Professional output, headless operation, robust error handling.

**Validation checkpoint:** After Wave 4, jb can run `orc run TICKET --headless` in a CI/CD pipeline or unattended terminal, capture structured output, and present a professional report to the client.

---

### R-016: Headless mode

Run orc with no interactive prompts. All gates are auto-approved (like `--auto`), no stdin reading, machine-friendly output. For CI/CD, cron jobs, and Ralph wrapping.

**Usage:**
```bash
orc run KS-42 --headless
```

**Behavior:**
- All gates are auto-approved (implies `--auto`)
- No stdin reader (no steering)
- No ANSI color codes in output
- No interactive permission prompts — agent phases use the same `--allowedTools` mechanism as auto mode (not `--dangerously-skip-permissions`). If a tool outside the allowed list is requested, the denial is logged and the phase continues (same as current unattended behavior). A separate `--skip-permissions` flag is available for cases where broad tool access is explicitly desired, with a warning printed to stderr
- Output is clean and parseable
- Exit codes (R-003) are the primary status signal

**Implementation approach:**
- `--headless` flag on `orc run`
- Sets `AutoMode = true` in dispatch environment
- Disables ANSI colors (check `isatty` or respect `--headless` flag)
- Disables stdin reader in agent executor
- All UX output functions check headless flag and emit plain text

**Acceptance criteria:**
- `orc run --headless` runs without any interactive prompts
- Output contains no ANSI escape codes
- Exit codes correctly signal success/failure type
- Agent phases work correctly without stdin
- Gate phases are auto-approved
- Headless mode does NOT use `--dangerously-skip-permissions` by default — uses `--allowedTools` (same as auto mode in `dispatch/agent.go`)

**Priority:** P1
**Effort:** Medium
**Dependencies:** R-003 (exit codes). Benefits from R-017 (if done first, R-016 reuses the color-disabling infrastructure instead of implementing its own).

---

### R-017: `--no-color` output mode

Separate from headless — allows interactive use without ANSI codes. Useful for piping output, logging, and terminals that don't support color.

**Usage:**
```bash
orc run KS-42 --no-color
ORC_NO_COLOR=1 orc run KS-42
NO_COLOR=1 orc run KS-42          # respect standard NO_COLOR env var
```

**Implementation approach:**
- Check `--no-color` flag, `ORC_NO_COLOR` env var, or `NO_COLOR` env var (standard: https://no-color.org/)
- Also check `isatty(stdout)` — if output is piped, disable color by default
- All `ux` functions (which use ANSI constants from `internal/ux/output.go`) emit empty strings for color codes when color is disabled

**Acceptance criteria:**
- `--no-color` produces color-free output
- `NO_COLOR` env var is respected
- Piped output (non-TTY) disables color automatically
- All UX functions respect the color setting

**Priority:** P2
**Effort:** Small
**Dependencies:** None

---

### R-018: Error message quality audit

Review every error message in the codebase for clarity. Error messages should tell the user what happened, why, and what to do about it. Currently, some errors assume familiarity with orc internals.

**Areas to review:**
- Config validation errors: Are they specific enough? Do they include line numbers?
- Runtime errors: Do they suggest fixes? (e.g., "phase 'implement' timed out after 45m — consider increasing timeout in config")
- Resume hints: Are they always shown on failure?
- Agent errors: Are permission denials explained clearly?
- Missing binary errors: Do they suggest installation steps?

**This is an audit task, not a feature.** Go through every `fmt.Errorf` and `ux.PhaseFail` call and improve the messages where needed.

**Acceptance criteria:**
- Every error message includes: what happened, which phase/config element, and a suggested fix or next step
- No raw Go error messages leak to the user — every `fmt.Errorf` in the call chain is wrapped with user-facing context (verified by grep for unwrapped error returns)
- Config validation errors include the phase name (or index) and field name that failed
- A new user can understand and act on every error without reading source code

**Priority:** P2
**Effort:** Medium
**Dependencies:** None

---

### R-019: Phase output artifacts — structured metadata

Currently, agent phase output is saved as raw text to `logs/phase-N.log`. Add structured metadata alongside the raw log.

**Metadata file:** `logs/phase-N.meta.json`
```json
{
  "phase_name": "implement",
  "phase_type": "agent",
  "model": "opus",
  "session_id": "abc-123",
  "start_time": "2026-02-28T14:30:00Z",
  "end_time": "2026-02-28T14:38:22Z",
  "duration_seconds": 502,
  "cost_usd": 1.87,
  "input_tokens": 45000,
  "output_tokens": 22000,
  "exit_code": 0,
  "tools_used": ["Read", "Edit", "Write", "Bash"],
  "tools_denied": [],
  "files_modified": ["src/handler.go", "src/handler_test.go"]
}
```

This metadata enables `orc report` to produce richer reports and gives `orc improve` signal about which phases are expensive, which tools are used, etc.

**Implementation approach:**
- After each phase completes, write the metadata file alongside the log file
- For agent phases: extract session_id, cost, tokens, tools from the stream parser results
- For script phases: capture exit code, duration
- Tool tracking: the stream parser already handles tool names in `content_block_start`/`content_block_stop` events for inline display (via `streamState.toolName`) — add a `ToolsUsed []string` field to `StreamResult` and accumulate tool names there

**Acceptance criteria:**
- Every phase produces a `.meta.json` file in the logs directory
- Metadata includes timing, cost, tokens, tools used, exit code
- `orc report` uses metadata for richer output
- Metadata files are JSON and parseable by external tools

**Priority:** P2
**Effort:** Medium
**Dependencies:** R-004 (cost tracking)

---

## Wave 5: Distribution & Packaging

**Theme:** Make orc installable by people who aren't jb. Binary releases, documentation, and the groundwork for wider adoption.

**Validation checkpoint:** After Wave 5, another engineer at JBD can install orc, follow the getting-started guide, set up a workflow on their project, and run it successfully.

---

### R-020: Binary releases via GoReleaser

Automated binary releases for Linux, macOS (Intel + Apple Silicon), and Windows. Published to GitHub Releases.

**Setup:**
- Add `.goreleaser.yaml` to the repo
- GitHub Action that triggers on tag push (e.g., `v0.1.0`)
- Produces binaries, checksums, and a GitHub Release with changelog

**Binary names:** `orc_linux_amd64`, `orc_darwin_arm64`, etc.

**Versioning:** Semantic versioning. `orc --version` prints the version.

**Implementation approach:**
- Add GoReleaser config
- Add GitHub Action workflow `.github/workflows/release.yml`
- Embed version via `-ldflags` at build time
- Add `--version` flag to CLI

**Acceptance criteria:**
- `git tag v0.1.0 && git push --tags` produces a GitHub Release with binaries
- Binaries work on Linux amd64, macOS arm64, macOS amd64
- `orc --version` prints the version
- README updated with installation from GitHub Releases

**Priority:** P2
**Effort:** Small
**Dependencies:** None

---

### R-021: Homebrew formula

Install orc via Homebrew on macOS and Linux.

```bash
brew install jorge-barreto/tap/orc
```

**Implementation approach:**
- Create a Homebrew tap repo (`homebrew-tap`)
- GoReleaser can auto-generate Homebrew formulae
- Or maintain a manual formula that downloads from GitHub Releases

**Acceptance criteria:**
- `brew install jorge-barreto/tap/orc` installs a working orc binary
- Formula is updated automatically on new releases (or has clear manual update steps)

**Priority:** P3
**Effort:** Small
**Dependencies:** R-020 (binary releases)

---

### R-022: Getting-started documentation

A comprehensive guide for someone who has never used orc. Goes beyond the README.

**Content:**
1. **What is orc?** — The problem it solves, how it compares to alternatives (Ralph Wiggum, manual agent workflows)
2. **Installation** — From binary, from source, via Homebrew
3. **Your first workflow** — Step-by-step: `orc init`, edit config, `orc validate`, `orc run --dry-run`, `orc run`
4. **Workflow patterns** — Common patterns with examples: simple pipeline, retry loop, convergent review, parallel quality checks, worktree isolation
5. **Prompt engineering for orc** — How to write effective agent prompts: self-contained, artifact-aware, retry-aware, with explicit output expectations
6. **Troubleshooting** — Common issues, `orc doctor`, reading artifacts, debugging prompts
7. **Configuration reference** — Full schema with examples (already exists as `orc docs`)

**Format:** Either a docs/ directory in the repo or a simple docs site (GitHub Pages from markdown).

**Acceptance criteria:**
- A new user can go from zero to running a workflow by following the guide
- All workflow patterns are documented with example configs and prompts
- Prompt engineering section includes do's and don'ts from real experience

**Priority:** P2
**Effort:** Medium (content work)
**Dependencies:** R-005 (validate), R-006 (loop), R-012 (recipes)

---

## Wave 6: Provider Abstraction

**Theme:** Decouple orc from Claude CLI. Support alternative AI providers and the Claude Code SDK subprocess protocol. This wave is about architectural changes, not adding providers — the goal is to make orc provider-agnostic in design, then add specific providers as needed.

**Validation checkpoint:** After Wave 6, orc can run agent phases using something other than `claude -p`. The simplest case: a script that reads a prompt from stdin and writes output to stdout.

---

### R-023: Agent provider interface

Extract the Claude-specific agent executor into a provider interface. The runner and dispatcher interact with the interface, not with Claude directly.

**Interface design:**
```go
// AgentProvider runs an AI agent with a prompt and returns the result.
type AgentProvider interface {
    // Run executes the agent with the given prompt and options.
    Run(ctx context.Context, prompt string, opts AgentOpts) (*AgentResult, error)

    // Name returns the provider name for display ("claude", "openai", etc.)
    Name() string
}

type AgentOpts struct {
    Model      string
    Effort     string
    Timeout    time.Duration
    SessionID  string        // for resume/multi-turn
    LogWriter  io.Writer     // where to write raw output
    CWD        string
    Env        []string
    AllowTools []string
}

type AgentResult struct {
    ExitCode     int
    Output       string      // captured stdout
    CostUSD      float64
    InputTokens  int
    OutputTokens int
    SessionID    string
    ToolsUsed    []string
    Denials      []string
}
```

**Providers:**
1. **`ClaudeProvider`** — Current implementation, extracted from `dispatch/agent.go`. Uses `claude -p` with stream-json parsing.
2. **`ScriptProvider`** — Wraps any CLI command. The prompt is passed via stdin (or a temp file), stdout is captured. Exit code is the result. No cost/token tracking.

**Config:**
```yaml
# Top-level default provider
agent-provider: claude    # "claude" (default), "script"

# Per-phase override
- name: implement
  type: agent
  agent-provider: script
  agent-command: my-custom-agent --prompt-file
```

For v1, only `claude` and `script` providers. The `script` provider enables wrapping any CLI tool (aider, Codex CLI, custom scripts) without orc knowing about them.

**Implementation approach:**
- Define `AgentProvider` interface in `dispatch/`
- Extract current Claude logic into `ClaudeProvider` struct
- Create `ScriptProvider` struct for generic CLI wrapping
- `Dispatch()` selects provider based on phase config (or top-level default)
- Provider is constructed once during runner setup, not per-phase

**Acceptance criteria:**
- Agent phases work identically to current behavior with `ClaudeProvider`
- `ScriptProvider` can wrap a simple script (e.g., `echo "response"` reads prompt from stdin)
- Per-phase provider override works
- Tests cover both providers
- No behavioral regression in existing agent functionality

**Priority:** P2
**Effort:** Large (significant refactor)
**Dependencies:** None (but should wait until Waves 0-4 are stable)

---

### R-024: Claude Code SDK subprocess protocol

The TODO.md mentions using the JSON-based subprocess protocol instead of `claude -p`. This gives orc programmatic control over tool approvals, richer status information, and better error handling.

**What changes:**
- Instead of `claude -p <prompt>`, orc spawns `claude` as a subprocess and communicates via JSON over stdin/stdout
- Tool-use requests come as JSON messages; orc can approve/deny programmatically
- Richer metadata (token counts, cost, session info) is available per-message
- Permission handling moves from Claude's interactive prompts to orc's control

**Why this matters:**
- Attended mode becomes much more controllable (orc manages permissions, not the terminal)
- Tool approval policies can be configured in orc's config (not Claude's settings)
- Better error recovery (orc can detect and handle specific tool failures)

**Implementation approach:**
- This would be a new `ClaudeSDKProvider` that implements the `AgentProvider` interface from R-023
- Uses the subprocess protocol documented in Claude Code SDK
- Replaces stream-json parsing with structured JSON message handling
- Requires Claude Code to support the subprocess protocol

**Acceptance criteria:**
- Agent phases work via the subprocess protocol
- Tool approvals are handled by orc (not interactive terminal prompts)
- All metadata (cost, tokens, tools) is captured
- Falls back to `claude -p` if subprocess protocol is not available

**Priority:** P3
**Effort:** Large
**Dependencies:** R-023 (provider interface)

---

### R-025: API key configuration

Move from Claude CLI subscription to API billing. orc needs to manage API keys for itself and for agent invocations.

**Config:**
```yaml
# In .orc/config.yaml or a separate .orc/secrets.yaml (gitignored)
api-key: ${ANTHROPIC_API_KEY}    # env var reference
```

Or more practically: orc passes the API key via environment variable to the Claude CLI, which already supports `ANTHROPIC_API_KEY`.

**What orc needs to do:**
- Document how to set up API billing (env var, config file, or secrets manager)
- Verify `ANTHROPIC_API_KEY` passes through to child processes — this already works because `dispatch.BuildEnv()` only strips `CLAUDECODE*` vars, but needs explicit documentation and a test
- Support `ORC_API_KEY` env var as an alternative. Note: a `--api-key` CLI flag is NOT recommended — API keys passed via CLI flags are visible in process listings (`ps aux`) and shell history, which is a compliance risk for client deployments. Env vars are the primary key mechanism
- Cost tracking (R-004) becomes more important with API billing

**Acceptance criteria:**
- `ANTHROPIC_API_KEY` env var is respected by agent phases
- Documentation explains API key setup
- Cost tracking works correctly with API billing (non-zero cost_usd)

**Priority:** P3
**Effort:** Small
**Dependencies:** None (but R-004 cost tracking becomes critical once API billing is active — implement R-004 first)

---

## Wave 7: Enterprise & Scale

**Theme:** Features for licensing orc as a product. Multi-project management, audit trails, and self-improving workflows. These are the features that justify a maintenance contract.

**Validation checkpoint:** After Wave 7, JBD can license orc to a client with: auditable run history, cost tracking per project, and workflow configs that improve over time based on run data.

---

### R-026: Audit logging

Structured JSON log of every action orc takes. Who ran what, when, with what config, what it touched.

**Log format:** One JSON line per event, appended to `.orc/audit.log` (or a configurable path).

```jsonl
{"ts":"2026-02-28T14:30:00Z","event":"run_start","ticket":"KS-42","config_hash":"abc123","user":"jb"}
{"ts":"2026-02-28T14:30:01Z","event":"phase_start","phase":"setup","type":"script"}
{"ts":"2026-02-28T14:30:13Z","event":"phase_end","phase":"setup","type":"script","exit_code":0,"duration_s":12}
{"ts":"2026-02-28T14:30:13Z","event":"phase_start","phase":"plan","type":"agent","model":"opus"}
{"ts":"2026-02-28T14:34:43Z","event":"phase_end","phase":"plan","type":"agent","exit_code":0,"cost_usd":0.42}
{"ts":"2026-02-28T14:58:00Z","event":"run_end","ticket":"KS-42","status":"completed","total_cost_usd":2.29}
```

**What's logged:**
- Run start/end with ticket, config hash, user (from `$USER` or `$ORC_USER`)
- Phase start/end with type, model, exit code, duration, cost
- Loop events (loop-back, on-fail trigger, with iteration counts)
- Config changes (when `orc improve` modifies the config)
- Errors and warnings

**Implementation approach:**
- New `internal/audit/` package with a simple append-to-file logger
- Runner calls audit logger at key points
- JSON lines format for easy parsing by log aggregation tools (ELK, Datadog, etc.)

**Acceptance criteria:**
- Every run produces audit log entries
- Log format is documented in `orc docs` with a schema definition, and existing field names/types are not changed without a major version bump
- Audit log survives across runs (append, not overwrite)
- Config hash enables tracking which config version produced each run

**Priority:** P3
**Effort:** Medium
**Dependencies:** None (R-004 enriches audit entries with cost data when available, but audit logging works without it)

---

### R-027: Multi-project config management

When JBD manages orc configs for multiple clients, there needs to be a way to maintain shared patterns across projects while keeping project-specific customization.

**Approach: Config imports**
```yaml
name: client-project
import:
  - https://github.com/JBD/orc-recipes/blob/main/standard-quality.yaml
  - .orc/local-overrides.yaml

phases:
  # imported phases from standard-quality.yaml, plus local phases
```

Or simpler: a shared directory of prompt templates and scripts that projects reference.

**This is exploratory.** The right design depends on how many clients JBD onboards and what patterns emerge. For now, the best approach might be:
1. A private GitHub repo of prompt templates and scripts
2. Projects clone/copy from this repo during `orc init`
3. Updates are applied via `orc improve` with instructions referencing the shared repo

**Acceptance criteria:**
- A mechanism exists for sharing workflow patterns across projects (either config imports or a shared template repo referenced by `orc init --recipe`)
- At least 2 JBD projects can use the same base workflow config with project-specific overrides (different ticket patterns, different prompt customizations)
- Updates to shared patterns can be propagated to existing projects via `orc improve` with an instruction referencing the updated shared pattern
- A project using shared patterns can diverge from the base without breaking the shared mechanism

**Priority:** P3
**Effort:** Large (design TBD)
**Dependencies:** R-009 (orc improve for applying updates)

---

### R-028: Self-improving workflows — full `orc improve`

The second-order loop: `orc improve` reads run history (timing, costs, loop counts, failures) across multiple runs and suggests workflow config changes.

**Examples of what it could suggest:**
- "Phase 'implement' averages 35 minutes but has a 30-minute timeout. Consider increasing timeout to 45."
- "The quality→implement loop triggers on 80% of runs. The implement prompt may need clearer instructions about test requirements."
- "Phase 'plan' costs $0.40 per run and could use model sonnet instead of opus (savings estimate: $0.25/run)."
- "The review loop converges in 2 iterations on average. Consider reducing min from 3 to 2."

**This requires:**
- Run history (R-014) with per-phase metrics
- Cost tracking (R-004) with per-phase costs
- Enough run data to identify patterns (at least 5-10 runs)

**Implementation:** Extend `orc improve` with a `--analyze` flag that reads history and generates improvement suggestions:
```bash
orc improve --analyze           # print suggestions based on run history
orc improve --analyze --apply   # apply suggested changes automatically
```

**Acceptance criteria:**
- `orc improve --analyze` reads run history and produces actionable suggestions
- Suggestions are based on statistical patterns, not single data points
- `--apply` mode modifies config with the suggestions (with confirmation prompt)
- At least 3 categories of suggestions: timeout tuning, model selection, loop tuning

**Priority:** P3
**Effort:** Large
**Dependencies:** R-009 (orc improve), R-014 (run history), R-004 (cost tracking)

---

### R-029: Phase output variables — dynamic inter-phase data

Allow phases to set variables that subsequent phases can read, without writing to artifact files.

**Mechanism:** A special output file `$ARTIFACTS_DIR/.env` that orc reads after each phase and injects into subsequent phases' environment.

```bash
# In a script phase:
echo "PR_NUMBER=42" >> "$ORC_OUTPUT"
echo "BRANCH_NAME=feature-xyz" >> "$ORC_OUTPUT"
```

`$ORC_OUTPUT` is a new built-in variable pointing to a file that orc reads after phase completion. Key-value pairs are added to the variable map for subsequent phases.

**Why this matters:**
- Reduces boilerplate for simple inter-phase data (currently requires writing to a file, then reading it with `cat` in the next phase)
- The kitchen-scheduler's `push-pr.sh` writes PR number to a file, then `ci-check` reads it with `cat $ARTIFACTS_DIR/pr.txt`. With output variables: `echo "PR_NUM=42" >> $ORC_OUTPUT`, then ci-check uses `$PR_NUM` directly.

**Acceptance criteria:**
- Phases can write key=value pairs to `$ORC_OUTPUT`
- Subsequent phases have access to these variables via `$KEY` substitution
- Variables persist across the run (saved to artifacts)
- Conflicts with built-in vars are rejected

**Priority:** P3
**Effort:** Medium
**Dependencies:** None

---

## Prompt Engineering Track

This is a parallel workstream — not engine features, but the prompt templates and scripts that make orc workflows effective. These are content, not code.

### P-001: Generic plan prompt template

A plan prompt that works across codebases. Reads ticket details, explores code, writes a self-contained implementation plan.

**Key elements:**
- Read ticket from `$ARTIFACTS_DIR/ticket.txt` (or accept ticket ID via `$TICKET`)
- Explore the codebase: read README, CLAUDE.md, key config files
- Identify files that need changes
- Write plan to `$ARTIFACTS_DIR/plan.md`
- Plan must be self-contained for a separate implement agent
- Include: context, files to modify, implementation approach, test strategy, commit strategy

**Priority:** P1 | **Effort:** Small | **Dependencies:** None

### P-002: Generic implement prompt template

An implement prompt that follows a plan and handles retry loops.

**Key elements:**
- Read `$ARTIFACTS_DIR/plan.md`
- Check `$ARTIFACTS_DIR/feedback/` for retry feedback
- Write code, tests, commits per the plan
- Handle first-run vs. retry scenarios differently
- Verify clean working tree before finishing

**Priority:** P1 | **Effort:** Small | **Dependencies:** P-001

### P-003: Generic review prompt template [Subsumed by R-007]

**Note:** This item is subsumed by R-007 (review prompt templates). P-003's generic review prompt is implemented as R-007's `review-code.md` template. Do not implement P-003 separately — work on R-007 instead.

A review prompt for the convergent review loop.

**Key elements:**
- Read recent changes (git diff or artifact files)
- Evaluate against the plan's acceptance criteria
- Check: logic errors, test coverage, security (OWASP Top 10), code quality
- Classify findings as blocking vs. non-blocking
- Write findings to `$ARTIFACTS_DIR/review-findings.md`
- Exit 0 if no blocking issues, exit 1 if blocking
- Handle both first-run and retry-loop scenarios (check `$ARTIFACTS_DIR/feedback/` for previous findings)

**Priority:** P1 | **Effort:** Small | **Dependencies:** None (benefits from R-006, subsumed by R-007)

### P-004: Generic quality script

A quality-check script that works across project types.

**Detects and runs:**
- If `package.json` exists: `npm test` / `yarn test`
- If `Makefile` exists: `make test`
- If `go.mod` exists: `go test ./...`
- If `pyproject.toml` exists: `pytest`
- Clean tree check (no uncommitted changes)
- Clean commits check (no "claude" in commit messages)

**Priority:** P1 | **Effort:** Small | **Dependencies:** None

### P-005: Setup script template

Generic setup script for worktree-based workflows.

**Steps:**
1. Create git worktree at `$WORKTREE` (idempotent)
2. Fetch ticket details (if ticket system integration is configured)
3. Set up branch naming from ticket ID

**Priority:** P2 | **Effort:** Small | **Dependencies:** None

### P-006: Push-PR script template

Generic script to push and create a PR.

**Steps:**
1. Push branch
2. Create or update PR (idempotent)
3. Save PR number to `$ARTIFACTS_DIR/pr.txt`

**Priority:** P2 | **Effort:** Small | **Dependencies:** None

---

## Dependency Map

```
R-001 (close #1)           → standalone
R-002 (top-level defaults)  → standalone, subsumes Issue #5
R-003 (exit codes)          → standalone, enables Ralph wrapping
R-004 (cost tracking)       → standalone, required by R-031 and R-019; enriches R-008, R-015, R-026, R-028
R-005 (orc validate)        → standalone
R-030 (artifact isolation)  → standalone
R-035 (run summary table)   → standalone

R-006 (loop field)          → standalone (requires feedback injection from commit 1892763; R-001 verifies)
R-007 (review prompts)      → standalone (benefits from R-006)
R-009 (orc improve)         → standalone (design done in PLAN.md)
R-031 (cost limits)         → depends on R-004
R-010 (--from names)        → standalone
R-011 (dry-run flow viz)    → benefits from R-006
R-012 (init recipes)        → depends on R-006, R-007
R-013 (orc test)            → standalone
R-032 (step-through mode)   → standalone
R-033 (orc debug)           → standalone (richer with R-019)

R-008 (orc report)          → standalone (benefits from R-004)
R-014 (run history)         → depends on R-030 (ticket-scoped artifact dir)
R-015 (enhanced status)     → standalone (benefits from R-004, R-006)
R-034 (eval framework)      → standalone (benefits from R-014, R-004)

R-016 (headless mode)       → depends on R-003, benefits from R-017
R-017 (--no-color)          → standalone
R-018 (error audit)         → standalone
R-019 (phase metadata)      → depends on R-004

R-020 (binary releases)     → standalone
R-021 (homebrew)            → depends on R-020
R-022 (getting-started docs)→ depends on R-005, R-006, R-012

R-023 (provider interface)  → standalone (but wait for Waves 0-4)
R-024 (claude SDK protocol) → depends on R-023
R-025 (API key config)      → standalone (R-004 recommended first for cost visibility)

R-026 (audit logging)       → standalone (R-004 enriches with cost data)
R-027 (multi-project)       → depends on R-009
R-028 (self-improving)      → depends on R-009, R-014, R-004
R-029 (output variables)    → standalone

P-001 (plan prompt)         → standalone
P-002 (implement prompt)    → depends on P-001
P-003 (review prompt)       → subsumed by R-007 (benefits from R-006)
P-004 (quality script)      → standalone
P-005 (setup script)        → standalone
P-006 (push-PR script)      → standalone
```

**Critical path for "sprint into next week":**
```
R-001 (feedback) ──┐
R-006 (loop field) ├── independent, both unblock daily use
R-002 (defaults) ──┤
R-003 (exit codes) ┤
R-004 (cost) ──────┘
R-009 (improve) ────── unblocks fast config iteration
R-030 (artifact isolation) ── unblocks concurrent tickets
```

**Critical path for "client deployment":**
```
R-004 (cost) → R-031 (cost limits) ── safety for client billing
R-008 (report) ────┐
R-014 (history) ───├── observability, deploy in parallel
R-030 (isolation) ─┘
R-003 (exit codes) → R-016 (headless)
R-006 (loop) → R-012 (recipes)
R-007 (review prompts) → R-012 (recipes)
```

**Critical path for "prompt quality":**
```
R-013 (orc test) ──┐
R-032 (step-through) ├── iteration tools, all standalone
R-033 (orc debug) ─┘
R-034 (eval framework) ── systematic quality measurement
```

---

## Anti-Roadmap — Things orc Will Not Build

1. **Built-in Jira/Linear/GitHub Issues integration** — Script phases handle this. Every client uses different tools. orc shouldn't know about them.

2. **Git operations (worktree, branch, merge, push)** — Script phases. The kitchen-scheduler proves this pattern works.

3. **Test framework integration** — Script phases detect and run the appropriate test command.

4. **IDE plugins** — orc is a CLI. IDE integration adds maintenance burden without proportional value.

5. **Web dashboard** — `orc report` produces markdown. For v1, that's sufficient. A web UI is a separate product.

6. **DAG execution** — Phases are a linear sequence. `parallel-with` handles pair concurrency. N-way parallelism is handled within script phases (`cmd1 & cmd2 & wait`). A full DAG engine is a different product.

7. **Plugin system for custom phase types** — The three types (script, agent, gate) cover everything. Scripts are infinitely extensible.

8. **Multi-user / authentication** — orc is single-operator. The operator is jb (or a CI bot). Multi-user access control is a future concern.

9. **Model hosting** — orc calls external AI providers. It does not host models.

10. **Prompt versioning / A-B testing** — Interesting but premature. Version control (git) handles prompt versioning. A-B testing requires enough run volume to be statistically meaningful.

---

## Validation Checkpoints

| After | jb can... | Validates |
|-------|-----------|-----------|
| Wave 0 | Run workflows with cleaner configs, see costs, wrap in Ralph loop, run concurrent tickets, see run summary table | Core usability and safety work |
| Wave 1 | Demonstrate adversarial quality assurance with guaranteed minimum review passes, iterate on configs with `orc improve`, cost limits prevent runaway | The product differentiator works safely, config iteration is fast |
| Wave 2 | Set up orc on a new project in 30 minutes, step through phases to debug prompts, run `orc debug` to analyze failures | Iteration speed is sufficient for client onboarding |
| Wave 3 | Show a client a run report, measure prompt quality with `orc eval`, track scores across prompt versions | Stakeholder communication and quality measurement work |
| Wave 4 | Run orc in CI/CD on a client project with headless mode | Unattended deployment works |
| Wave 5 | Hand another engineer a binary and a getting-started guide | The tool is distributable |
| Wave 6 | Run orc with a non-Claude provider | Provider lock-in is eliminated |
| Wave 7 | License orc with audit trails and self-improving configs | The product is enterprise-ready |

---

## Open Questions

1. ~~**Should `loop.min` force re-running the implement phase or just re-running the review phase?**~~ **Resolved in R-006:** v1 uses the full-loop approach (goto→phase). This is simpler to implement, provides the implement agent with review feedback on each pass, and can be revisited later if testing shows review-only passes produce better results.

2. **Should run history be opt-in or default?** Default is better for observability but increases disk usage. A config option `history: false` could disable it for resource-constrained environments.

3. **How should `orc improve --analyze` weight recent runs vs. old runs?** Exponential decay? Fixed window (last N runs)? This affects whether the system oscillates (overreacting to recent data) or is slow to adapt (weighting old data too heavily).

4. **Should the `script` agent provider pass prompts via stdin or temp file?** Stdin is simpler but some tools don't support it. Temp file is more compatible but requires cleanup. Could support both via config.

5. **When should orc switch from subscription to API billing?** When client deployment starts. The subscription is fine for personal use. API billing is needed for: cost tracking per client, per-ticket billing, and running in CI/CD without a logged-in subscription.

6. **Should prompt templates live in the orc repo or a separate repo?** In the orc repo is simpler for distribution (they ship with the binary via `go embed`). A separate repo allows independent versioning and client-specific forks. Start in-repo, extract later if needed.
