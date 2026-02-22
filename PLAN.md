# Plan: orc — Deterministic Agent Orchestrator

## Context

**orc** is a CLI tool that orchestrates AI coding agents through project-defined workflows. Instead of giving an LLM a long prompt and trusting it to self-manage a multi-phase process, orc inverts control: a deterministic runner drives the workflow as a state machine, dispatching each phase either as a direct script call (for linting, testing, environment checks) or as a scoped `claude -p` invocation (for researching a ticket, planning an implementation, writing code). Context passes between phases through artifact files on disk, not conversational memory. The human gets full observability and control without the agent ever seeing or driving the overall workflow.

**Why now:** The current `/ticket` workflow in the Idaho project relies on an LLM agent to self-orchestrate 10 phases from a 300-line monolithic prompt. The agent is responsible for knowing what phase it's in, doing the right work, and calling checkpoints. If it skips steps, misinterprets boundaries, or loses context, guardrails don't fire until after the damage. Deterministic phases burn LLM tokens unnecessarily. Inspired by Stripe's "blueprint" pattern (Minions Part 2), we invert control.

**Two-phase build:** First we build orc as a standalone open-source tool at `~/work/orc/`. Then we create an orc workflow definition for the Idaho project. orc is repo-agnostic — any project provides a `.orc/config.yaml` and phase prompts, and the engine runs them.

---

## Part A: Build the Engine (`~/work/orc/`)

This is a standalone open-source project. The engine handles all orchestration, state management, UX, and agent dispatch. It knows nothing about any specific project.

**Language: Go.** Single static binary, zero runtime dependencies, native YAML parsing (`gopkg.in/yaml.v3`), `os.Expand` replaces `envsubst`, `os/exec` for subprocess management, cross-platform builds with `GOOS`/`GOARCH`.

### Prerequisites

System has Go 1.19 — need **Go 1.22+** for urfave/cli/v3. Install first:
```bash
wget https://go.dev/dl/go1.23.6.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.23.6.linux-amd64.tar.gz
```

### Dependencies

- `gopkg.in/yaml.v3` — YAML config parsing
- `github.com/urfave/cli/v3` — CLI framework (subcommands, flags, args)
- No other external deps. Everything else is Go stdlib.

### Architecture

```
~/work/orc/
├── cmd/orc/main.go              <- CLI entrypoint (urfave/cli/v3 wiring)
├── internal/
│   ├── config/
│   │   ├── config.go            <- Config + Phase structs, YAML loading, PhaseIndex()
│   │   └── validate.go          <- All validation rules, sets defaults
│   ├── state/
│   │   ├── state.go             <- State read/write/advance (JSON persistence)
│   │   ├── timing.go            <- Per-phase start/end/duration
│   │   └── artifacts.go         <- Dir management, loop counts, feedback, output checks
│   ├── dispatch/
│   │   ├── dispatch.go          <- Router + Environment struct
│   │   ├── script.go            <- Script phase: exec.CommandContext, io.MultiWriter, BuildEnv()
│   │   ├── agent.go             <- Agent phase: prompt render, claude -p, streaming
│   │   ├── gate.go              <- Gate phase: human y/n/e prompt, auto-skip
│   │   └── expand.go            <- ExpandVars() using os.Expand with custom map
│   ├── runner/
│   │   └── runner.go            <- Main loop, on-fail jumps, parallel, output validation
│   └── ux/
│       ├── output.go            <- Phase headers, timestamps, ANSI colors
│       └── status.go            <- Status command rendering
├── go.mod                        <- github.com/jorge-barreto/orc, go 1.22
├── go.sum
├── Makefile
├── README.md
└── LICENSE                       <- MIT
```

### CLI Interface

```bash
orc run <ticket> [options]       # run workflow
orc status <ticket>              # show progress
orc run <ticket> --auto          # skip human gates
orc run <ticket> --retry <N>     # redo phase N (resets state to N-1)
orc run <ticket> --from <N>      # start from phase N (skip prior phases)
orc run <ticket> --dry-run       # print phase plan without executing
```

### Config Format (`.orc/config.yaml`)

The engine reads this from the project root. Each project defines its own.

```yaml
name: my-project
ticket-pattern: "PROJ-\\d+"
main-branch: main

phases:
  - name: context-gate
    type: agent
    description: "Research ticket, Confluence, and beads for domain context"
    prompt: .orc/phases/context-gate.md
    model: sonnet
    timeout: 10
    outputs: [meta.json, context.md]

  - name: worktree-setup
    type: script
    description: "Create isolated git worktree for this ticket"
    run: scripts/worktree create $TICKET $BRANCH_TYPE $SLUG

  - name: env-verification
    type: script
    description: "Verify Docker, PostgreSQL, and migrations"
    run: scripts/verify-env

  - name: plan
    type: agent
    description: "Explore codebase and write implementation plan"
    prompt: .orc/phases/plan.md
    model: opus
    timeout: 15
    outputs: [plan.md]

  - name: plan-review
    type: gate
    description: "Human reviews and approves the plan"
    skip-with: --auto

  - name: implement
    type: agent
    description: "Implement the plan one bead at a time"
    prompt: .orc/phases/implement.md
    model: opus
    timeout: 45
    outputs: [implement-summary.md]

  - name: quality-gate
    type: script
    description: "Run formatting, compilation, lint, and tests"
    run: scripts/verify
    on-fail:
      goto: implement
      max: 3

  - name: self-review
    type: agent
    description: "Review implementation against checklist"
    prompt: .orc/phases/self-review.md
    model: opus
    timeout: 20
    outputs: [review-synthesis.md]
    on-fail:
      goto: implement
      max: 2

  - name: done
    type: script
    description: "Print summary report and next steps"
    run: scripts/report
```

### Phase Schema

**Phase types:**
- **`script`** — runner executes the command directly. Zero LLM tokens.
- **`agent`** — runner renders the prompt file (variable substitution), invokes `claude -p`, streams output.
- **`gate`** — runner pauses for human input (approve/reject/edit). Skippable with `--auto` or the flag specified in `skip-with`.

**Required fields:**
- **`name`** — unique identifier, used in logs, status, and `on-fail.goto` references
- **`type`** — `script`, `agent`, or `gate`

**Optional fields:**
- **`description`** — human-readable text shown in `--status`, `--dry-run`, and phase headers
- **`condition`** — shell command. If exits non-zero, phase auto-passes (e.g., skip migration gate if no migrations)
- **`parallel-with`** — name of another phase to run concurrently
- **`model`** — model override for agent phases (default: `opus`). Allows cheaper models for lightweight phases
- **`timeout`** — max minutes for the phase. Default: 30 for agent phases, 10 for script phases. Engine kills the process and reports failure on timeout
- **`outputs`** — list of artifact filenames the phase must produce (relative to `.artifacts/`). After a phase completes, the engine checks each file exists. If any are missing and the phase is an agent type, the engine re-invokes the agent with: "You did not produce the expected artifact at `<path>`. Please produce it now." One retry, then fail. This catches silent agent failures at the handoff moment
- **`on-fail`** — backward transition on phase failure:
  - **`goto`** — name of the phase to jump back to
  - **`max`** — maximum number of backward jumps from this phase (default: 2). After max, engine stops for human
  - On backward jump, engine writes the failing phase's error output to `.artifacts/feedback/from-<phase-name>.md`. The target phase's agent prompt should include: "If files exist in `$ARTIFACTS_DIR/feedback/`, read them — they contain errors from downstream phases that need fixing"
- **`skip-with`** — for gate phases, the CLI flag that skips this gate (e.g., `--auto`)

### State Management (owned entirely by the engine)

All state lives under `.artifacts/` in the working directory:

```
.artifacts/
├── state                      <- current phase index (integer)
├── ticket                     <- ticket identifier
├── meta.json                  <- phase outputs (written by agents)
├── timing.json                <- per-phase start/end/duration
├── prompts/
│   └── phase-N.md             <- rendered prompt sent to each agent
└── logs/
    └── phase-N.log            <- full output from each phase
```

The engine owns all reads/writes to this directory. No external script manages state. The `.state` file at worktree root (used by the old `ticket-checkpoint` and `ticket-identity`) is replaced by `.artifacts/state`.

### Config Validation

On startup, the engine validates the config:
- Required fields present (`name`, `phases`)
- Each phase has `name` and `type`
- Agent phases have `prompt` pointing to an existing file
- Script phases have `run`
- `parallel-with` references an existing phase name
- `on-fail.goto` references an existing phase name that appears *before* the current phase (no forward jumps)
- No duplicate phase names
- Gate phases have valid `skip-with` flags
- `model` is a recognized value (`opus`, `sonnet`, `haiku`)
- `timeout` is a positive integer
- `outputs` entries are valid filenames (no path separators — relative to `.artifacts/`)

Prints clear errors and exits if invalid.

### UX Layer

All of this is v1 — not optional.

**Streaming output + logs.** Every `claude -p` invocation streams to terminal in real-time via `io.MultiWriter(os.Stdout, logFile)` to `.artifacts/logs/phase-N.log`. Deterministic phase stdout/stderr also captured.

**Phase headers with timestamps:**
```
[14:23:07] ══════════════════════════════════════
[14:23:07]  Phase 4/10: plan (agent)
[14:23:07] ══════════════════════════════════════
```
On completion:
```
[14:27:38]  Phase 4 complete (4m 31s)
```

**Rendered prompts saved.** Before each agent invocation, save the prompt (after variable substitution) to `.artifacts/prompts/phase-N.md`.

**`orc status <ticket>`:**
```
Ticket:  ISLMS-873
State:   5/10 (implement) — in progress

Completed:
  1  context-gate       done  (2m 14s)
  2  worktree-setup     done  (0m 08s)
  3  env-verification   done  (0m 12s)
  4  plan               done  (4m 31s)
  -  plan-review        done  (approved)

Artifacts:
  .artifacts/meta.json
  .artifacts/context.md
  .artifacts/plan.md
  .artifacts/prompts/phase-1.md .. phase-4.md
  .artifacts/logs/phase-1.log .. phase-4.log
```

**`--retry N` and `--from N`.** `--retry N` sets state to N-1, re-runs from phase N. `--from N` does the same (alias with clearer intent for skipping).

**`--dry-run`.** Prints full phase sequence with types, commands/prompt paths, conditions evaluated. For agent phases, prints the rendered prompt. Executes nothing.

**Ctrl+C handling.** `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`. Kill child `claude` process via `cmd.Cancel`. Print resume command. Do NOT advance state.

**Timing.** `.artifacts/timing.json` records start/end per phase. Used by `status` command.

### Agent Invocation

```go
// internal/dispatch/agent.go — core of RunAgent()
func RunAgent(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
    // Apply timeout
    if phase.Timeout > 0 {
        ctx, cancel = context.WithTimeout(ctx, time.Duration(phase.Timeout)*time.Minute)
        defer cancel()
    }

    // Read + render prompt template
    promptData, _ := os.ReadFile(filepath.Join(env.ProjectRoot, phase.Prompt))
    rendered := ExpandVars(string(promptData), env.Vars())  // os.Expand with custom map

    // Save rendered prompt for inspection
    os.WriteFile(state.PromptPath(env.ArtifactsDir, env.PhaseIndex), []byte(rendered), 0644)

    // Invoke claude non-interactively, stream to terminal + log
    cmd := exec.CommandContext(ctx, "claude", "-p", rendered, "--model", phase.Model,
        "--dangerously-skip-permissions")
    cmd.Dir = env.WorkDir
    cmd.Env = BuildEnv(env)  // inherits env, adds ORC vars, strips CLAUDECODE

    logFile, _ := os.Create(state.LogPath(env.ArtifactsDir, env.PhaseIndex))
    var captured bytes.Buffer
    cmd.Stdout = io.MultiWriter(os.Stdout, logFile, &captured)
    cmd.Stderr = io.MultiWriter(os.Stderr, logFile, &captured)

    err := cmd.Run()
    return &Result{ExitCode: exitCode(err), Output: captured.String()}, err
}
```

- No `--max-budget-usd` (subscription plan, not API billing)
- MCP tools inherited from project's `.claude/settings.local.json`
- Must NOT be invoked from inside Claude Code (`CLAUDECODE` env var check at startup)
- `BuildEnv()` strips `CLAUDECODE` from child process env so `claude -p` can run

### Variable Substitution (Go-specific)

Uses `os.Expand()` with a custom lookup function instead of `envsubst`:

```go
// internal/dispatch/expand.go
func ExpandVars(template string, vars map[string]string) string {
    return os.Expand(template, func(key string) string {
        if v, ok := vars[key]; ok { return v }
        return os.Getenv(key)
    })
}
```

This handles `$TICKET`, `${ARTIFACTS_DIR}`, etc. without mutating the process environment. Safe for parallel phases.

### Variables Available to Phase Prompts

These are substituted by the engine before passing to `claude -p`:

| Variable | Value |
|----------|-------|
| `$TICKET` | The ticket identifier (e.g., `ISLMS-123`) |
| `$ARTIFACTS_DIR` | Absolute path to `.artifacts/` |
| `$WORK_DIR` | Current working directory (may change after worktree phase) |
| `$PROJECT_ROOT` | The repo root where `.orc/config.yaml` lives |

Phase prompts use these to reference artifact paths. The engine handles `WORK_DIR` switching after worktree-creating phases.

### Main Loop (Resumable)

```
state = read .artifacts/state (default 0)
loop_counts = read .artifacts/loop-counts.json (default {})
phases = load from .orc/config.yaml

for i, phase in phases:
  if i < state: skip (already done)

  print phase header (name, type, index, description)
  record start time

  if phase.condition exists and eval fails: auto-pass, advance, continue

  if phase.parallel-with:
    run phase + parallel partner concurrently (goroutines + sync.WaitGroup)
    advance state past both
    continue

  dispatch by phase.type:
    script -> exec.CommandContext("bash", "-c", phase.run) with timeout + streaming
    agent  -> render prompt, save to .artifacts/prompts/, invoke claude -p (with timeout + model)
    gate   -> print content, prompt human y/n/e (skip if --auto or skip-with flag)

  if exit code != 0 and phase.on-fail:
    count = loop_counts[phase.name] + 1
    if count > phase.on-fail.max:
      print "Phase failed after {max} retry loops. Manual intervention needed."
      exit 1
    loop_counts[phase.name] = count
    save loop_counts
    write error output -> .artifacts/feedback/from-{phase.name}.md
    print "Phase failed. Looping back to {phase.on-fail.goto} (attempt {count}/{max})"
    state = index of phase.on-fail.goto
    write state
    restart loop from new state (continue outer loop)

  if exit code != 0 (no on-fail):
    print error + "Resume: orc run <ticket>"
    exit 1

  if phase.outputs:
    for artifact in phase.outputs:
      if not exists .artifacts/{artifact}:
        if phase.type == agent:
          re-invoke agent with: "You did not produce {artifact} at {path}. Produce it now."
          if still missing after retry: fail phase
        else:
          fail phase with "Expected output {artifact} not found"

  record end time + duration
  advance state
  print completion with duration
```

### Failure Model

**Without `on-fail`:** Phase fails -> engine prints error + resume command -> exits. Human intervenes manually.

**With `on-fail`:** Phase fails -> engine writes error output to `.artifacts/feedback/from-<phase-name>.md` -> resets state to `goto` target -> resumes from there. The target phase's agent sees the feedback and addresses the issues. Loop counter tracked in `.artifacts/loop-counts.json` prevents infinite cycling.

**Output validation:** After a phase completes successfully (exit 0), engine checks declared `outputs` exist in `.artifacts/`. If missing and it's an agent phase, engine re-invokes the agent once with a targeted "produce this file" message. If still missing, phase fails (and `on-fail` kicks in if configured).

**Timeout:** `context.WithTimeout` wrapping the signal context. `exec.CommandContext` kills the process on timeout. Treated as failure (exit code 124).

### Go-Specific Design Decisions

1. **Parallel phases:** Goroutines + `sync.WaitGroup`. Each goroutine gets a copy of `Environment` (different `PhaseIndex`). Cancellable context: if either fails, the other is cancelled.

2. **SIGINT:** `signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)`. When context cancels, `exec.CommandContext` kills the subprocess. Runner detects `ctx.Err()` and prints resume hint.

3. **On-fail loop counting:** Keyed by failing phase name (not goto target). Each failure path has independent retry budget.

4. **Parallel-with:** Must be bidirectional and adjacent in config. Runner detects the first of the pair and launches both.

5. **Script execution:** `exec.CommandContext(ctx, "bash", "-c", command)` — shell features (pipes, redirects) work.

6. **State persistence:** JSON files (not plain integers). `state.json` has phase index + ticket + status. Enables richer resume semantics.

---

## Part B: Idaho Project Workflow (in `idaho-surplus-line-suite`)

After orc is built and working, create the workflow definition for Idaho.

### New Files

**`.orc/config.yaml`** (~50 lines)

Idaho's workflow — uses the full config syntax including `on-fail` loops, `outputs` validation, per-phase `model` selection, and `timeout`:

| Phase | Name | Type | Key config |
|-------|------|------|------------|
| 1 | context-gate | agent | model: sonnet, outputs: [meta.json, context.md] |
| 2 | worktree-setup | script | `scripts/worktree create $TICKET $BRANCH_TYPE $SLUG` |
| 3 | env-verification | script | `scripts/verify-env` |
| 4 | plan | agent | model: opus, outputs: [plan.md] |
| - | plan-review | gate | skip-with: --auto |
| 5 | implement | agent | model: opus, timeout: 45, outputs: [implement-summary.md] |
| 6 | migration-gate | script | condition: migrations in diff, on-fail -> implement (max 2) |
| 7 | quality-gate | script | `scripts/verify`, on-fail -> implement (max 3) |
| 8 | visual-verify | agent | parallel-with: self-review, condition: frontend changes |
| 9 | self-review | agent | outputs: [review-synthesis.md], on-fail -> implement (max 2) |
| 10 | done | script | `scripts/orc-report` |

The `on-fail` loops create this graph:
```
implement -> migration-gate -> quality-gate -> visual-verify -+
    ^              |               |          self-review -----+
    +--------------+               |              |            |
    +------------------------------+              |            |
    +---------------------------------------------+            |
                                                               v
                                                             done
```

**`.orc/phases/context-gate.md`** (~80 lines) — Fetch Jira ticket via MCP, search Confluence DGN space, search beads, create epic bead, classify ticket, determine branch type/slug. Produces `meta.json` + `context.md`.

**`.orc/phases/plan.md`** (~100 lines) — Read context artifacts, explore codebase read-only, create child beads with dependencies, write `plan.md`. No code edits.

**`.orc/phases/implement.md`** (~100 lines) — Read plan, work through beads in order (one commit per bead), capture before screenshots if frontend. Produces `implement-summary.md`.

**`.orc/phases/visual-verify.md`** (~60 lines) — Start servers, take after screenshots, compare with before, write `visual-comparison.md`.

**`.orc/phases/self-review.md`** (~80 lines) — Embedded review checklist (correctness, security, naming, tests, Idaho-specific patterns). Identifies blocking issues and writes `review-synthesis.md`. Does NOT fix code — if blocking issues are found, exits non-zero so `on-fail` loops back to implement with the findings as feedback context.

**`scripts/verify-env`** (~40 lines) — Docker running, PostgreSQL reachable, migrations applied. Extracted from current checkpoint phase 3.

**`scripts/orc-report`** (~60 lines) — Read meta.json + git log + artifacts, print summary + next steps for human.

### Modified Files

**`scripts/ticket-checkpoint`** — Remove `do_stop_hook()` function and the `*` case in the main dispatch. Keep `check_phase_*` functions and `do_advance` for manual debugging only. This is now a legacy tool — orc owns state management entirely.

**`.claude/settings.local.json`** — Remove the Stop hook block. orc expects `claude -p` to exit normally. Keep SessionStart, PreCompact, UserPromptSubmit hooks.

**`.claude/commands/ticket.md`** — Replace 300-line monolith with ~15-line redirect to `orc run $ARGUMENTS`.

**`scripts/ticket-identity`** — Update to read `.artifacts/state` instead of `.state` (or keep backward-compatible with both). This hook is still useful for interactive Claude Code sessions.

---

## Context Flow (Idaho Blueprint)

```
Phase 1 (Agent) -> .artifacts/meta.json, .artifacts/context.md
                     |
Phase 2 (Script) -> .worktrees/ISLMS-XXX/ (moves .artifacts/ into worktree)
                     |
Phase 4 (Agent) <- reads context.md, meta.json
                -> .artifacts/plan.md, child beads
                     |
         +--------------------------------------------------------------+
         |                                                              |
Phase 5 (Agent) <- reads plan.md, context.md + feedback/ if present    |
                -> git commits, screenshots/before/, implement-summary.md
                     |                                                  |
Phase 6 (Script)    migration check ---- on-fail ---------------------->
                     |                                                  |
Phase 7 (Script)    quality gate -------- on-fail --------------------->
                     |                                                  |
         +-----------+-----------+                                      |
Phase 8 (Agent)    Phase 9 (Agent)                                      |
visual-verify      self-review -------- on-fail ----------------------->
         |              |
Phase 10 (Script) <- reads all artifacts, prints summary
```

---

## Artifact Directory Structure

```
.artifacts/
├── state                      <- current phase index (engine-managed)
├── ticket                     <- ticket ID (engine-managed)
├── timing.json                <- engine: per-phase start/end/duration
├── loop-counts.json           <- engine: backward jump counts per phase
├── meta.json                  <- phase 1 output: ticket metadata, branch type
├── context.md                 <- phase 1 output: Jira, Confluence, beads research
├── plan.md                    <- phase 4 output: implementation plan with bead list
├── implement-summary.md       <- phase 5 output: commits, decisions, deviations
├── visual-comparison.md       <- phase 8 output: before/after comparison
├── review-synthesis.md        <- phase 9 output: review findings and fixes
├── prompts/
│   ├── phase-1.md             <- rendered prompt sent to agent
│   ├── phase-4.md
│   ├── phase-5.md
│   ├── phase-8.md
│   └── phase-9.md
├── logs/
│   ├── phase-1.log            <- full phase output
│   ├── phase-2.log
│   └── ...
└── feedback/
    ├── from-quality-gate.md   <- error output when quality-gate fails -> implement loops back
    └── from-self-review.md    <- review findings when self-review fails -> implement loops back
```

---

## Verification

### Part A (Engine)

1. **Config validation:** Write configs with intentional errors (missing fields, bad `on-fail.goto` references, unknown models), verify `orc run` catches each
2. **Dry run:** Write a valid config, run `orc run TEST-1 --dry-run`, verify it prints phase sequence with descriptions, models, timeouts
3. **Deterministic dispatch:** Config with script-only phases, verify they execute and state advances
4. **Agent dispatch:** Config with one agent phase, verify prompt rendering + `claude -p` invocation + log capture + rendered prompt saved
5. **Gate dispatch:** Config with a gate phase, verify it prompts and `--auto` skips
6. **Resume:** Run, kill mid-phase, re-run, verify it resumes from failed phase
7. **Retry:** Complete to phase 3, run `--retry 2`, verify state resets and phase 2 re-runs
8. **Status:** Set up partial state with timing data, run `orc status`, verify output shows durations and artifacts
9. **Ctrl+C:** Run with a slow phase, Ctrl+C, verify clean stop + resume message
10. **Parallel phases:** Config with `parallel-with`, verify both run concurrently
11. **on-fail loop:** Config where a script phase fails, verify state resets to `goto` target, feedback file written, loop count tracked
12. **on-fail max:** Same config, fail repeatedly, verify engine stops after `max` loops
13. **Output validation:** Agent phase with `outputs: [foo.md]`, agent doesn't produce it, verify engine re-prompts once then fails
14. **Timeout:** Agent phase with `timeout: 1` (1 minute), run a slow prompt, verify engine kills it

### Part B (Idaho Blueprint)

1. **Phase 1 isolation:** Run `orc run ISLMS-TEST --dry-run`, verify rendered context-gate prompt looks correct
2. **End-to-end:** Run `orc run ISLMS-XXX` on a real ticket through all phases
3. **Stop hook removal:** Run `claude -p "echo test"` from a worktree, verify clean exit

---

## Implementation Order

### Part A — Engine (`~/work/orc/`)
1. Install Go 1.23, scaffold repo: `go mod init`, `cmd/orc/`, `internal/` structure, README, LICENSE, Makefile
2. `internal/config/` — Config + Phase structs, YAML loading, validation (all field validations including on-fail, outputs, model, timeout)
3. `internal/state/` — state management (read/write/advance JSON, timing, artifacts dir, loop counts, feedback files)
4. `internal/ux/` — phase headers with descriptions, timestamps, ANSI colors, status command formatting
5. `internal/dispatch/` — expand.go (ExpandVars), script.go (exec.CommandContext + io.MultiWriter), agent.go (prompt render + claude -p), gate.go (human prompt + auto-skip), dispatch.go (router)
6. `internal/runner/` — main loop (state machine, on-fail backward jumps, parallel via goroutines, condition evaluation, output validation + retry)
7. `cmd/orc/main.go` — CLI entrypoint (urfave/cli/v3 wiring, project root discovery, arg parsing, subcommands: run/status, flags: --auto/--retry/--from/--dry-run, CLAUDECODE guard, signal.NotifyContext for SIGINT)
8. `go build`, test with a minimal dummy config (script + agent + gate + on-fail loop)

### Part B — Idaho Blueprint (`idaho-surplus-line-suite`)
9. `.orc/config.yaml` — Idaho phase graph
10. `.orc/phases/*.md` — all 5 phase prompt files
11. `scripts/verify-env` + `scripts/orc-report` — deterministic phase scripts
12. Modify `scripts/ticket-checkpoint` — remove stop-hook
13. Modify `.claude/settings.local.json` — remove Stop hook
14. Modify `scripts/ticket-identity` — read from `.artifacts/state`
15. Replace `.claude/commands/ticket.md` — thin redirect
16. End-to-end test on a real ticket

---

## Future (v2+)

- **`go install`/binary releases:** `go install github.com/jorge-barreto/orc/cmd/orc@latest` — installable globally
- **`orc init`:** Scaffold a new project's `.orc/` directory with starter config + example phases
- **Incremental feedback in implementation:** Engine runs compile checks between bead commits, feeds errors back to agent
- **Parallel expert reviews:** Engine orchestrates 8 separate `claude -p` calls for self-review phase
- **Token usage tracking** per phase
- **`orc logs <ticket> <phase>`** — quick access to phase logs
- **`orc artifacts <ticket>`** — list/inspect artifacts
- **`orc diff <ticket>`** — show what the agents changed
