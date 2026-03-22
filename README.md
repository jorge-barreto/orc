# orc

Deterministic agent orchestrator CLI — run AI workflows as a state machine, not a conversation.

![Go 1.22+](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go) ![License: MIT](https://img.shields.io/badge/License-MIT-yellow)

## What is orc?

LLM agents that self-manage multi-phase workflows are unreliable. They lose context, skip steps, and hallucinate progress. **orc** takes a different approach: a deterministic state machine drives the workflow, and context passes through artifact files on disk — not conversational memory.

You define your workflow as a series of **phases** in a YAML config file. Each phase is either a shell script, an AI agent invocation, or a human approval gate. orc runs them in order, persists state after each phase, and can resume from where it left off if interrupted.

## Features

### Workflow Engine
- **Three phase types**: `script` (shell commands), `agent` (Claude AI via `claude -p`), `gate` (human approval with feedback)
- **Convergent loops**: Phases can loop back with `loop` for retry-on-failure and min-iteration enforcement, with optional `on-exhaust` recovery
- **Parallel execution**: Run two phases concurrently with `parallel-with`
- **Conditional phases**: Skip phases based on a shell command exit code
- **Pre-run / post-run hooks**: Shell commands that bracket phase dispatch — start services before, clean up after
- **Output validation**: Declare expected output files; agents are re-prompted once if outputs are missing
- **Multi-workflow support**: Define multiple named workflows (bugfix, refactor, etc.) under `.orc/workflows/` with isolated artifacts per workflow

### Configuration
- **Variable substitution**: `$TICKET`, `$ARTIFACTS_DIR`, `$WORK_DIR`, `$PROJECT_ROOT`, `$WORKFLOW` expanded in prompts and commands
- **Custom variables**: Define project-specific variables under `vars:` that reference built-ins and each other
- **Per-phase model and timeout**: Choose `opus`, `sonnet`, or `haiku` per agent phase
- **Per-phase working directory**: Set `cwd` on script/agent phases for worktree workflows
- **Cost budgets**: Set per-run and per-phase spending limits with `max-cost`
- **Tool approval**: Configure which tools agents can use with `default-allow-tools` and `allow-tools`
- **MCP server support**: Connect agent phases to MCP servers with `mcp-config`

### Execution Modes
- **Attended mode**: Steer agent phases interactively — provide follow-up instructions, approve denied tools, answer agent questions
- **Unattended mode**: `--auto` skips gates and disables all interactive prompts
- **Step-through mode**: `--step` pauses after each phase — continue, rewind to a previous phase, inspect artifacts, or abort
- **Dry-run mode**: Preview the phase plan without executing anything
- **Resume from interruption**: State is saved after every phase — Ctrl+C and resume with `--resume` to continue the agent session

### Developer Tools
- **`orc test`**: Run a single phase in isolation for rapid prompt iteration — no need to execute the full workflow
- **`orc debug`**: Analyze a phase execution — rendered prompt, tool call sequence, cost/token data, and exit status
- **`orc doctor`**: AI-powered diagnostics for failed runs — gathers logs, timing, and feedback, then recommends next steps
- **`orc improve`**: AI-assisted workflow refinement — one-shot or interactive editing of config and prompts
- **`orc eval`**: Run eval cases against known scenarios to measure workflow quality, cost, and time — compare before and after workflow changes
- **`orc flow`**: Visualize the workflow as a rich flow diagram with loop regions, model badges, and hook annotations
- **Prompt recipes**: `orc init --recipe` scaffolds from proven workflow patterns (simple, standard, full-pipeline, review-loop)

### Observability
- **Full audit trail**: Rendered prompts, agent logs, cost/token data, timing, and state all saved to `.orc/artifacts/`
- **`orc report`**: Generate a run summary with timing, costs, phase outcomes, loop activity, and artifact listing — markdown or JSON
- **`orc stats`**: Aggregate metrics across runs — success rate, cost/duration distributions, per-phase breakdown, failure categories, and weekly trends
- **`orc eval`**: Measure workflow quality, cost, and time across eval cases pinned to known git refs — track score trends across config changes
- **Structured exit codes**: 0 (success), 1 (retryable), 2 (human needed), 3 (config error), 130 (interrupted)

## Prerequisites

- **Go 1.22+** — for building from source
- **Claude CLI** — install from [claude.ai/code](https://claude.ai/code). orc invokes `claude -p` for agent phases.

## Installation

### From source

```bash
go install github.com/jorge-barreto/orc/cmd/orc@latest
```

### Clone and build

```bash
git clone https://github.com/jorge-barreto/orc.git
cd orc
make build    # produces ./orc binary
make install  # installs to $GOPATH/bin
```

## Quick Start

```bash
# 1. Initialize a new project
cd your-project
orc init                                         # auto-detect
orc init "data pipeline with validation loop"    # or guide with a description
orc init --recipe standard                       # or start from a built-in recipe

# 2. Edit .orc/config.yaml to define your workflow

# 3. Preview the plan
orc run EXAMPLE-1 --dry-run

# 4. Run for real
orc run EXAMPLE-1
```

## CLI Reference

### `orc init`

Analyzes your project and generates a tailored workflow config using AI. Falls back to a default template if AI generation fails.

```bash
orc init                                    # auto-detect project and generate workflow
orc init "microservice with integration tests"  # guide generation with a description
```

Optionally pass a natural-language description to guide the generated workflow toward a specific shape. The description supplements the auto-detected project context.

Creates `.orc/config.yaml` and one or more `.orc/phases/*.md` prompt templates named after your workflow phases (e.g., `plan.md`, `implement.md`). Also creates `.orc/.gitignore` to exclude the artifacts directory.

Alternatively, scaffold from a built-in recipe for a proven workflow pattern:

```bash
orc init --recipe simple                         # minimal plan → implement → review
orc init --recipe standard                       # quality-assured pipeline with test loop
orc init --recipe full-pipeline                  # AI self-review + human gate
orc init --recipe review-loop                    # convergent AI review loop
orc init --list-recipes                          # show all available recipes
orc init --add-workflow bugfix                   # add a named workflow
orc init --add-workflow bugfix --recipe simple   # add from a recipe
```

Recipes are deterministic — no AI generation required. The description argument is ignored when `--recipe` is set.

### `orc run <ticket>`

Runs the workflow for the given ticket identifier.

```bash
orc run PROJ-123
orc run PROJ-123 --auto        # unattended — skip gates, no steering
orc run PROJ-123 --dry-run     # preview without executing
orc run PROJ-123 --retry 3           # retry from phase 3
orc run PROJ-123 --from implement    # start from the "implement" phase
orc run PROJ-123 --from 2            # still works with numbers
orc run PROJ-123 --verbose     # save raw stream-json output
orc run PROJ-123 --resume      # resume interrupted agent session
orc run PROJ-123 --step        # step through phases interactively
orc run bugfix PROJ-123         # named workflow (positional)
orc run -w bugfix PROJ-123      # named workflow (explicit flag)
```

| Flag | Description |
|------|-------------|
| `--auto` | Unattended mode — skip all gates, no interactive steering |
| `--dry-run` | Print the phase plan without executing |
| `--retry <phase>` | Retry from phase (number or name), resets loop counts |
| `--from <phase>` | Start from phase (number or name), resets loop counts |
| `--verbose`, `-v` | Save raw stream-json output to `.stream.jsonl` files in the logs directory |
| `--resume` | Resume an interrupted agent phase using saved Claude session ID |
| `--step` | Step-through mode — pause after each phase for inspection |
| `--workflow`, `-w` | Select a named workflow from `.orc/workflows/` |

`--retry`, `--from`, and `--resume` are mutually exclusive.

**Attended vs auto mode**: By default, orc runs in attended mode — you can type follow-up instructions to steer agent phases, if an agent attempts a tool that wasn't pre-approved, orc prompts you to approve it, and if the agent asks a question (via AskUserQuestion), orc displays it and collects your answer. With `--auto`, orc runs fully unattended with no stdin interaction.

**Step-through mode**: `--step` pauses after each phase with an interactive prompt. You can continue, rewind to a specific phase, abort, or inspect artifact files. Incompatible with `--auto`.

### `orc flow`

Visualizes the workflow config as a rich flow diagram with bracket-loop regions, phase icons, model badges, and color.

```bash
orc flow                  # colored flow diagram
orc flow --no-color       # without ANSI colors
```

| Flag | Description |
|------|-------------|
| `--no-color` | Disable colored output |

### `orc status [ticket]`

Shows workflow progress. With a ticket argument, shows detailed phase-by-phase execution trace with timing, costs, token counts, and artifacts listing. Without an argument, lists all tickets with their status and cost.

```bash
orc status               # list all tickets
orc status PROJ-123      # detailed view for one ticket
```

### `orc report [ticket]`

Generate a readable summary of a completed, failed, or interrupted run.

```bash
orc report                    # report for most recent ticket
orc report PROJ-123           # report for a specific ticket
orc report --json             # structured JSON output for tooling
orc report -w bugfix PROJ-123 # report for a named workflow
```

Shows status, duration, cost, per-phase results, loop activity, and artifact listing.
Missing data (no costs.json, no timing.json) shows "—" placeholders.
Use `--json` for a stable, versioned JSON schema suitable for CI pipelines and dashboards.

### `orc docs [topic]`

Shows built-in documentation. With no argument, lists available topics. With a topic name, prints the full article.

```bash
orc docs               # list topics
orc docs config        # config file reference
orc docs variables     # template variables and custom vars
orc docs phases        # phase type details
```

Topics: `quickstart`, `config`, `phases`, `variables`, `runner`, `artifacts`, `quality-loops`, `workflows`, `eval`.

### `orc improve [instruction]`

AI-assisted workflow refinement. Two modes:

```bash
orc improve "add a lint phase parallel with tests"    # one-shot
orc improve                                            # interactive
```

**One-shot mode**: Reads your current config and prompt files, sends them to Claude with your instruction, validates the output, and writes changed files.

**Interactive mode**: Launches Claude in interactive mode with your workflow context pre-loaded for a conversational editing experience.

### `orc validate`

Validates `.orc/config.yaml` without running anything. Useful for checking config before committing.

```bash
orc validate
orc validate --config path/to/config.yaml
```

### `orc cancel <ticket>`

Cancels a ticket and archives its artifacts to history. Audit data (costs, timing, archived logs) is preserved by rotating to a timestamped directory.

```bash
orc cancel PROJ-123
orc cancel PROJ-123 --force    # cancel even if a run appears active
orc cancel PROJ-123 --purge    # remove all artifacts including history
```

### `orc history [ticket]`

Lists past runs for a ticket with status, date, duration, and cost. Completed runs are archived immediately. Failed or interrupted runs stay in place for --resume/--retry, and are archived automatically when the next fresh `orc run` starts.

```bash
orc history                     # most recent ticket
orc history PROJ-123            # specific ticket
orc history --prune             # remove entries beyond the history limit
```

### `orc stats [ticket]`

Cross-run aggregate metrics — success rate, cost/duration distributions, per-phase breakdown, failure categories, and weekly cost trends.

```bash
orc stats                    # aggregate across all tickets
orc stats KS-42              # aggregate for a single ticket
orc stats --last 20          # limit to last N runs
orc stats --json             # structured JSON output
```

### `orc eval [case]`

Run eval cases to measure workflow quality. Each case is defined in `.orc/evals/<case>/` with a fixture (git ref + ticket) and rubric (scoring criteria). Runs the workflow in an isolated git worktree and evaluates results.

```bash
orc eval                     # run all eval cases
orc eval bug-fix             # run a specific case
orc eval --report            # show score history across runs
orc eval --list              # list available eval cases
orc eval --json              # structured JSON output
```

### `orc doctor <ticket>`

Diagnoses a failed workflow run using AI. Gathers the failed phase's config, logs, rendered prompt, feedback files, timing data, and loop iteration history, then sends everything to Claude for analysis. Recommends whether to `--retry`, `--from`, or fix-first.

```bash
orc doctor PROJ-123
```

### `orc test <phase> <ticket>`

Runs a single phase in isolation for testing prompts and scripts without running the entire workflow. Sets up the full environment (variables, artifacts dir) as if the workflow were running, dispatches only the specified phase, and does not modify state or advance the workflow.

```bash
orc test plan KS-42              # run just the "plan" phase
orc test implement KS-42         # run just "implement"
orc test 3 KS-42                 # run phase 3 (1-indexed)
orc test -w bugfix fix KS-42     # test a phase from a named workflow
```

| Flag | Description |
|------|-------------|
| `--auto` | Unattended mode — skip gates, no interactive steering |
| `--verbose`, `-v` | Save raw stream-json output to `.stream.jsonl` files |
| `--with-hooks` | Run pre-run and post-run hooks around the phase dispatch |

Missing artifacts from prior phases produce a warning listing which files are absent and which earlier phases normally create them.

By default, pre-run and post-run hooks do not run during `orc test`. Use `--with-hooks` to execute them with the same semantics as a full workflow run. Without the flag, hooks are skipped and hook side effects (e.g., starting/stopping services) will not occur.

### `orc debug <phase> [ticket]`

Analyzes a phase execution — shows the rendered prompt, tool call sequence, cost/token data, feedback injection, and exit status. Useful for understanding why a phase produced unexpected results without manually reading raw log files.

```bash
orc debug plan                    # analyze most recent ticket's "plan" phase
orc debug plan KS-42              # analyze a specific ticket's phase
orc debug 2                       # analyze phase by index (1-indexed)
orc debug -w bugfix plan KS-42    # analyze a phase from a named workflow
```

When no ticket is specified, analyzes the most recently executed ticket.

## Configuration Reference

Workflows are defined in `.orc/config.yaml`.

### Top-level fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Project name |
| `ticket-pattern` | string | No | Regex pattern for ticket IDs (anchored automatically for full-match) |
| `model` | string | No | Default model for all agent phases: `opus`, `sonnet`, or `haiku`. Per-phase `model` overrides this. |
| `effort` | string | No | Default effort for all agent phases: `low`, `medium`, or `high`. Per-phase `effort` overrides this. |
| `cwd` | string | No | Default working directory for script and agent phases (expanded with vars). Per-phase `cwd` overrides this. Not applied to gate phases. |
| `max-cost` | float | No | Per-run cost budget in USD. Workflow stops if cumulative cost exceeds this. |
| `history-limit` | int | No | Maximum archived runs per ticket (default 10) |
| `default-allow-tools` | list | No | Tools auto-approved for all agent phases, merged with built-in defaults. |
| `vars` | map | No | Custom variables expanded at startup (declaration order) |
| `phases` | list | Yes | Ordered list of phases |

### Phase fields

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `name` | string | — | Unique phase name (required) |
| `type` | string | — | `script`, `agent`, or `gate` (required) |
| `description` | string | — | Human-readable description |
| `run` | string | — | Shell command (required for `script`) |
| `prompt` | string | — | Path to prompt template file, relative to project root (required for `agent`) |
| `model` | string | `opus` | Claude model: `opus`, `sonnet`, or `haiku` (agent only). Overrides top-level `model`. |
| `effort` | string | `high` | Effort level: `low`, `medium`, or `high` (agent only). Overrides top-level `effort`. |
| `timeout` | int | 30 (agent), 10 (script) | Timeout in minutes |
| `max-cost` | float | — | Per-phase cost budget in USD (agent only). Workflow stops if phase cost exceeds this. |
| `outputs` | list | — | Expected output filenames in artifacts dir |
| `allow-tools` | list | — | Additional tools to approve for this agent phase, merged with `default-allow-tools` and built-in defaults |
| `mcp-config` | string | — | Path to MCP server config file (agent only). Supports variable expansion. Passed as `--mcp-config` to `claude -p`. File need not exist at config load time. |
| `condition` | string | — | Shell command; phase is skipped if exit code is non-zero |
| `parallel-with` | string | — | Name of another phase to run concurrently |
| `loop` | object | — | Convergent loop: `goto` (phase name), `min` (default 1), `max` (required), optional `check` (shell command for pass/fail), optional `on-exhaust` |
| `cwd` | string | — | Working directory for this phase (expanded with vars). Not supported on gate phases. |
| `pre-run` | string | — | Shell command to run before dispatch. Non-zero exit skips dispatch and fails the phase. Post-run still runs. |
| `post-run` | string | — | Shell command to run after dispatch regardless of outcome (cleanup semantics). Failure overrides dispatch success. |

### Phase types

**script** — Executes a shell command via `bash -c`. The `run` field supports variable substitution. Child processes inherit the parent environment plus `ORC_*` variables.

**agent** — Reads a prompt template file, expands variables, and invokes `claude -p`. Output is streamed to the terminal and saved to `.orc/artifacts/<ticket>/logs/`. The following tools are always approved by default: Read, Edit, Write, Glob, Grep, Task, WebFetch, WebSearch. Add more via `default-allow-tools` (all agents) or `allow-tools` (per phase). If outputs are declared and missing after the agent finishes, orc re-invokes the agent once to produce them. Use `mcp-config` to connect agents to MCP servers with a dynamically-generated config file.

**gate** — Prompts the operator for approval. The operator can type `y` to continue, or any other text to request a revision — the text is captured as feedback in the phase log. Skipped automatically when using `--auto`.

## Complete Example Config

```yaml
name: my-service
ticket-pattern: '[A-Z]+-\d+'
model: opus
max-cost: 10.00

default-allow-tools:
  - Bash

vars:
  WORKTREE: $PROJECT_ROOT/.worktrees/$TICKET

phases:
  - name: setup
    type: script
    description: Create worktree
    run: git worktree add $WORKTREE

  - name: design
    type: agent
    description: Produce a technical design
    prompt: .orc/phases/design.md
    model: opus
    timeout: 20
    cwd: $WORKTREE
    outputs:
      - design.md

  - name: implement
    type: agent
    description: Implement the design
    prompt: .orc/phases/implement.md
    model: opus
    timeout: 45
    cwd: $WORKTREE
    outputs:
      - implementation.md
    loop:
      goto: design
      max: 3
      check: test -f $ARTIFACTS_DIR/review-pass.txt

  - name: test
    type: script
    description: Run test suite
    run: make test
    cwd: $WORKTREE
    condition: test -f Makefile

  - name: lint
    type: script
    description: Run linter
    run: make lint
    cwd: $WORKTREE
    parallel-with: test

  - name: review
    type: gate
    description: Human approval before merge
```

## Variable Substitution

Variables are available using `$VAR` or `${VAR}` syntax in agent prompt templates, script `run` commands, conditions, loop checks, and `pre-run`/`post-run` hooks.

| Variable | Description |
|----------|-------------|
| `$TICKET` | The ticket identifier passed to `orc run` |
| `$ARTIFACTS_DIR` | Absolute path to `.orc/artifacts/<ticket>/` |
| `$WORK_DIR` | Absolute path to the working directory (project root, or `cwd` if set) |
| `$PROJECT_ROOT` | Absolute path to the project root (where `.orc/` lives) |
| `$WORKFLOW` | Current workflow name (empty for single-config projects) |

For agent prompt templates, `cwd`, and `mcp-config` paths, variables are expanded via Go string substitution (with `os.Expand` falling back to environment variables). For bash-executed fields (`run`, `condition`, `loop.check`, `pre-run`, `post-run`), variables are set as environment variables in the child process — standard bash quoting rules apply.

### Custom Variables

Define project-specific variables under `vars:` in `config.yaml`:

```yaml
vars:
  WORKTREE: $PROJECT_ROOT/.worktrees/$TICKET
  SRC: $WORKTREE/src
```

Variables are expanded in declaration order, so later vars can reference earlier ones (`SRC` references `WORKTREE` above). Custom vars are available everywhere built-ins are — prompt templates, `run` commands, `condition`, `loop.check`, `cwd` fields, and `pre-run`/`post-run` hooks.

Custom vars cannot override built-in variables (`TICKET`, `WORKFLOW`, `ARTIFACTS_DIR`, `WORK_DIR`, `PROJECT_ROOT`).

## Artifacts Directory

orc creates a `.orc/artifacts/<ticket>/` directory per ticket to store all run data:

```
.orc/artifacts/<ticket>/
├── state.json              # Current run state (phase_index, ticket, status, failure_category)
├── costs.json              # Per-phase cost and token counts
├── timing.json             # Per-phase timing data
├── loop-counts.json        # Persisted loop iteration counters
├── prompts/                # Rendered prompt for each phase
├── logs/                   # Agent output for each phase
├── feedback/               # Loop/failure feedback
└── history/                # Archived past runs
    └── <run-id>/           # Timestamp-based directory (same layout as parent)
```

**Feedback auto-injection**: When a phase loops (fails or is forced back by `min`), its output is written to `feedback/from-<phase>.md`. On the next iteration, all feedback files are automatically prepended to agent prompts — agents see prior failure context without manual intervention.

### Audit Directory

orc also maintains `.orc/audit/<ticket>/` to preserve data across cancellations and re-runs:

```
.orc/audit/<ticket>/
├── costs.json              # Persisted cost data (survives cancel)
├── timing.json             # Persisted timing data
├── logs/
│   ├── phase-1.iter-1.log  # Archived logs from prior loop iterations
│   └── ...
├── prompts/
│   └── phase-1.iter-1.md   # Archived rendered prompts
└── feedback/
    └── ...                 # Archived feedback files
```

When you run `orc cancel`, the audit directory is preserved (rotated to a timestamped name). `orc status` reads from the audit directory for cost and timing data.

### History Directory

When a run completes, artifacts are archived immediately. Failed or interrupted runs stay in place for `--resume`/`--retry`, and are archived automatically when the next fresh `orc run` starts. The run-id is a filesystem-safe timestamp. Old entries are pruned based on the `history-limit` config field (default 10).

## Loops

The `loop` field is orc's core convergence construct, handling both simple retry and deliberate quality iteration.

```yaml
loop:
  goto: implement       # jump-back target (must be earlier phase)
  min: 1                # minimum iterations even on success (default 1)
  max: 3                # total iterations before exhaustion (required)
  check: test -f $ARTIFACTS_DIR/review-pass.txt  # quality gate (optional)
  on-exhaust: plan      # outer recovery (optional, string or object)
```

**Failure path:** When a phase with `loop` fails, orc writes the failure output to `.orc/artifacts/feedback/from-<phase>.md`, increments the loop counter, and jumps back to `loop.goto`. If the counter reaches `loop.max`, the loop is exhausted.

**Success path:** When a phase with `loop` succeeds but the iteration count is less than `loop.min`, orc forces another iteration (writing the output as feedback). Once iteration >= min, the loop breaks normally.

**Check path:** When a phase with `loop.check` succeeds (exit 0), the check command runs. If the check exits non-zero, orc treats it as a loop failure — writing the check output to feedback and looping back. If the check exits 0, the normal success path applies (min enforcement, then advance). Variables (`$ARTIFACTS_DIR`, etc.) are available as environment variables in the check command. This eliminates the need for a separate `*-check` script phase.

**On-exhaust recovery:** When a loop exhausts, if `on-exhaust` is set, the loop counter resets and orc jumps to the on-exhaust target. This enables outer recovery (e.g., re-plan then re-implement). Accepts a string (`on-exhaust: plan`) or object (`on-exhaust: {goto: plan, max: 2}`).

Loop counts are persisted to `.orc/artifacts/loop-counts.json` and reset when using `--retry`, `--from`, or step-mode backward rewind. Note: `loop.max` means total iterations, not retries.

## Parallel Phases

Two phases can run concurrently using `parallel-with`:

```yaml
- name: test
  type: script
  run: make test

- name: lint
  type: script
  run: make lint
  parallel-with: test
```

Both phases start at the same time. If either fails, the other is cancelled. After both complete, the runner advances past both phases.

**Constraints**: `parallel-with` and `loop` cannot be combined on the same phase.

## Multi-Workflow Support

Projects can define multiple named workflows for different task types:

```
.orc/
  config.yaml           # default workflow
  workflows/
    bugfix.yaml         # named workflows
    refactor.yaml
  phases/               # shared prompt files
```

### Running a Named Workflow

```bash
orc run bugfix TICKET-123           # positional: first arg matches a workflow name
orc run -w bugfix TICKET-123        # explicit: -w/--workflow flag
orc run TICKET-123                   # uses default workflow
```

### Default Workflow Resolution

| Scenario | Default |
|----------|---------|
| Only `config.yaml` | It's the default (flat artifact layout, backward compatible) |
| Only `workflows/` with one file | That sole workflow is the default |
| `config.yaml` + `workflows/` | `config.yaml` is the default |
| Multiple in `workflows/`, no `config.yaml` | Must specify (error lists options) |

In multi-workflow projects, artifacts are namespaced: `.orc/artifacts/<workflow>/<ticket>/`.

### Adding a Workflow

```bash
orc init --add-workflow bugfix                   # minimal starter
orc init --add-workflow bugfix --recipe simple   # from a recipe
```

With `--recipe`, prompt files are also written to `.orc/phases/` (existing files are not overwritten). Without `--recipe`, create prompt files manually.

### Multi-Workflow Commands

```bash
orc flow                   # show all workflows
orc flow -w bugfix         # one workflow
orc validate               # validate all
orc validate -w bugfix     # one workflow
```

## Environment Variables

Child processes (scripts and agents) inherit the parent environment with these additional variables:

| Variable | Description |
|----------|-------------|
| `ORC_TICKET` | The ticket identifier |
| `ORC_WORKFLOW` | Current workflow name (empty for single-config projects) |
| `ORC_ARTIFACTS_DIR` | Absolute path to `.orc/artifacts/<ticket>/` |
| `ORC_WORK_DIR` | Working directory |
| `ORC_PROJECT_ROOT` | Project root directory |
| `ORC_PHASE_INDEX` | Current phase index (0-based) |
| `ORC_PHASE_COUNT` | Total number of phases |
| `ORC_<NAME>` | Custom vars get an `ORC_` prefix (e.g., `WORKTREE` → `ORC_WORKTREE`) |

The `CLAUDECODE` environment variable is stripped from child processes so that `claude -p` can run without nesting conflicts.

## Signal Handling

When you press Ctrl+C (SIGINT) or send SIGTERM/SIGHUP:

- The current phase is cancelled via context cancellation
- State is saved with status `interrupted`
- A resume hint is printed: `orc run <ticket>`

Resume the workflow later — it picks up from the interrupted phase.

## Exit Codes

`orc run` returns structured exit codes for scripting and CI/CD:

| Code | Meaning |
|------|---------|
| 0 | Success — workflow completed, all phases passed |
| 1 | Retryable failure — agent phase failed, loop exhausted, or timeout hit |
| 2 | Human intervention needed — gate denied or cost limit exceeded |
| 3 | Configuration error — invalid config, missing prompt file, setup failure |
| 130 | Signal interrupt — SIGINT, SIGTERM, or SIGHUP received |

## Run Summary

After every run, orc prints a summary table showing each phase's outcome, duration, run count (for looped phases), and separate totals for agent and script time.

For detailed documentation on the execution model, loops, output validation, and more, run `orc docs` to see all available topics — especially `orc docs runner` and `orc docs quality-loops`.

## License

MIT
