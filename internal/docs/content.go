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
}

const topicQuickstart = `Quick Start
===========

1. Initialize a project:

    cd your-project
    orc init

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
  orc cancel <ticket>           Cancel run and remove all artifacts
  orc cancel <ticket> --force   Cancel even if a run appears active
  orc status <ticket>           Show workflow status for a ticket
  orc doctor <ticket>           Diagnose a failed run using AI
  orc init                      Initialize .orc/ directory (AI-powered)
  orc docs                      List documentation topics
  orc docs <topic>              Show a documentation topic
  orc improve "..."             Apply a specific change to the workflow
  orc improve                   Interactive AI-assisted workflow refinement

--retry and --from accept a 1-indexed phase number or a phase name.
They are mutually exclusive. Both reset loop counts.
`

const topicConfig = `Configuration Reference
=======================

Workflows are defined in .orc/config.yaml.

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
  vars                map       Custom variables expanded at startup (declaration order).
  phases              list      Required. Ordered list of phases.

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
- loop.goto must reference an earlier phase (no forward jumps).
- loop.max is required and means total iterations (not retries).
- loop.on-exhaust.goto must reference an earlier phase.
- parallel-with must reference an existing phase.
- parallel-with and loop cannot be combined on the same phase.
- Agent phases require a prompt file that exists on disk.
- Model must be opus, sonnet, haiku, or empty.
- Output filenames must not contain path separators.
- Gate phases cannot have a cwd field.

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

gate
----

Prompts the operator for approval at the terminal. The operator can type
"y" to continue, or any other text to request a revision — the text is
captured as feedback in the phase log and the workflow stops.

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
  $ARTIFACTS_DIR   Absolute path to the .orc/artifacts/<ticket>/ directory.
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
command runs with full variable expansion ($ARTIFACTS_DIR, custom vars,
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
and reset when using --retry or --from.

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

Cancelling
----------

To permanently cancel a run and wipe all artifacts:

  orc cancel TICKET

This removes the .orc/artifacts/<ticket>/ directory for that ticket (state, timing, logs,
prompts, feedback, and any declared outputs). The workflow config and
prompt files under .orc/ are not affected.

If state.json shows status "running", cancel refuses by default — press
Ctrl+C in the running terminal first, or pass --force:

  orc cancel TICKET --force
`

const topicArtifacts = `Artifacts Directory
===================

orc creates a .orc/artifacts/<ticket>/ directory in the project root to store all
run data. This directory is the primary mechanism for passing context
between phases — phases read and write files here rather than relying
on conversational memory.

Directory Structure
-------------------

  .orc/artifacts/<ticket>/
  ├── state.json              Current run state
  ├── timing.json             Start/end timestamps per phase
  ├── loop-counts.json        Loop iteration counters per phase
  ├── prompts/
  │   ├── phase-1.md          Rendered prompt for phase 1
  │   ├── phase-2.md          Rendered prompt for phase 2
  │   └── ...
  ├── logs/
  │   ├── phase-1.log         Agent output for phase 1
  │   ├── phase-2.log         Agent output for phase 2
  │   └── ...
  └── feedback/
      └── from-<phase>.md     Output from failed or looped phase

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

Declared Outputs
----------------

Phases can declare expected output files via the outputs field. These
files are expected to appear directly in the .orc/artifacts/<ticket>/ directory (not
in subdirectories). Output filenames must not contain path separators.
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

// SchemaReference returns the combined config schema, phase types, and
// variables documentation suitable for embedding in prompts.
func SchemaReference() string {
	return topicConfig + "\n\n" + topicPhases + "\n\n" + topicVariables
}
