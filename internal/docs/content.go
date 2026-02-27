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
		Summary: "Conditions, parallel execution, on-fail loops, and resuming",
		Content: topicRunner,
	},
	{
		Name:    "artifacts",
		Title:   "Artifacts Directory",
		Summary: "Structure of .orc/artifacts/ and what gets saved",
		Content: topicArtifacts,
	},
}

const topicQuickstart = `Quick Start
===========

1. Initialize a project:

    cd your-project
    orc init

   This creates .orc/config.yaml and .orc/phases/example.md.

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
  orc run <ticket> --retry N    Retry from phase N (1-indexed)
  orc run <ticket> --from N     Start from phase N (1-indexed)
  orc cancel <ticket>           Cancel run and remove all artifacts
  orc cancel <ticket> --force   Cancel even if a run appears active
  orc status                    Show current run status
  orc status <ticket>           Show workflow status for a ticket
  orc init                      Scaffold .orc/ directory
  orc docs                      List documentation topics
  orc docs <topic>              Show a documentation topic

--retry and --from are mutually exclusive. Both reset loop counts.
`

const topicConfig = `Configuration Reference
=======================

Workflows are defined in .orc/config.yaml.

Top-level fields
----------------

  name             string    Required. Project name.
  ticket-pattern   string    Regex for ticket IDs (anchored automatically).
  vars             map       Custom variables expanded at startup (declaration order).
  phases           list      Required. Ordered list of phases.

Phase fields
------------

  name             string    Required. Unique phase name.
  type             string    Required. "script", "agent", or "gate".
  description      string    Human-readable description.
  run              string    Shell command (required for script phases).
  prompt           string    Path to prompt template, relative to project root
                             (required for agent phases).
  model            string    "opus" (default), "sonnet", or "haiku" (agent only).
  timeout          int       Minutes. Default: 30 (agent), 10 (script).
  outputs          list      Expected output filenames in artifacts dir.
  condition        string    Shell command; phase skipped if exit code non-zero.
  parallel-with    string    Name of another phase to run concurrently.
  on-fail          object    Retry loop: goto (phase name) and max (default 2).
  cwd              string    Working directory for this phase (expanded with vars).
                             Not supported on gate phases.

Custom Variables (vars)
-----------------------

The vars field is an ordered key-value map at the top level of config.yaml.
Variables are expanded at startup in declaration order, so later vars can
reference earlier ones. Custom vars cannot override built-in variables
(TICKET, ARTIFACTS_DIR, WORK_DIR, PROJECT_ROOT). Duplicate names are not
allowed.

Validation Rules
----------------

- Phase names must be unique.
- on-fail.goto must reference an earlier phase (no forward jumps).
- parallel-with must reference an existing phase.
- parallel-with and on-fail cannot be combined on the same phase.
- Agent phases require a prompt file that exists on disk.
- Model must be opus, sonnet, haiku, or empty.
- Output filenames must not contain path separators.
- Gate phases cannot have a cwd field.

Example Config
--------------

  name: my-service
  ticket-pattern: '[A-Z]+-\d+'

  vars:
    WORKTREE: $PROJECT_ROOT/.worktrees/$TICKET
    SRC: $WORKTREE/src

  phases:
    - name: setup
      type: script
      description: Create worktree
      run: git worktree add $WORKTREE

    - name: implement
      type: agent
      description: Implement the ticket
      prompt: .orc/phases/implement.md
      model: opus
      cwd: $WORKTREE
      outputs:
        - summary.md
      on-fail:
        goto: implement
        max: 3

    - name: test
      type: script
      description: Run tests
      run: make test
      cwd: $WORKTREE

    - name: review
      type: gate
      description: Human approval
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

  claude -p <prompt> --model <model> --dangerously-skip-permissions

Output is streamed to the terminal and saved to .orc/artifacts/logs/phase-N.log.
The rendered prompt is saved to .orc/artifacts/prompts/phase-N.md.

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
    outputs:
      - design.md
    on-fail:
      goto: plan
      max: 2

gate
----

Prompts the operator for y/n approval at the terminal. If the operator
answers "n", the workflow stops with status "failed".

When --auto is passed, all gate phases are automatically approved and skipped.

Gate phases do not support the cwd field.

Example:

  - name: review
    type: gate
    description: Review implementation before merging
`

const topicVariables = `Template Variables
==================

Variables are expanded in agent prompt templates, script run commands, and
phase cwd fields using $VAR or ${VAR} syntax.

Built-in Variables
------------------

  $TICKET          The ticket identifier passed to orc run.
  $ARTIFACTS_DIR   Absolute path to the .orc/artifacts/ directory.
  $WORK_DIR        Absolute path to the working directory (project root).
  $PROJECT_ROOT    Absolute path to the project root (where .orc/ lives).

If a variable is not in the built-in set or custom vars, os.Expand falls
back to environment variables.

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
  and cwd fields.
- Cannot override built-in variables (TICKET, ARTIFACTS_DIR, WORK_DIR,
  PROJECT_ROOT). Config validation rejects attempts to do so.
- No duplicate variable names allowed.

Environment Variables (ORC_* prefix)
------------------------------------

Child processes (scripts and agents) inherit the parent environment with
these additional ORC_-prefixed variables:

  ORC_TICKET           The ticket identifier.
  ORC_ARTIFACTS_DIR    Absolute path to .orc/artifacts/.
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

Constraints: parallel-with and on-fail cannot be combined on the same
phase.

On-Fail Retry Loops
-------------------

When a phase with on-fail fails:

  1. The failure output is written to .orc/artifacts/feedback/from-<phase>.md.
  2. The loop counter for that phase is incremented.
  3. The runner jumps back to the phase named in on-fail.goto.
  4. Execution resumes from there (the earlier phase can read the
     feedback file).

If the loop counter exceeds on-fail.max (default: 2), the workflow stops.
Loop counts are persisted to .orc/artifacts/loop-counts.json and reset when
using --retry or --from.

The on-fail.goto target must reference an earlier phase (no forward jumps).

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

Signal Handling
---------------

When you press Ctrl+C (SIGINT) or send SIGTERM/SIGHUP:

- The current phase is cancelled via context cancellation.
- State is saved with status "interrupted".
- A resume hint is printed: orc run <ticket>

Resume the workflow later — it picks up from the interrupted phase.

Resuming
--------

orc automatically resumes from the last completed phase. State is saved
to .orc/artifacts/state.json after every phase. To manually control the
resume point:

  orc run TICKET --retry 3    Retry from phase 3 (re-runs phase 3)
  orc run TICKET --from 2     Start from phase 2 (skips phase 1)

Both flags reset loop counts.

Cancelling
----------

To permanently cancel a run and wipe all artifacts:

  orc cancel TICKET

This removes the entire .orc/artifacts/ directory (state, timing, logs,
prompts, feedback, and any declared outputs). The workflow config and
prompt files under .orc/ are not affected.

If state.json shows status "running", cancel refuses by default — press
Ctrl+C in the running terminal first, or pass --force:

  orc cancel TICKET --force
`

const topicArtifacts = `Artifacts Directory
===================

orc creates a .orc/artifacts/ directory in the project root to store all
run data. This directory is the primary mechanism for passing context
between phases — phases read and write files here rather than relying
on conversational memory.

Directory Structure
-------------------

  .orc/artifacts/
  ├── state.json              Current run state
  ├── timing.json             Start/end timestamps per phase
  ├── loop-counts.json        On-fail retry counters per phase
  ├── prompts/
  │   ├── phase-1.md          Rendered prompt for phase 1
  │   ├── phase-2.md          Rendered prompt for phase 2
  │   └── ...
  ├── logs/
  │   ├── phase-1.log         Agent output for phase 1
  │   ├── phase-2.log         Agent output for phase 2
  │   └── ...
  └── feedback/
      └── from-<phase>.md     Error output from failed phase (on-fail loops)

state.json
----------

Tracks the current phase index, ticket identifier, and workflow status
(running, completed, failed, interrupted). Written atomically after every
phase.

timing.json
-----------

Records start and end timestamps for each phase. Useful for observability
and performance analysis.

loop-counts.json
----------------

Tracks how many times each phase has looped via on-fail. Reset when using
--retry or --from flags.

prompts/
--------

Rendered prompts for agent phases (after variable expansion). Saved as
phase-N.md where N is the 1-indexed phase number. Useful for debugging
what the agent actually received.

logs/
-----

Raw output from agent phases, saved as phase-N.log. Contains the full
agent response.

feedback/
---------

When a phase with on-fail fails, its output is written to
feedback/from-<phase-name>.md. The phase that on-fail.goto points to
can reference this file in its prompt to provide error context for the
retry attempt.

Declared Outputs
----------------

Phases can declare expected output files via the outputs field. These
files are expected to appear directly in the .orc/artifacts/ directory (not
in subdirectories). Output filenames must not contain path separators.
`

// SchemaReference returns the combined config schema, phase types, and
// variables documentation suitable for embedding in prompts.
func SchemaReference() string {
	return topicConfig + "\n\n" + topicPhases + "\n\n" + topicVariables
}
