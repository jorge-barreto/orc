# orc

Deterministic agent orchestrator CLI — run AI workflows as a state machine, not a conversation.

![Go 1.22+](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go) ![License: MIT](https://img.shields.io/badge/License-MIT-yellow)

## What is orc?

LLM agents that self-manage multi-phase workflows are unreliable. They lose context, skip steps, and hallucinate progress. **orc** takes a different approach: a deterministic state machine drives the workflow, and context passes through artifact files on disk — not conversational memory.

You define your workflow as a series of **phases** in a YAML config file. Each phase is either a shell script, an AI agent invocation, or a human approval gate. orc runs them in order, persists state after each phase, and can resume from where it left off if interrupted.

## Features

- **Three phase types**: `script` (shell commands), `agent` (Claude AI via `claude -p`), `gate` (human y/n approval)
- **Variable substitution**: `$TICKET`, `$ARTIFACTS_DIR`, `$WORK_DIR`, `$PROJECT_ROOT` expanded in prompts and commands
- **Convergent loops**: Phases can loop back with `loop` for retry-on-failure and min-iteration enforcement, with optional `on-exhaust` recovery
- **Parallel execution**: Run two phases concurrently with `parallel-with`
- **Conditional phases**: Skip phases based on a shell command exit code
- **Custom variables**: Define project-specific variables under `vars:` that reference built-ins and each other
- **Per-phase working directory**: Set `cwd` on script/agent phases for worktree workflows
- **Resume from interruption**: State is saved after every phase — Ctrl+C and resume later
- **Dry-run mode**: Preview the phase plan without executing anything
- **Output validation**: Declare expected output files; agents are re-prompted once if outputs are missing
- **Per-phase model and timeout**: Choose `opus`, `sonnet`, or `haiku` per agent phase
- **Full observability**: Rendered prompts, agent logs, timing data, and state all saved to `.orc/artifacts/`

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
orc init

# 2. Edit .orc/config.yaml to define your workflow

# 3. Preview the plan
orc run EXAMPLE-1 --dry-run

# 4. Run for real
orc run EXAMPLE-1
```

## CLI Reference

### `orc init`

Scaffolds a new `.orc/` directory with an example config and prompt file.

```bash
orc init
```

Creates:
- `.orc/config.yaml` — example workflow with script, agent, and gate phases
- `.orc/phases/example.md` — example agent prompt template

### `orc run <ticket>`

Runs the workflow for the given ticket identifier.

```bash
orc run PROJ-123
orc run PROJ-123 --auto        # skip human gates
orc run PROJ-123 --dry-run     # preview without executing
orc run PROJ-123 --retry 3     # retry from phase 3
orc run PROJ-123 --from 2      # start from phase 2
```

| Flag | Description |
|------|-------------|
| `--auto` | Skip all gate phases (auto-approve) |
| `--dry-run` | Print the phase plan without executing |
| `--retry N` | Retry from phase N (1-indexed), resets loop counts |
| `--from N` | Start from phase N (1-indexed), resets loop counts |

`--retry` and `--from` are mutually exclusive.

### `orc status <ticket>`

Shows the current workflow progress, completed phases with timing, remaining phases, and artifacts listing.

```bash
orc status PROJ-123
```

### `orc docs [topic]`

Shows built-in documentation. With no argument, lists available topics. With a topic name, prints the full article.

```bash
orc docs               # list topics
orc docs config        # config file reference
orc docs variables     # template variables and custom vars
orc docs phases        # phase type details
```

Topics: `quickstart`, `config`, `phases`, `variables`, `runner`, `artifacts`, `quality-loops`.

## Configuration Reference

Workflows are defined in `.orc/config.yaml`.

### Top-level fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | Yes | Project name |
| `ticket-pattern` | string | No | Regex pattern for ticket IDs (anchored automatically for full-match) |
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
| `model` | string | `opus` | Claude model: `opus`, `sonnet`, or `haiku` (agent only) |
| `timeout` | int | 30 (agent), 10 (script) | Timeout in minutes |
| `outputs` | list | — | Expected output filenames in artifacts dir |
| `condition` | string | — | Shell command; phase is skipped if exit code is non-zero |
| `parallel-with` | string | — | Name of another phase to run concurrently |
| `loop` | object | — | Convergent loop: `goto` (phase name), `min` (default 1), `max` (required), optional `on-exhaust` |
| `cwd` | string | — | Working directory for this phase (expanded with vars). Not supported on gate phases. |

### Phase types

**script** — Executes a shell command via `bash -c`. The `run` field supports variable substitution. Child processes inherit the parent environment plus `ORC_*` variables.

**agent** — Reads a prompt template file, expands variables, and invokes `claude -p <prompt> --model <model> --dangerously-skip-permissions`. Output is streamed to the terminal and saved to `.orc/artifacts/logs/`.

**gate** — Prompts the operator for y/n approval. Skipped automatically when using `--auto`.

## Complete Example Config

```yaml
name: my-service
ticket-pattern: '[A-Z]+-\d+'

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

Variables are expanded in agent prompt templates and script `run` commands using `$VAR` or `${VAR}` syntax.

| Variable | Description |
|----------|-------------|
| `$TICKET` | The ticket identifier passed to `orc run` |
| `$ARTIFACTS_DIR` | Absolute path to the `.orc/artifacts/` directory |
| `$WORK_DIR` | Absolute path to the working directory (project root) |
| `$PROJECT_ROOT` | Absolute path to the project root (where `.orc/` lives) |

If a variable is not in the built-in set or custom vars, `os.Expand` falls back to environment variables.

### Custom Variables

Define project-specific variables under `vars:` in `config.yaml`:

```yaml
vars:
  WORKTREE: $PROJECT_ROOT/.worktrees/$TICKET
  SRC: $WORKTREE/src
```

Variables are expanded in declaration order, so later vars can reference earlier ones (`SRC` references `WORKTREE` above). Custom vars are available everywhere built-ins are — prompt templates, `run` commands, and `cwd` fields.

Custom vars cannot override built-in variables (`TICKET`, `ARTIFACTS_DIR`, `WORK_DIR`, `PROJECT_ROOT`).

## Artifacts Directory

orc creates a `.orc/artifacts/` directory in the project root to store all run data:

```
.orc/artifacts/
├── state.json              # Current run state (phase_index, ticket, status)
├── timing.json             # Start/end timestamps for each phase
├── loop-counts.json        # Loop iteration counters per phase
├── prompts/
│   ├── phase-1.md          # Rendered prompt for phase 1
│   ├── phase-2.md          # Rendered prompt for phase 2
│   └── ...
├── logs/
│   ├── phase-1.log         # Agent output for phase 1
│   ├── phase-2.log         # Agent output for phase 2
│   └── ...
├── feedback/
│   └── from-implement.md   # Output from failed or looped phase
└── summary.md              # Example declared output artifact
```

## Loops

The `loop` field is orc's core convergence construct, handling both simple retry and deliberate quality iteration.

```yaml
loop:
  goto: implement       # jump-back target (must be earlier phase)
  min: 1                # minimum iterations even on success (default 1)
  max: 3                # total iterations before exhaustion (required)
  on-exhaust: plan      # outer recovery (optional, string or object)
```

**Failure path:** When a phase with `loop` fails, orc writes the failure output to `.orc/artifacts/feedback/from-<phase>.md`, increments the loop counter, and jumps back to `loop.goto`. If the counter reaches `loop.max`, the loop is exhausted.

**Success path:** When a phase with `loop` succeeds but the iteration count is less than `loop.min`, orc forces another iteration (writing the output as feedback). Once iteration >= min, the loop breaks normally.

**On-exhaust recovery:** When a loop exhausts, if `on-exhaust` is set, the loop counter resets and orc jumps to the on-exhaust target. This enables outer recovery (e.g., re-plan then re-implement). Accepts a string (`on-exhaust: plan`) or object (`on-exhaust: {goto: plan, max: 2}`).

Loop counts are persisted to `.orc/artifacts/loop-counts.json` and reset when using `--retry` or `--from`. Note: `loop.max` means total iterations, not retries.

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

## Environment Variables

Child processes (scripts and agents) inherit the parent environment with these additional variables:

| Variable | Description |
|----------|-------------|
| `ORC_TICKET` | The ticket identifier |
| `ORC_ARTIFACTS_DIR` | Absolute path to `.orc/artifacts/` |
| `ORC_WORK_DIR` | Working directory |
| `ORC_PROJECT_ROOT` | Project root directory |
| `ORC_PHASE_INDEX` | Current phase index (0-based) |
| `ORC_PHASE_COUNT` | Total number of phases |
| `ORC_<NAME>` | Custom vars get an `ORC_` prefix (e.g., `WORKTREE` → `ORC_WORKTREE`) |

The `CLAUDECODE` environment variable is stripped from child processes so that `claude -p` can run without nesting conflicts.

## Signal Handling

When you press Ctrl+C (SIGINT) or send SIGTERM:

- The current phase is cancelled via context cancellation
- State is saved with status `interrupted`
- A resume hint is printed: `orc run <ticket>`

Resume the workflow later — it picks up from the interrupted phase.

## License

MIT
