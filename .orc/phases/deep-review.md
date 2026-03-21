You are orchestrating a thorough final review for **orc** using an adaptive panel of domain-specific expert subagents.

Your job is to catch issues that the per-bead quick reviews missed — correctness problems, edge cases, test gaps, convention violations, and scope creep across the full changeset for this work item.

## Step 0: Determine Context

```bash
rm -f "$ARTIFACTS_DIR/deep-review-pass.txt"
WORK_ITEM=$(cat "$ARTIFACTS_DIR/current-ticket.txt" 2>/dev/null || echo "$TICKET")
BASE=$(cat "$ARTIFACTS_DIR/base-commit.txt" 2>/dev/null || echo "main")
echo "Reviewing: $WORK_ITEM (base: $BASE)"
```

Use `$BASE..HEAD` for all git diffs below (not `main..HEAD`). The base commit was recorded when work started on this item.

## Step 1: Assess Change Scope

```bash
CHANGED_FILES=$(git diff --name-only $BASE..HEAD | wc -l)
CHANGED_LINES=$(git diff $BASE..HEAD --numstat | awk '{s+=$1+$2} END {print s+0}')
echo "Files: $CHANGED_FILES, Lines: $CHANGED_LINES"
git diff --name-only $BASE..HEAD
```

Categorize changed files by package:
```bash
RUNNER_FILES=$(git diff --name-only $BASE..HEAD | grep -c 'internal/runner/' || echo 0)
DISPATCH_FILES=$(git diff --name-only $BASE..HEAD | grep -c 'internal/dispatch/' || echo 0)
STATE_FILES=$(git diff --name-only $BASE..HEAD | grep -c 'internal/state/' || echo 0)
CONFIG_FILES=$(git diff --name-only $BASE..HEAD | grep -c 'internal/config/' || echo 0)
CMD_FILES=$(git diff --name-only $BASE..HEAD | grep -c 'cmd/orc/' || echo 0)
UX_FILES=$(git diff --name-only $BASE..HEAD | grep -c 'internal/ux/' || echo 0)
echo "Runner: $RUNNER_FILES, Dispatch: $DISPATCH_FILES, State: $STATE_FILES, Config: $CONFIG_FILES, Cmd: $CMD_FILES, UX: $UX_FILES"
```

Read `$ARTIFACTS_DIR/plan.md` for the overall plan context.

## Step 2: Determine Tier and Select Experts

### Tier Rules

**Tier 1 — Small change (< 100 lines, < 5 files):**
Launch 2-3 experts: only the package-relevant domain experts + Test Coverage.

**Tier 2 — Medium change (100-500 lines, 5-15 files):**
Launch 3-5 experts: all affected package experts + Test Coverage.

**Tier 3 — Large change (> 500 lines or > 15 files):**
Launch all 6 experts: full panel.

### Expert Selection Rules

- Runner changes (RUNNER_FILES > 0) → **E1: State Machine & Flow Control**
- Dispatch changes (DISPATCH_FILES > 0) → **E2: Dispatch & Subprocess Management** + **E3: Stream Parsing & Variable Expansion**
- State changes (STATE_FILES > 0) → **E4: State Persistence & Atomicity**
- Config or cmd changes (CONFIG_FILES > 0 or CMD_FILES > 0) → **E5: Config Validation & CLI**
- Any Go files changed → **E6: Test Coverage** (always included)
- Tier 3 → launch ALL experts regardless of package detection

**Never launch experts for packages with zero changed files** (unless Tier 3).

## Step 3: Launch Selected Experts

Launch the selected expert subagents **in parallel** using the Agent tool with `model: "opus"`. Each expert receives the changed files list and writes findings to its designated output file.

Create the reviews directory first:
```bash
mkdir -p "$ARTIFACTS_DIR/reviews"
```

### Finding Format (all experts)

```
# {Expert Name} Review

## BLOCKING
- [file:line] Description. Why it must be fixed.

## WARNING
- [file:line] Description. Not blocking but should be considered.

## NOTE
- [file:line] Informational observation.
```

If no findings in a category, write `None.`

---

### E1: State Machine & Flow Control

**Output:** `$ARTIFACTS_DIR/reviews/state-machine.md`

> You are reviewing Go code changes in the **orc** project's state machine and flow control logic.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {runner files from Step 1, plus any files they import}
>
> For each changed file, read the full file, then run `git diff $BASE..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **Loop backward jumps:** When jumping from phase N to phase M, are loop counters for phases [M, N) cleared? Missing this causes incorrect min iteration enforcement on re-entry.
> - **Dispatch count tracking:** Is each phase invocation counted *before* error handling? Counts must increase monotonically for audit accuracy.
> - **PhaseSessionID persistence:** Is the session ID saved *immediately* after dispatch? If the process dies during error handling, the session ID is lost and --resume can't recover.
> - **Cost limit checks:** Run-level cost is checked *before* phase dispatch; phase-level cost is checked *after*. Is this asymmetry preserved?
> - **Loop.check timing:** Does the check command run *after* phase success but *before* min iteration enforcement?
> - **Output re-prompting:** Missing outputs trigger a *single* re-prompt (not a loop). Does the code limit to one retry?
> - **Feedback clearing:** After a loop succeeds, is feedback cleared so downstream phases don't see stale feedback?
> - **Parallel phase ordering:** Does `parallel-with` partner have index > current phase? Backward reference means partner was already executed.
> - **Condition evaluation:** Are conditions checked *before* parallel setup? If a parallel partner's condition fails, is the semantics clear?
> - **State.Save() after mutations:** Is every state mutation followed by Save() before continuing (defensive against crashes)?
>
> Write findings to `$ARTIFACTS_DIR/reviews/state-machine.md`.
> Only flag issues in code introduced by this diff.

---

### E2: Dispatch & Subprocess Management

**Output:** `$ARTIFACTS_DIR/reviews/dispatch.md`

> You are reviewing Go code changes in **orc**'s phase dispatch and subprocess management.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {dispatch files excluding stream.go and expand.go}
>
> For each changed file, read the full file, then run `git diff $BASE..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **CLAUDECODE env stripping:** Does `FilteredEnv`/`BuildEnv` strip CLAUDECODE vars? Leaking this causes `claude -p` to refuse to run (nesting violation).
> - **Process group cleanup:** Do all dispatchers set `Setpgid: true` and use `syscall.Kill(-pid, syscall.SIGTERM)` to kill process groups? Missing this causes zombie processes on timeout.
> - **Resume fallback:** On resume failure, does `RunAgent` recursively call itself with a fresh sessionID? Is there a guard against infinite recursion if the failure is permanent?
> - **Session ID management:** Is `--session-id` used on first turn and `--resume` on continuation? Mixing these up breaks agent continuity.
> - **Permission denial handling:** Do denied tools get re-prompted in attended mode? Are approved tools added to `extraTools` correctly without duplicates?
> - **User question forwarding:** Are user questions from `AskUserQuestion` tool use properly forwarded to the terminal and responses sent back?
> - **Timeout enforcement:** Is `context.WithTimeout` used correctly? Does the phase timeout cover the full execution including retries?
> - **Error wrapping:** Are all errors wrapped with `%w` for error chains?
>
> Write findings to `$ARTIFACTS_DIR/reviews/dispatch.md`.
> Only flag issues in code introduced by this diff.

---

### E3: Stream Parsing & Variable Expansion

**Output:** `$ARTIFACTS_DIR/reviews/stream-expand.md`

> You are reviewing Go code changes in **orc**'s stream JSON parser and variable expansion system.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {stream.go, expand.go, and related test files}
>
> For each changed file, read the full file, then run `git diff $BASE..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **Malformed JSON handling:** Are malformed JSON lines silently skipped without crashing the parser? Partial JSON lines should be accumulated, not discarded.
> - **Usage/cost tracking:** Does the parser correctly handle multiple `result` events? Usage should accumulate, not overwrite unconditionally.
> - **Tool use summaries:** Does `toolUseSummary` extract the right field for each known tool (e.g., "command" for Bash, "pattern" for Grep)? Unknown tools should show a sensible fallback.
> - **Variable expansion order:** Are custom vars expanded in YAML declaration order, with each expansion seeing prior vars' values? This is critical for cross-referencing vars.
> - **Builtin protection:** Can custom vars shadow builtins (TICKET, ARTIFACTS_DIR, PROJECT_ROOT, etc.)? They must not.
> - **Env fallback:** Does `os.Expand` fall through to environment variables when a custom var is not found? Is this intentional and documented?
> - **Empty string on missing:** Does a missing variable expand to empty string silently? Should it error or warn?
> - **Prompt rendering:** Is the expanded prompt written to the audit dir for debugging? Is the raw template also preserved?
>
> Write findings to `$ARTIFACTS_DIR/reviews/stream-expand.md`.
> Only flag issues in code introduced by this diff.

---

### E4: State Persistence & Atomicity

**Output:** `$ARTIFACTS_DIR/reviews/state-persistence.md`

> You are reviewing Go code changes in **orc**'s state persistence layer.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {state package files}
>
> For each changed file, read the full file, then run `git diff $BASE..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **Atomic writes:** Do ALL state file writes use the `writeFileAtomic` pattern (write to `.tmp`, fsync, rename)? A crash mid-write must never corrupt state.
> - **Thread-safety in CostData:** Is `CostData.mu` held for ALL reads and writes to cost data? Parallel phases append costs concurrently.
> - **Thread-safety in Timing:** Does `TotalElapsed()` read `Timing.Entries` without holding a mutex? If entries can be added concurrently, this is a data race.
> - **Timing entry matching:** Does `AddEnd()` search backward for the *first unmatched* entry with the phase name? If a phase runs twice, could it close the wrong entry?
> - **Loop count persistence:** Are `loop-counts.json` keys phase names (strings)? Renaming phases between runs would orphan old counts.
> - **Feedback file naming:** Do feedback filenames use `from-<phase>.md`? Phase names with special characters could corrupt the path.
> - **Output path flattening:** Does `AuditOutputPath` use `filepath.Base()`? Output files with identical basenames from different phases would overwrite each other.
> - **Missing directory handling:** Does `ReadAllFeedback()` handle a missing feedback dir gracefully (return empty, not error)?
> - **JSON formatting:** Do all JSON files use `MarshalIndent` for readability?
>
> Write findings to `$ARTIFACTS_DIR/reviews/state-persistence.md`.
> Only flag issues in code introduced by this diff.

---

### E5: Config Validation & CLI

**Output:** `$ARTIFACTS_DIR/reviews/config-cli.md`

> You are reviewing Go code changes in **orc**'s config validation and CLI layer.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {config package files + cmd/orc files}
>
> For each changed file, read the full file, then run `git diff $BASE..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **Loop.goto validation:** Does `loop.goto` always reference an *earlier* phase? Forward or self-references create infinite loops.
> - **Parallel-with + loop exclusion:** Are `parallel-with` and `loop` rejected when both present on the same phase?
> - **Prompt file existence:** Do agent phases verify their prompt file exists on disk during validation?
> - **Ticket pattern anchoring:** Are patterns wrapped in `^(?:pattern)$` for full-match semantics? Unanchored patterns allow injection.
> - **Builtin var override:** Can custom vars shadow TICKET, ARTIFACTS_DIR, etc.? They must be rejected.
> - **Variable name format:** Must match `^[A-Za-z_][A-Za-z0-9_]*$`. Hyphens or spaces break shell substitution.
> - **Phase name ambiguity:** If a phase is named "1", could it be confused with index 1? How is disambiguation handled?
> - **Ticket path validation:** Are path separators and `..` sequences rejected in ticket names?
> - **On-fail deprecation:** Is the old `on-fail` field rejected with a migration hint to `loop`?
> - **Default cascading:** Do top-level defaults (model, cwd, effort) correctly cascade to phases that don't override them?
> - **Documentation surfaces:** If the change affects user-visible behavior, are all doc surfaces updated (README, orc docs, CLI help, scaffold)?
>
> Write findings to `$ARTIFACTS_DIR/reviews/config-cli.md`.
> Only flag issues in code introduced by this diff.

---

### E6: Test Coverage

**Output:** `$ARTIFACTS_DIR/reviews/test-coverage.md`

> You are reviewing Go test coverage for changes in the **orc** project.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {all changed Go files}
>
> For each changed `.go` file, read the full file and its corresponding `_test.go` file.
>
> Your checklist:
> - Every new exported function has a test?
> - Tests follow existing patterns in the package's `_test.go` files (table-driven tests, `t.TempDir()` for temp dirs)?
> - Edge cases covered: empty input, nil, zero, missing files, error paths?
> - Tests verify behavior, not just "runs without panic"?
> - Test names are descriptive and follow Go conventions (`TestFunctionName_Scenario`)?
> - Are there untested error paths or concurrent code paths in the changed code? (Only flag gaps where the absence of a test could let a crash, data race, or silent wrong result ship — do not flag missing tests for rendering, formatting, or simple deterministic functions.)
> - For runner tests: does the test use `mockDispatcher` correctly?
> - For config tests: are validation error messages checked (not just error presence)?
> - For state tests: are atomic write guarantees tested (crash simulation)?
>
> Write findings to `$ARTIFACTS_DIR/reviews/test-coverage.md`.
> Only flag issues in test code introduced by this diff.

---

## Step 4: Synthesize Results

After all subagents complete:

1. Read all report files from `$ARTIFACTS_DIR/reviews/`
2. Compile all BLOCKING findings
3. Compile all WARNING findings
4. Deduplicate: if 2+ experts flag the same issue, note it as "high confidence"

## Step 5: Acceptance Criteria Check

Read `$ARTIFACTS_DIR/plan.md` and check each acceptance criterion:
- Can it be verified by reading the code or running a command?
- Is the criterion met?

## Step 6: Write Verdict

Write your full review to `$ARTIFACTS_DIR/review-findings.md`:

```markdown
# Deep Review: $WORK_ITEM

**Tier:** {1|2|3}
**Experts launched:** {count} ({comma-separated list of expert names})

## Blocking Issues
{numbered list with [file:line] and flagging expert, or "None."}

## Warnings
{numbered list with [file:line] and flagging expert, or "None."}

## Acceptance Criteria Check

- [x] Criterion 1 — verified by: how you verified
- [ ] Criterion 2 — NOT MET: explanation

## Verdict

**PASS** or **FAIL**
```

## Step 7: Pass/Fail Decision

**If zero blocking issues AND all acceptance criteria met:**
```bash
echo "PASS" > "$ARTIFACTS_DIR/deep-review-pass.txt"
```

**If any blocking issues exist:**
Do NOT create `deep-review-pass.txt`. The blocking issues in `review-findings.md` are sufficient — the orchestrator will loop back to plan.

## Rules

- **Only flag issues introduced by this ticket's diff.** Pre-existing issues are out of scope.
- **Never launch experts for packages with zero changed files** (unless Tier 3).
- Be specific. Cite file paths and line numbers.
- Every blocking issue needs a suggested fix.
- Launch all selected experts in parallel for speed.
