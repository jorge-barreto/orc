package docs

var topics = []Topic{
	{
		Name:    "quickstart",
		Title:   "Quick Start",
		Summary: "Getting started with orc",
		Content: topicQuickstart,
	},
	{
		Name:    "config",
		Title:   "Configuration Reference",
		Summary: "Config file schema, fields, and defaults",
		Content: topicConfig,
	},
	{
		Name:    "phases",
		Title:   "Phase Types",
		Summary: "Script, agent, and gate phase details",
		Content: topicPhases,
	},
	{
		Name:    "variables",
		Title:   "Template Variables",
		Summary: "Built-in vars, custom vars, and environment variables",
		Content: topicVariables,
	},
	{
		Name:    "runner",
		Title:   "Execution Model",
		Summary: "Conditions, parallel execution, loops, and resuming",
		Content: topicRunner,
	},
	{
		Name:    "artifacts",
		Title:   "Artifacts Directory",
		Summary: "Structure of .orc/artifacts/ and what gets saved",
		Content: topicArtifacts,
	},
	{
		Name:    "quality-loops",
		Title:   "Adversarial Quality Loops",
		Summary: "Writing review prompts that catch real issues",
		Content: topicQualityLoops,
	},
	{
		Name:    "workflows",
		Title:   "Multi-Workflow Support",
		Summary: "Named workflow configs for different task types",
		Content: topicWorkflows,
	},
	{
		Name:    "devtools",
		Title:   "Developer Tools",
		Summary: "orc test, debug, report, improve, eval, flow, and doctor",
		Content: topicDevtools,
	},
	{
		Name:    "eval",
		Title:   "Eval Framework",
		Summary: "Measure workflow quality with eval cases and rubrics",
		Content: topicEval,
	},
}

const topicQuickstart = `Quick Start
===========

1. Initialize a project:

    cd your-project
    orc init
    orc init "description of desired workflow"
    orc init --recipe standard              # scaffold from a built-in recipe
    orc init --list-recipes                 # show available recipes

   This analyzes your project and generates .orc/config.yaml and prompt
   files tailored to your codebase. If AI generation fails, a default
   template is used instead.

2. Edit .orc/config.yaml to define your workflow. A workflow is a list
   of phases — each phase is a script, agent, or gate.

3. Preview the plan without executing:

    orc run TICKET-1 --dry-run

4. Run for real:

    orc run TICKET-1

5. Check progress:

    orc status TICKET-1

CLI Flags
---------

  orc run <ticket>              Run the workflow
  orc run <ticket> --auto       Skip human gate phases
  orc run <ticket> --dry-run    Preview phase plan
  orc run <ticket> --retry <phase>    Retry from phase (number or name)
  orc run <ticket> --from <phase>     Start from phase (number or name)
  orc run <ticket> --resume        Resume interrupted agent phase session
  orc run <ticket> --step          Step through phases interactively
  orc run <ticket> --headless     Non-interactive mode for CI/CD (implies --auto)
  orc flow                        Visualize workflow as a flow diagram
  orc run -w bugfix <ticket>    Run a named workflow (multi-workflow projects)
  orc flow -w bugfix            Flow diagram for a specific workflow
  orc --no-color flow             Flow diagram without color (flag works on any command)
  orc cancel <ticket>           Cancel run and archive artifacts to history
  orc cancel <ticket> --purge   Cancel and remove all artifacts including history
  orc cancel <ticket> --force   Cancel even if a run appears active
  orc history                   List past runs for most recent ticket
  orc history <ticket>          List past runs for a specific ticket
  orc history --prune           Remove history beyond the configured limit
  orc status <ticket>           Show workflow status for a ticket
  orc report                    Generate a run report (most recent ticket)
  orc report <ticket>           Report for a specific ticket
  orc report --json             Structured JSON output
  orc doctor <ticket>           Diagnose a failed run using AI
  orc init                      Initialize .orc/ directory (AI-powered)
  orc init "description"        Guide AI generation with a description
  orc init --recipe <name>      Scaffold from a recipe (simple, standard, full-pipeline, review-loop)
  orc init --list-recipes       Show available recipes with descriptions
  orc docs                      List documentation topics
  orc docs <topic>              Show a documentation topic
  orc improve "..."             Apply a specific change to the workflow
  orc improve                   Interactive AI-assisted workflow refinement
  orc test <phase> <ticket>   Run one phase in isolation for testing
  orc debug <phase> [ticket]  Analyze a phase execution

--retry and --from accept a 1-indexed phase number or a phase name.
They are mutually exclusive. Both reset loop counts.

--resume uses the saved Claude session ID to continue an interrupted agent
phase. Mutually exclusive with --retry and --from. If the session has expired,
falls back to a fresh start automatically.

--step pauses after each phase with an interactive prompt (continue,
rewind, abort, or inspect artifacts). Incompatible with --auto.

--headless runs in fully non-interactive mode: implies --auto (gates
auto-approved, no steering), disables ANSI color codes, and produces
clean parseable output. Designed for CI/CD pipelines, cron jobs, and
wrapper scripts. Exit codes are the primary status signal. Incompatible
with --step.
`

const topicConfig = `Configuration Reference
=======================

Workflows are defined in .orc/config.yaml, or in named files under
.orc/workflows/ for multi-workflow projects (see 'orc docs workflows').

Top-level fields
----------------

  name                string    Required. Project name.
  ticket-pattern      string    Regex for ticket IDs (anchored automatically).
  default-allow-tools list      Tools auto-approved for all agent phases.
                                Merged with built-in defaults (see 'orc docs phases').
  model               string    Default model for all agent phases. "opus", "sonnet",
                                or "haiku". Per-phase model overrides this.
  cwd                 string    Default working directory for script and agent phases.
                                Expanded with vars. Per-phase cwd overrides this.
                                Not applied to gate phases.
  effort              string    Default effort for all agent phases. "low", "medium",
                                or "high". Per-phase effort overrides this.
  max-cost            float     Per-run cost budget in USD. Workflow stops with
                                exit code 2 if cumulative cost exceeds this.
  history-limit       int       Maximum archived runs per ticket. Default 10.
                                Set to prevent unbounded disk usage.
  vars                map       Custom variables expanded at startup (declaration order).
  phases              list      Required. Ordered list of phases.

Phase fields
------------

  name             string    Required. Unique phase name. Must be a simple
                             name (no path separators or '.' / '..').
  type             string    Required. "script", "agent", or "gate".
  description      string    Human-readable description.
  run              string    Shell command (required for script phases).
  prompt           string    Path to prompt template, relative to project root
                             (required for agent phases).
  model            string    "opus" (default), "sonnet", or "haiku" (agent only).
  timeout          int       Minutes. Default: 30 (agent), 10 (script).
  max-cost         float     Per-phase cost budget in USD (agent only). Workflow
                             stops with exit code 2 if phase cost exceeds this.
  outputs          list      Expected output filenames in artifacts dir.
  condition        string    Shell command; phase skipped if exit code non-zero.
  parallel-with    string    Name of another phase to run concurrently.
  loop             object    Convergent loop: goto (phase name), min (default 1),
                             max (required), optional check (shell command — if exit
                             non-zero, treated as failure), and optional on-exhaust
                             for recovery.
  allow-tools      list      Additional tools to approve for this agent phase.
                             Merged with defaults. Only valid on agent phases.
  mcp-config       string    Path to MCP server config file (agent only). Supports
                             variable expansion. Passed as --mcp-config to claude.
                             File need not exist at config validation time (may be
                             produced by a prior phase).
  cwd              string    Working directory for this phase (expanded with vars).
                             Not supported on gate phases.
  pre-run          string    Shell command to run before dispatch. Non-zero exit
                             skips dispatch and fails the phase. Post-run still
                             runs. Supports variable expansion.
  post-run         string    Shell command to run after dispatch (cleanup semantics).
                             Runs regardless of dispatch outcome. If post-run fails
                             and dispatch succeeded, phase is marked failed.
                             Supports variable expansion.

Custom Variables (vars)
-----------------------

The vars field is an ordered key-value map at the top level of config.yaml.
Variables are expanded at startup in declaration order, so later vars can
reference earlier ones. Custom vars cannot override built-in variables
(TICKET, WORKFLOW, ARTIFACTS_DIR, WORK_DIR, PROJECT_ROOT). Duplicate names are not
allowed.

Validation Rules
----------------

- Phase names must be unique.
- Phase names must not contain path separators or be '.' / '..'.
- loop.goto must reference an earlier phase (no forward jumps).
- loop.max is required and means total iterations (not retries).
- loop.on-exhaust.goto must reference an earlier phase.
- parallel-with must reference an existing phase.
- parallel-with and loop cannot be combined on the same phase.
- Agent phases require a prompt file that exists on disk.
- Model must be opus, sonnet, haiku, or empty.
- Output filenames must be simple filenames (no path separators, . or ..).
- mcp-config is only valid on agent phases.
- Gate phases cannot have a cwd field.
- history-limit must not be negative. Defaults to 10 if unset.

Example Config
--------------

  name: my-service
  ticket-pattern: '[A-Z]+-\d+'
  model: opus
  cwd: $WORKTREE

  default-allow-tools:
    - "mcp__atlassian__*"
    - Bash

  vars:
    WORKTREE: $PROJECT_ROOT/.worktrees/$TICKET
    SRC: $WORKTREE/src

  phases:
    - name: setup
      type: script
      description: Create worktree
      run: git worktree add $WORKTREE
      cwd: .                         # override: run in project root

    - name: implement
      type: agent
      description: Implement the ticket
      prompt: .orc/phases/implement.md
      # inherits model: opus, cwd: $WORKTREE
      outputs:
        - summary.md
      loop:
        goto: implement
        max: 3

    - name: test
      type: script
      description: Run tests
      run: make test
      # inherits cwd: $WORKTREE
      loop:
        goto: implement
        max: 5

    - name: review
      type: gate
      description: Human approval
      # gate phases don't inherit cwd
`

const topicPhases = `Phase Types
===========

script
------

Executes a shell command via bash -c. The run field supports variable
substitution ($TICKET, $ARTIFACTS_DIR, custom vars, etc.). Child processes
inherit the parent environment plus ORC_* variables.

If a cwd field is set, the script runs in that directory (expanded with the
full vars map). Otherwise it runs in the project root.

Example:

  - name: test
    type: script
    run: make test
    timeout: 15
    condition: test -f Makefile
    cwd: $WORKTREE

agent
-----

Reads a prompt template file, expands variables, and invokes:

  claude -p <prompt> --model <model> --allowedTools <tools...>

Output is streamed to the terminal and saved to .orc/artifacts/<ticket>/logs/phase-N.log.
The rendered prompt is saved to .orc/artifacts/<ticket>/prompts/phase-N.md.

Tool Permissions
~~~~~~~~~~~~~~~~

The following tools are always approved (built-in defaults):

  Read, Edit, Write, Glob, Grep, Task, WebFetch, WebSearch

Additional tools can be approved at two levels:

  default-allow-tools    Top-level config. Applied to all agent phases.
                         Use for project-wide tools like MCP servers.
  allow-tools            Per-phase config. Applied to a single phase.
                         Use for phase-specific tools like Bash.

All lists are merged and deduplicated. In attended mode (without --auto),
if the agent attempts a tool that wasn't pre-approved, orc prompts you
to approve it for the remainder of that phase.

Agent Questions
~~~~~~~~~~~~~~~

In attended mode (without --auto), if the agent calls AskUserQuestion,
orc displays the question and any options in the terminal and waits for
your answer. You can select a numbered option or type a custom response.
The answer is forwarded to the agent via --resume, and the agent continues
with your input.

In unattended mode (--auto), agent questions are logged as warnings but
not answered — the agent proceeds with Claude Code's internal handling.

MCP Configuration
~~~~~~~~~~~~~~~~~

Agent phases can connect to MCP servers by specifying a config file:

  mcp-config: $ARTIFACTS_DIR/mcp-config.json

The path supports variable expansion ($ARTIFACTS_DIR, custom vars, etc.).
When set, orc passes --mcp-config <expanded-path> to claude -p. The file
need not exist at config load time — it can be produced by a prior script
phase (e.g., launching a browser and writing the CDP endpoint).

Example:

  - name: browser-setup
    type: script
    run: .orc/scripts/launch-browser.sh
    outputs: [mcp-config.json]

  - name: verify
    type: agent
    prompt: .orc/phases/verify.md
    mcp-config: $ARTIFACTS_DIR/mcp-config.json
    allow-tools:
      - "mcp__playwright__*"

If outputs are declared and missing after the agent finishes, orc re-invokes
the agent once with a prompt asking it to produce the missing files. If they
are still missing after the retry, the phase fails.

If a cwd field is set, the agent runs in that directory.

Example:

  - name: implement
    type: agent
    prompt: .orc/phases/implement.md
    model: opus
    timeout: 45
    allow-tools:
      - Bash
    outputs:
      - design.md
    loop:
      goto: plan
      max: 3

Hooks (pre-run / post-run)
--------------------------

Any phase type can have pre-run and post-run hooks — shell commands that
bracket the main dispatch:

  - name: verify
    type: agent
    prompt: .orc/phases/verify.md
    pre-run: .orc/scripts/start-browser.sh
    post-run: .orc/scripts/stop-browser.sh

pre-run runs before dispatch. If it exits non-zero, dispatch is skipped
and the phase fails — but post-run still executes (cleanup semantics).

post-run runs after dispatch regardless of outcome. If post-run fails and
dispatch succeeded, the phase is marked failed. If dispatch already failed,
post-run failure is logged as a warning.

Both hooks:
- Variables available as environment variables ($TICKET, $ARTIFACTS_DIR, custom vars, etc.)
- Run in the phase's cwd (or project root if unset)
- Do NOT run when condition causes a phase skip
- In loops, run every iteration
- In parallel-with, wrap each goroutine's dispatch
- Output is captured in the phase log file
- Do NOT run during orc test unless --with-hooks is passed

Testing Phases in Isolation
~~~~~~~~~~~~~~~~~~~~~~~~~~~

Use orc test to run a single phase without running the full workflow:

  orc test implement KS-42     Run just the "implement" phase
  orc test 3 KS-42             Run phase 3 (1-indexed)

This sets up the full environment (variables, artifacts dir) but does not
modify state or advance the workflow. Missing artifacts from prior phases
produce a warning.

Note: by default, orc test skips pre-run and post-run hooks. Use
--with-hooks to execute them with the same semantics as a full workflow
run (pre-run failure skips dispatch; post-run runs regardless for cleanup).

Use orc debug to analyze what happened during a phase execution:

  orc debug plan KS-42           Show prompt, tool calls, costs, status
  orc debug 3                    Analyze phase 3 of the most recent ticket

gate
----

Prompts the operator for approval at the terminal. The operator can type
"y" to continue, or any other text to request a revision — the text is
captured as feedback in the phase log and the workflow stops.

When --auto is passed, all gate phases are automatically approved and skipped.
When --headless is used, gates are also auto-approved (headless implies --auto).

Gate phases do not support the cwd field.

Example:

  - name: review
    type: gate
    description: Review implementation before merging
`

const topicVariables = `Template Variables
==================

Variables are available in agent prompt templates, script run commands,
conditions, loop checks, phase cwd fields, and pre-run/post-run hooks
using $VAR or ${VAR} syntax.

For agent prompt templates, cwd, and mcp-config fields, variables are
expanded via Go string substitution before use.

For bash-executed fields (run, condition, loop.check, pre-run, post-run),
variables are set as environment variables in the child process. This means
bash quoting rules apply normally — single quotes prevent expansion, double
quotes allow it, just like any shell script.

Built-in Variables
------------------

  $TICKET          The ticket identifier passed to orc run.
  $ARTIFACTS_DIR   Absolute path to the .orc/artifacts/<ticket>/ directory.
  $WORK_DIR        Absolute path to the working directory (project root).
  $PROJECT_ROOT    Absolute path to the project root (where .orc/ lives).
  $WORKFLOW        Current workflow name (empty for single-config projects).

For Go-expanded fields, if a variable is not in the built-in set or custom
vars, os.Expand falls back to environment variables. For bash-executed
fields, the child process inherits the full parent environment.

Custom Variables
----------------

Define project-specific variables under the vars field in config.yaml:

  vars:
    WORKTREE: $PROJECT_ROOT/.worktrees/$TICKET
    SRC: $WORKTREE/src

Key behaviors:

- Expanded at startup in declaration order. Later vars can reference
  earlier ones (e.g., SRC references WORKTREE above).
- Available everywhere built-ins are: prompt templates, run commands,
  condition, loop.check, cwd fields, and pre-run/post-run hooks.
- Cannot override built-in variables (TICKET, WORKFLOW, ARTIFACTS_DIR, WORK_DIR,
  PROJECT_ROOT). Config validation rejects attempts to do so.
- No duplicate variable names allowed.

Environment Variables (ORC_* prefix)
------------------------------------

Child processes (scripts and agents) inherit the parent environment with
these additional ORC_-prefixed variables:

  ORC_TICKET           The ticket identifier.
  ORC_WORKFLOW         Current workflow name (empty for single-config projects).
  ORC_ARTIFACTS_DIR    Absolute path to .orc/artifacts/<ticket>/.
  ORC_WORK_DIR         Working directory.
  ORC_PROJECT_ROOT     Project root directory.
  ORC_PHASE_INDEX      Current phase index (0-based).
  ORC_PHASE_COUNT      Total number of phases.

Custom vars are also exported with an ORC_ prefix. For example, a var
named WORKTREE becomes ORC_WORKTREE in child processes.

The CLAUDECODE environment variable is stripped from child processes so
that claude -p can run without nesting conflicts.
`

const topicRunner = `Execution Model
===============

orc runs your workflow as a deterministic state machine. The runner
iterates through phases in order, dispatching each one and persisting
state after completion.

Conditions
----------

A phase with a condition field runs a shell command first. If the
command exits non-zero, the phase is skipped.

  - name: test
    type: script
    run: make test
    condition: test -f Makefile

Parallel Execution
------------------

Two phases can run concurrently using parallel-with:

  - name: test
    type: script
    run: make test

  - name: lint
    type: script
    run: make lint
    parallel-with: test

Both phases start at the same time. If either fails, the other is
cancelled. After both complete, the runner advances past both phases.

Constraints: parallel-with and loop cannot be combined on the same
phase.

Loops
-----

The loop field is orc's core convergence construct. It handles both
simple error retry and deliberate quality iteration.

  loop:
    goto: implement       # jump-back target (must be earlier phase)
    min: 1                # minimum iterations even on success (default 1)
    max: 3                # total iterations before exhaustion (required)
    check: test -f ...    # quality gate command (optional)
    on-exhaust: plan      # outer recovery (optional, string or object)

Failure path: When a phase with loop fails, the failure output is
written to .orc/artifacts/<ticket>/feedback/from-<phase>.md, the loop
counter increments, and the runner jumps back to loop.goto. On the
next iteration, all feedback files are automatically prepended to
agent prompts so agents see prior failure context. If the counter
reaches loop.max, the loop is exhausted.

Success path: When a phase with loop succeeds but the iteration count
is less than loop.min, the runner forces another iteration (writing
the output as feedback). Once iteration >= min, the loop breaks and
the runner advances normally.

Check path: When a phase with loop.check succeeds (exit 0), the check
command runs with all variables available as environment variables ($ARTIFACTS_DIR, custom vars,
etc.). If the check exits non-zero, orc treats it as a loop failure —
writing the check output to feedback and looping back. If the check
exits 0, the normal success path applies. This eliminates the need for
a separate script phase solely to evaluate loop pass/fail.

On-exhaust recovery: When a loop exhausts, if on-exhaust is set, the
runner resets the loop counter and jumps to the on-exhaust target.
This allows an outer recovery strategy (e.g., re-plan then re-implement).
on-exhaust accepts a string (phase name, fires once) or an object
({goto: plan, max: 2} for multiple recovery attempts).

Loop counts are persisted to .orc/artifacts/<ticket>/loop-counts.json
and reset when using --retry, --from, or step-mode backward rewind.

Note: loop.max means total iterations, not retries. A phase with
max: 3 runs at most 3 times before exhaustion.

Output Validation
-----------------

Agent phases can declare expected outputs:

  outputs:
    - design.md
    - plan.md

After the agent finishes, orc checks whether each file exists in the
artifacts directory. If any are missing, the agent is re-invoked once
with a prompt requesting the missing files. If they are still missing,
the phase fails.

Exit Codes
----------

orc run returns structured exit codes for scripting and CI/CD:

  0    Success. Workflow completed, all phases passed.
  1    Retryable failure. An agent phase failed, a loop exceeded
       its max, or a timeout was hit. A fresh orc run might succeed.
  2    Human intervention needed. A gate was denied, a cost limit was
       exceeded, or a phase produced an unrecoverable error. Don't retry
       automatically.
  3    Configuration or setup error. Config invalid, prompt file missing,
       required binary not found. Fix the config before retrying.
  130  Signal interrupt. SIGINT (Ctrl+C), SIGTERM, or SIGHUP was received.

A wrapper script can check $? to decide whether to retry:

  orc run TICKET-1
  case $? in
    0) echo "Success" ;;
    1) echo "Retryable — running again..." ; orc run TICKET-1 ;;
    2) echo "Needs human intervention" ;;
    3) echo "Fix the config" ;;
    130) echo "Interrupted" ;;
  esac

Signal Handling
---------------

When you press Ctrl+C (SIGINT) or send SIGTERM/SIGHUP:

- The current phase is cancelled via context cancellation.
- State is saved with status "interrupted".
- Exit code 130 is returned (conventional for SIGINT).
- A resume hint is printed: orc run <ticket>

Resume the workflow later — it picks up from the interrupted phase.

Resuming
--------

orc automatically resumes from the last completed phase. State is saved
to .orc/artifacts/<ticket>/state.json after every phase. To manually control the
resume point:

  orc run TICKET --retry 3           Retry from phase 3 (re-runs phase 3)
  orc run TICKET --from 2            Start from phase 2 (skips phase 1)
  orc run TICKET --from implement    Start from the "implement" phase

Both flags reset loop counts.

Agent Session Resume
~~~~~~~~~~~~~~~~~~~~

When an agent phase is interrupted, orc saves its Claude session ID to
state.json. Use --resume to continue the interrupted session:

  orc run TICKET --resume

This invokes claude -p --resume <session-id>, recovering in-progress work.
If the saved session has expired, orc automatically falls back to a fresh
start. --resume is mutually exclusive with --retry and --from, and does not
reset loop counts.

The session ID is cleared when a phase completes successfully or when
using --retry/--from.

Step-Through Mode
~~~~~~~~~~~~~~~~~

Use --step to pause after each phase for interactive inspection:

  orc run TICKET --step

After each phase completes, orc shows artifacts written and presents
a prompt:

  ✓ Phase 2 complete (4m 30s)

  Artifacts written:
    plan.md (4.2 KB)

  [c]ontinue  [r]ewind to phase  [a]bort  [i]nspect artifact > _

Commands:
  c, continue            Proceed to the next phase
  r N, rewind N          Jump back to phase N (1-indexed number)
  r <name>, rewind name  Jump back to a named phase
  a, abort               Stop the run, save state as interrupted
  i <file>, inspect file Print an artifact file to the terminal

Rewind preserves artifacts from completed phases. When rewinding
backward, loop counters for phases between the target and the current
position are reset so loops re-execute correctly.

--step is incompatible with --auto (step-through requires interactive
input).

Cancelling
----------

orc cancel archives the current run's artifacts to history/ before
cleaning up, so past runs are preserved:

  orc cancel TICKET

To completely remove all artifacts including history, use --purge:

  orc cancel TICKET --purge

If state.json shows status "running", cancel refuses by default — press
Ctrl+C in the running terminal first, or pass --force:

  orc cancel TICKET --force
`

const topicArtifacts = `Artifacts Directory
===================

orc creates a .orc/artifacts/<ticket>/ directory in the project root to store all
run data. In multi-workflow projects, artifacts are namespaced by workflow:
.orc/artifacts/<workflow>/<ticket>/ (see 'orc docs workflows'). This directory
is the primary mechanism for passing context between phases — phases read and
write files here rather than relying on conversational memory.

Directory Structure
-------------------

  .orc/artifacts/<ticket>/
  ├── state.json              Current run state
  ├── timing.json             Start/end timestamps per phase
  ├── costs.json              Per-phase cost and token counts
  ├── loop-counts.json        Loop iteration counters per phase
  ├── prompts/
  │   ├── phase-1.md          Rendered prompt for phase 1
  │   ├── phase-2.md          Rendered prompt for phase 2
  │   └── ...
  ├── logs/
  │   ├── phase-1.log           Agent output for phase 1
  │   ├── phase-1.meta.json     Structured metadata for phase 1
  │   ├── phase-2.log           Agent output for phase 2
  │   ├── phase-2.meta.json     Structured metadata for phase 2
  │   └── ...
  ├── feedback/
  │   └── from-<phase>.md     Output from failed or looped phase
  └── history/                Archived past runs
      └── <run-id>/
          └── (same layout as parent)

state.json
----------

Tracks the current phase index, ticket identifier, workflow status
(running, completed, failed, interrupted), the Claude session ID
for interrupted agent phases (used by --resume), and for failed/interrupted
runs, a failure_category (loop_exhaustion, cost_overrun, gate_rejection,
script_failure, output_missing, interrupted, agent_error) and optional
failure_detail with a human-readable description. Written atomically
after every phase.

timing.json
-----------

Records start and end timestamps for each phase. Useful for observability
and performance analysis.

loop-counts.json
----------------

Tracks loop iteration counts per phase. Reset when using --retry or
--from flags.

prompts/
--------

Rendered prompts for agent phases (after variable expansion). Saved as
phase-N.md where N is the 1-indexed phase number. Useful for debugging
what the agent actually received.

logs/
-----

Raw output from agent phases, saved as phase-N.log. Contains the full
agent response.

logs/*.meta.json
----------------

Structured metadata for each phase, written as phase-N.meta.json alongside
the corresponding log file. Contains timing, cost, tokens, tools used, exit
code, model, and session ID. Useful for programmatic analysis — consumed by
orc report for richer output.

Fields:
  phase_name         string     Phase name from config
  phase_type         string     "agent", "script", or "gate"
  phase_index        int        0-indexed phase number
  model              string     Model used (agent phases only)
  effort             string     Effort level (agent phases only)
  session_id         string     Claude session ID (agent phases only)
  start_time         string     ISO 8601 timestamp
  end_time           string     ISO 8601 timestamp
  duration_seconds   float      Wall-clock seconds
  cost_usd           float      Cost in USD (agent phases only)
  input_tokens       int        Input token count (agent phases only)
  output_tokens      int        Output token count (agent phases only)
  exit_code          int        Process exit code
  tools_used         []string   Unique tool names invoked (agent phases only)
  tools_denied       []string   Tools denied by permissions (agent phases only)
  timed_out          bool       Whether the phase was killed by timeout

feedback/
---------

When a phase with a loop fails (or succeeds but min is not yet met),
its output is written to feedback/from-<phase-name>.md. Feedback is
automatically prepended to agent prompts on the next iteration, so
agents receive prior failure context without needing to manually read
feedback files. Multiple feedback files are concatenated with headers
(e.g., "--- Feedback from review ---").

Audit Directory
---------------

orc maintains a separate .orc/audit/<ticket>/ directory that preserves
cost data, timing, and archived iteration logs across cancellations and
re-runs. When a phase loops, its previous iteration's logs and prompts
are archived here with iteration numbers (e.g., phase-1.iter-1.log).

When you run orc cancel, the audit directory is preserved by rotating
to a timestamped name. orc status reads from the audit directory for
cost and timing data.

History Directory
-----------------

When a run completes, orc archives the artifacts
to .orc/artifacts/<ticket>/history/<run-id>/. The run-id is a filesystem-safe
timestamp (e.g., 2026-03-22T14-30-05.123). Completed runs are archived
immediately. Failed or interrupted runs stay in place for --resume/--retry,
and are archived automatically when the next fresh orc run starts.

  .orc/artifacts/<ticket>/
  ├── history/
  │   ├── 2026-03-22T14-30-05.123/
  │   │   ├── state.json
  │   │   ├── timing.json
  │   │   ├── costs.json
  │   │   └── ...
  │   └── 2026-03-21T09-15-00.456/
  │       └── ...
  └── (current run files)

The history directory is preserved across cancellations. Use orc history
to list past runs. Old entries are pruned automatically based on the
history-limit config field (default 10).

If a previous run left stale artifacts (completed, failed, interrupted,
or killed mid-execution), the next orc run auto-archives them before
starting. Recovery flags (--resume, --retry, --from) skip auto-archiving
so the prior run's state is preserved.

Declared Outputs
----------------

Phases can declare expected output files via the outputs field. These
files are expected to appear directly in the .orc/artifacts/<ticket>/ directory (not
in subdirectories). Output filenames must be simple filenames (no path separators, . or ..).
`

const topicQualityLoops = `Adversarial Quality Loops
========================

orc's core differentiator is deterministic, auditable, quality-assured code
delivery through adversarial review loops. The loop construct provides
the iteration structure. The prompts provide the teeth. This doc covers how
to write review prompts that actually catch real issues.

Anatomy of an Adversarial Loop
------------------------------

Every adversarial loop has three components:

  1. Doer phase (agent)    — produces work (a plan, code, a document)
  2. Checker phase (agent) — reviews the work, writes findings
  3. Decision gate (script) — checks for a pass signal file; fails if absent

Example config:

  - name: implement
    type: agent
    prompt: .orc/phases/implement.md

  - name: review
    type: agent
    prompt: .orc/phases/review.md
    outputs:
      - review-findings.md

  - name: review-check
    type: script
    run: >
      test -f $ARTIFACTS_DIR/review-pass.txt ||
      { cat $ARTIFACTS_DIR/review-findings.md 2>/dev/null; exit 1; }
    loop:
      goto: implement
      max: 3

The checker writes a findings file every time. It only writes the pass
signal file (review-pass.txt) when zero blocking issues remain. The
decision gate's loop sends the doer back with feedback injected
automatically.

With loop.check, the decision gate can be inlined:

  - name: implement
    type: agent
    prompt: .orc/phases/implement.md

  - name: review
    type: agent
    prompt: .orc/phases/review.md
    outputs:
      - review-findings.md
    loop:
      goto: implement
      max: 3
      check: test -f $ARTIFACTS_DIR/review-pass.txt

This replaces the 3-phase pattern (doer + checker + script gate) with
a 2-phase pattern (doer + checker-with-check).

The Asymmetry Principle
-----------------------

The doer and checker have fundamentally different jobs. Do not write them
symmetrically.

The doer is:
  - Surgical and targeted (especially on retries)
  - Plan-following and constrained
  - Focused on fixing specific issues from feedback

The checker is:
  - Comprehensive and aggressive
  - Independent — reviews the work with fresh eyes
  - Responsible for finding issues the doer didn't anticipate

A common mistake is writing a checker that is deferential to the doer
("only flag issues the plan asked for"). This defeats the purpose. The
checker must review the WORK, not just verify the plan was followed. If
the checker finds a real bug the plan didn't anticipate, that is a
blocking issue.

Clean Slate
-----------

Every checker prompt should start by removing the previous pass signal:

  rm -f "$ARTIFACTS_DIR/review-pass.txt"

Without this, a pass signal from a prior run can short-circuit the
review entirely.

Iteration-Aware Rigor
---------------------

The checker should behave differently depending on which loop iteration
it's on. Read the loop counter from artifacts:

  cat "$ARTIFACTS_DIR/loop-counts.json" 2>/dev/null || echo "first"

Apply rigor by iteration:

  First review    Maximum scrutiny. Examine everything. You MUST find
                  blocking issues — non-trivial work invariably has
                  substantive problems on first inspection. A reviewer
                  that passes on the first iteration is rubber-stamping.

  Second review   Verify prior issues are resolved. Apply fresh scrutiny
                  to areas the doer changed — fixes often introduce new
                  problems. Still expect to find issues.

  Third+ review   May pass if zero blocking issues remain. Apply the
                  convergence rule: don't hold work hostage over minor
                  preferences. The work doesn't need to be perfect — it
                  needs to be correct and complete.

This creates natural convergence: each loop tightens around remaining
substantive problems, then releases when quality is sufficient.

The "When in Doubt, Block" Policy
---------------------------------

The checker prompt must explicitly state: if you're uncertain whether
something is blocking or a suggestion, classify it as blocking.

This flips the natural bias. Without this instruction, reviewers
default to permissive — they avoid raising issues to seem cooperative.
The explicit policy gives the reviewer permission to be aggressive.

The cost asymmetry supports this: a false positive (flagging something
minor as blocking) costs one loop iteration where the doer addresses
it and the checker downgrades it. A false negative (missing a real
issue) ships broken code.

Structured Blocking Taxonomy
----------------------------

Don't leave "what counts as blocking" to the reviewer's judgment.
Enumerate it explicitly. The taxonomy should be domain-specific.

For code review, blocking means:
  - Tests failing
  - Bugs or incorrect behavior
  - Missing acceptance criteria
  - Regressions in existing functionality
  - Security issues (injection, traversal, unsanitized input)
  - Missing error handling at system boundaries
  - Race conditions, nil dereferences, index-out-of-range risks

For code review, NOT blocking means:
  - Stylistic convention violations
  - Alternative approaches when the current one works
  - Additional tests beyond adequate coverage
  - Cosmetic issues

Always pair the blocking taxonomy with a "what is NOT blocking" section.
Without it, aggressive reviewers will block on style.

Verification Requirement
------------------------

The checker must read the actual source before asserting something is
wrong. Include this as an explicit rule:

  "If you assert something is a bug or that behavior is incorrect,
   trace through the code to confirm. Do not make unverified claims."

Without this, the checker can hallucinate issues, sending the doer on
wild goose chases that waste loop iterations.

Convergence Rules
-----------------

Adversarial loops need a convergence mechanism. Without one, an
aggressive checker can hold work hostage indefinitely.

Two mechanisms work together:

  1. loop.max (config)       Hard limit on total iterations. Set this
                             based on domain: 3 for plan review,
                             3-4 for code review, up to 11 for
                             test-fix loops.

  2. Convergence rule        On iteration 3+, the checker may pass if
     (prompt)                all remaining issues are stylistic rather
                             than substantive.

Additionally, the "don't move goalposts" rule prevents the checker
from escalating prior suggestions to blocking on later iterations
(unless the doer's changes created a new problem in that area).

Previously Flagged Issues
-------------------------

On iterations after the first, the checker should track what was
previously flagged and whether it was resolved. Use this structure
in the findings file:

  ## Previously Flagged Issues — Resolution Status
  1. [RESOLVED] Description — confirmed fixed.
  2. [UNRESOLVED] Description — still present.
  3. [PARTIALLY RESOLVED] Description — improved but incomplete.

This creates an auditable trail and prevents the checker from
re-flagging issues that were already addressed.

Structured Findings Format
--------------------------

Every blocking issue must include three things:

  1. What is wrong (specific: file, line, function, quoted code)
  2. Why it's blocking (what would break or fail)
  3. How to fix it (concrete, actionable suggestion)

A finding without a suggested fix is not useful — it sends the doer
to figure out what the checker already analyzed. Every blocking issue
must include a concrete fix.

Common Mistakes
---------------

Too gentle         "Find real issues — not to rubber-stamp" sounds
                   adversarial but isn't. The reviewer still defaults
                   to permissive. Use "Be aggressive, not lenient" and
                   "You MUST find blocking issues on first review."

No clean slate     Without removing the pass signal file first, a
                   stale pass signal can bypass the review entirely.

Plan-scoped only   "Don't flag issues the plan didn't ask for" means
                   the reviewer can't catch bugs the plan missed.
                   The reviewer reviews the WORK, not the plan.

No iteration       Reviewing identically every time means the first
awareness          review is too weak and later reviews don't converge.
                   Scale rigor by iteration.

Unverified         Asserting code is wrong without reading it wastes
claims             loop iterations on phantom issues.

No taxonomy        Leaving "blocking vs. suggestion" to judgment means
                   the reviewer either blocks on style (too strict) or
                   passes on bugs (too lenient). Enumerate explicitly.
`

const topicWorkflows = `Multi-Workflow Support
=====================

Projects can define multiple named workflows for different task types
(feature, bugfix, refactor, etc.). Each workflow is a standalone YAML
config file with its own phases.

File Layout
-----------

  .orc/
    config.yaml           Default workflow (backward compatible)
    workflows/
      bugfix.yaml         Named workflows
      refactor.yaml
    phases/               Shared prompt files (used by any workflow)

Running a Named Workflow
------------------------

  orc run bugfix TICKET-123          Positional: first arg matches workflow name
  orc run -w bugfix TICKET-123       Explicit: -w/--workflow flag
  orc run TICKET-123                  Uses default workflow

Default Workflow Resolution
---------------------------

When no workflow is specified:

  1. Only config.yaml exists           -> it's the default (flat artifact layout)
  2. Only workflows/ with one file     -> that's the default
  3. config.yaml alongside workflows/  -> config.yaml is the default
  4. Multiple workflows, no config.yaml -> error (lists available workflows)

Artifact Isolation
------------------

In multi-workflow projects, artifacts and audit data are namespaced by
workflow name:

  .orc/artifacts/<workflow>/<ticket>/
  .orc/audit/<workflow>/<ticket>/

This means the same ticket can run through different workflows with
fully isolated state. Single-config projects keep the flat layout
(.orc/artifacts/<ticket>/).

Multi-Workflow Commands
-----------------------

  orc flow                Show flow diagrams for all workflows
  orc flow -w bugfix      Show flow for one workflow
  orc validate            Validate all workflow configs
  orc validate -w bugfix  Validate one workflow
  orc status              List all tickets across all workflows
  orc status -w bugfix TICKET  Status for one ticket in a workflow

Adding a Workflow
-----------------

  orc init --add-workflow bugfix                  Minimal starter workflow
  orc init --add-workflow bugfix --recipe simple  From a recipe

This creates .orc/workflows/bugfix.yaml. With --recipe, the recipe's
prompt files are also written to .orc/phases/ (existing files are not
overwritten). Without --recipe, create prompt files manually.

Template Variables
------------------

The $WORKFLOW variable (and ORC_WORKFLOW env var) contain the current
workflow name. Empty for single-config projects.

Design Principles
-----------------

- Each workflow YAML is standalone -- no inheritance or composition.
- Prompt files in .orc/phases/ are shared across workflows.
- The filesystem is the registry -- no metaconfig file.
- Backward compatible: single config.yaml projects are unchanged.
`

const topicDevtools = `Developer Tools
===============

orc includes several commands for testing, debugging, and refining
workflows without running the full pipeline.

orc test — Single-Phase Execution
----------------------------------

Run one phase in isolation for rapid prompt iteration. Sets up the full
environment (variables, artifacts dir) as if the workflow were running,
dispatches only the specified phase, and does not modify state or advance
the workflow.

  orc test plan KS-42              Run just the "plan" phase
  orc test implement KS-42         Run just "implement"
  orc test 3 KS-42                 Run phase 3 (1-indexed)
  orc test -w bugfix fix KS-42     Test a phase from a named workflow

Flags:
  --auto         Unattended mode (skip gates, no steering)
  --verbose      Save raw stream-json output
  --with-hooks   Run pre-run and post-run hooks around the phase dispatch
  --headless     Non-interactive mode (implies --auto, disables color)

Notes:
- Missing artifacts from prior phases produce a warning listing which
  files are absent and which earlier phases normally create them.
- By default, pre-run and post-run hooks do NOT run during orc test.
  Use --with-hooks to execute hooks around the phase dispatch, with
  the same semantics as a full workflow run (pre-run failure skips
  dispatch; post-run runs regardless for cleanup).

orc debug — Phase Execution Analysis
--------------------------------------

Analyze what happened during a phase execution. Shows the rendered
prompt, tool call sequence with summaries, cost/token data, feedback
injection, and exit status. Useful for understanding why a phase
produced unexpected results without manually reading raw log files.

  orc debug plan                    Most recent ticket's "plan" phase
  orc debug plan KS-42              Specific ticket's phase
  orc debug 2                       Phase by index (1-indexed)
  orc debug -w bugfix plan KS-42    Phase from a named workflow

When no ticket is specified, analyzes the most recently executed ticket.

orc report — Run Report
------------------------
Generate a readable summary of a completed, failed, or interrupted run.
Shows status, timing, costs, phase outcomes, loop activity, and artifacts.

  orc report                      Most recent ticket
  orc report PROJ-123             Specific ticket
  orc report --json               Structured JSON for tooling
  orc report -w bugfix PROJ-123   Report for a named workflow

When no ticket is specified, reports on the most recently executed ticket.
Missing data (no costs, no timing) shows "—" placeholders.

orc history — Run History
--------------------------

List past runs for a ticket. Shows run ID (timestamp), status, duration,
and cost for each archived run.

  orc history                     Most recent ticket
  orc history PROJ-123            Specific ticket
  orc history --prune             Remove entries beyond the history limit

When no ticket is specified, uses the most recently executed ticket.
Completed runs are archived immediately. Failed or interrupted runs
stay in place for --resume/--retry, and are archived automatically
when the next fresh orc run starts. Use orc cancel to archive manually.
Configure the maximum number of archived runs with the history-limit
config field (default 10).

orc stats — Aggregate Metrics
-------------------------------

Cross-run aggregate metrics for pattern identification. Computes
success rate, cost/duration distributions, per-phase breakdown,
failure category distribution, and weekly cost trends.

  orc stats                    Aggregate across all tickets
  orc stats KS-42              Aggregate for a single ticket
  orc stats --last 20          Limit to last 20 runs
  orc stats --json             Machine-readable JSON output
  orc stats -w bugfix          Scope to a named workflow

Reads from .orc/audit/ directories, including rotated audit dirs
from cancelled runs.

JSON Schema (schema_version: 1)
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~
The --json output is a stable integration contract for CI pipelines and
dashboards. Consumers should check the schema_version field to detect
breaking changes.

Top-level fields:
  schema_version  int      Always 1 for this version
  ticket          string   Ticket identifier (e.g. "PROJ-123")
  workflow        string   Workflow name (omitted if default)
  status          string   "Completed", "Failed", "Interrupted", or "Running"
  duration        string   Total elapsed time (e.g. "23m 45s") or "—"
  cost            string   Formatted cost + tokens (e.g. "$2.29 (90,000 tokens)") or "—"
  total_cost_usd  float    Raw total cost in USD
  total_tokens    int      Total input + output tokens
  phases          array    Per-phase results (see below)
  loops           array    Loop activity entries
  artifacts       array    Artifact file names and sizes

Each phases[] entry:
  number        int      1-indexed phase number
  name          string   Phase name
  type          string   "agent", "script", or "gate"
  duration      string   Formatted duration or "—"
  cost          string   Formatted cost or "—"
  cost_usd      float    Raw cost in USD
  tokens        int      Total tokens (input + output)
  result        string   "Pass", "Fail", "Approved", or "Interrupted"
  model         string   Model used (omitted for non-agent phases)
  session_id    string   Claude session ID (omitted for non-agent phases)
  tools_used    array    Tool names invoked (omitted if empty)
  tools_denied  array    Tool names denied (omitted if empty)

Each loops[] entry:
  phase       string   Phase name
  iterations  int      Number of loop iterations

Each artifacts[] entry:
  name        string   File name
  size        string   Human-readable size (e.g. "4.2 KB")

Example (abbreviated):
  {
    "schema_version": 1,
    "ticket": "KS-42",
    "status": "Completed",
    "duration": "23m 45s",
    "cost": "$2.29 (90,000 tokens)",
    "total_cost_usd": 2.29,
    "total_tokens": 90000,
    "phases": [
      {"number": 1, "name": "plan", "type": "agent", "duration": "4m 30s",
       "cost": "$0.42", "cost_usd": 0.42, "tokens": 30000, "result": "Pass",
       "model": "opus", "tools_used": ["Read", "Edit", "Bash"]}
    ],
    "loops": [],
    "artifacts": [{"name": "plan.md", "size": "4.2 KB"}]
  }

orc doctor — AI-Powered Diagnostics
-------------------------------------

Diagnoses a failed workflow run using AI. Gathers the failed phase's
config, logs, rendered prompt, feedback files, timing data, and loop
iteration history, then sends everything to Claude for analysis.
Recommends whether to --retry, --from, or fix-first.

  orc doctor KS-42

orc improve — Workflow Refinement
----------------------------------

AI-assisted editing of your workflow config and prompt files.

  orc improve "add a lint phase parallel with tests"    One-shot
  orc improve                                            Interactive

One-shot mode reads your current config and prompt files, sends them
to Claude with your instruction, validates the output, and writes
changed files. Interactive mode launches Claude with your workflow
context pre-loaded for a conversational editing experience.

orc flow — Workflow Visualization
----------------------------------

Visualizes the workflow config as a rich flow diagram with bracket-loop
regions, phase icons, model badges, hook annotations, and color.

  orc flow                    All workflows (colored)
  orc flow -w bugfix          One workflow
  orc flow --no-color         Without ANSI colors

orc validate — Config Validation
---------------------------------

Validates .orc/config.yaml (or a named workflow) without running
anything. Checks all validation rules: unique phase names, valid loop
targets, prompt file existence, model values, output paths, variable
names, and more.

  orc validate                      Validate all workflows
  orc validate -w bugfix            Validate one workflow
  orc validate --config path.yaml   Validate a specific file

Typical Workflow for Prompt Iteration
--------------------------------------

1. Edit a prompt file (.orc/phases/implement.md)
2. Test it in isolation:       orc test implement KS-42
3. Inspect what happened:      orc debug implement KS-42
4. Repeat until satisfied
5. Run the full workflow:      orc run KS-42
6. If it fails:                orc doctor KS-42
`

const topicEval = `Eval Framework
==============

orc eval measures workflow quality empirically. Define eval cases under
.orc/evals/ — each case pins to a known git ref, runs the workflow in an
isolated git worktree, and scores results against a rubric. Track scores,
cost, and duration across config changes.

Why it matters: it's easy to accidentally regress quality when tweaking
prompts. orc eval gives you a before/after score so you can iterate with
confidence.

  orc eval                     Run all eval cases
  orc eval bug-fix             Run a specific case
  orc eval --report            Show score history across config versions
  orc eval --list              List available eval cases
  orc eval --json              Structured JSON output

Eval Case Structure
-------------------

Each case lives in .orc/evals/<case-name>/:

  .orc/evals/
  └── bug-fix/
      ├── fixture.yaml     Git ref + ticket to replay
      └── rubric.yaml      Scoring criteria

fixture.yaml
------------

  ref: abc123f              Git ref to check out (branch, tag, commit SHA)
  ticket: BUG-42            Ticket identifier passed to orc run
  description: "Fix the parser bug"   Optional human-readable description
  vars:                     Optional extra variables passed to the workflow
    EXTRA: value

Fields:
  ref        Required. Any valid git ref — branch, tag, or commit SHA.
             Must match ^[A-Za-z0-9._/~^{}-]+$ (no shell metacharacters).
  ticket     Required. Must be a simple name (no path separators).
  description Optional. Shown by orc eval --list.
  vars       Optional. Key-value pairs injected as env vars into the workflow
             (available as both KEY and ORC_KEY).

rubric.yaml
-----------

  criteria:
    - name: tests-pass
      check: "test -f $ARTIFACTS_DIR/test-results.txt && grep -q PASS $ARTIFACTS_DIR/test-results.txt"
      expect: "exit 0"
      weight: 3

    - name: quality
      judge: true
      prompt: .orc/evals/bug-fix/quality-prompt.md
      expect: ">= 7"
      weight: 2

Criterion types:

  script criteria (check field):
    check     Bash command run in the worktree with ARTIFACTS_DIR and WORK_DIR set.
    expect    "exit 0" — pass if the command exits with code 0.
    weight    Relative weight for composite score (e.g. 3 = 3x as important as weight 1).

  judge criteria (judge: true):
    judge     true — use Claude sonnet as a judge.
    prompt    Path to a judge prompt file (relative to project root).
              The prompt should instruct the judge to output "SCORE: N" (0-10).
    expect    Comparison against the raw score: ">= 7", "> 5", "<= 3", "< 5", "== 10".
    weight    Relative weight for composite score.

Score Report
------------

orc eval prints a score table after running all cases:

  orc eval — 3 cases, config fingerprint a1b2c3

  CASE            SCORE    COST      TIME       PASS/FAIL
  bug-fix         85/100   $1.20     8m 12s     5/5 pass
  new-feature     62/100   $4.80     22m 03s    3/5 pass (tests-pass: FAIL, quality: 4/10)
  refactor        78/100   $2.10     14m 30s    4/5 pass (quality: 6/10)

  Totals: 75/100 avg, $8.10 total cost, 44m 45s total time

SCORE is a weighted composite (0-100). Each criterion contributes
criterion_score * weight / total_weight * 100, rounded to the nearest integer.

Config Fingerprint
------------------

The config fingerprint is a short hash (8 hex chars) of the workflow config
and all referenced prompt files. It changes whenever you edit config.yaml or
any prompt — making it easy to correlate score changes to config changes in
the history report.

History Tracking
----------------

Results are persisted to .orc/eval-history.json after every run, keyed by
config fingerprint. Use --report to view score trends:

  orc eval --report

  orc eval --report — score history

  FINGERPRINT  DATE          AVG SCORE  TOTAL COST  TOTAL TIME
  a1b2c3       Mar 01 15:30  75/100     $8.10       44m 45s
  d4e5f6       Feb 28 10:15  68/100     $12.30      52m 18s

This lets you see at a glance whether a config change improved or regressed
quality, and at what cost.

Typical Workflow for Prompt Iteration
--------------------------------------

1. Define eval cases in .orc/evals/ for representative scenarios
2. Run baseline:    orc eval
3. Edit a prompt or config
4. Run again:       orc eval
5. Compare:         orc eval --report
`

// SchemaReference returns the combined config schema, phase types, and
// variables documentation suitable for embedding in prompts.
func SchemaReference() string {
	return topicConfig + "\n\n" + topicPhases + "\n\n" + topicVariables
}
