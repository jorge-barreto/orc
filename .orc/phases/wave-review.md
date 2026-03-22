You are orchestrating a holistic review of all changes made during wave **$TICKET** using an adaptive panel of domain-specific expert subagents.

All children of this wave have been implemented individually, each with its own per-item review. Your job is to catch cross-cutting issues that per-item reviews missed — integration gaps, consistency problems, and systemic quality concerns across the full wave's changeset.

## Step 1: Understand the Wave

1. Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
2. Read the wave epic for context:
   ```bash
   bd show $TICKET
   ```
3. Determine the wave base commit and assess the full changeset:
   ```bash
   WAVE_BASE=$(cat "$ARTIFACTS_DIR/wave-base-commit.txt" 2>/dev/null || echo "main")
   echo "Wave base: $WAVE_BASE"
   git log --oneline $WAVE_BASE..HEAD
   git diff --stat $WAVE_BASE..HEAD
   CHANGED_FILES=$(git diff --name-only $WAVE_BASE..HEAD | wc -l)
   CHANGED_LINES=$(git diff $WAVE_BASE..HEAD --numstat | awk '{s+=$1+$2} END {print s+0}')
   echo "Files: $CHANGED_FILES, Lines: $CHANGED_LINES"
   git diff --name-only $WAVE_BASE..HEAD
   ```

   Use `$WAVE_BASE..HEAD` for all git diffs below (not `main..HEAD`). The wave base was recorded when the first item in this wave was claimed.

Categorize changed files by package:
```bash
RUNNER_FILES=$(git diff --name-only $WAVE_BASE..HEAD | grep -c 'internal/runner/' || echo 0)
DISPATCH_FILES=$(git diff --name-only $WAVE_BASE..HEAD | grep -c 'internal/dispatch/' || echo 0)
STATE_FILES=$(git diff --name-only $WAVE_BASE..HEAD | grep -c 'internal/state/' || echo 0)
CONFIG_FILES=$(git diff --name-only $WAVE_BASE..HEAD | grep -c 'internal/config/' || echo 0)
CMD_FILES=$(git diff --name-only $WAVE_BASE..HEAD | grep -c 'cmd/orc/' || echo 0)
UX_FILES=$(git diff --name-only $WAVE_BASE..HEAD | grep -c 'internal/ux/' || echo 0)
echo "Runner: $RUNNER_FILES, Dispatch: $DISPATCH_FILES, State: $STATE_FILES, Config: $CONFIG_FILES, Cmd: $CMD_FILES, UX: $UX_FILES"
```

## Step 2: Run Verification

```bash
cd $PROJECT_ROOT && make build && go vet ./...
cd $PROJECT_ROOT && make test
```

If tests fail, note the failures — they become automatic BLOCKING findings.

## Step 3: Determine Tier and Select Experts

### Tier Rules

**Tier 1 — Small wave (< 200 lines, < 10 files):**
Launch 3-4 experts: package-relevant domain experts + Test Coverage + Integration.

**Tier 2 — Medium wave (200-1000 lines, 10-30 files):**
Launch 5-6 experts: all affected package experts + Test Coverage + Integration.

**Tier 3 — Large wave (> 1000 lines or > 30 files):**
Launch all 7 experts: full panel.

### Expert Selection Rules

- Runner changes (RUNNER_FILES > 0) → **E1: State Machine & Flow Control**
- Dispatch changes (DISPATCH_FILES > 0) → **E2: Dispatch & Subprocess Management** + **E3: Stream Parsing & Variable Expansion**
- State changes (STATE_FILES > 0) → **E4: State Persistence & Atomicity**
- Config or cmd changes (CONFIG_FILES > 0 or CMD_FILES > 0) → **E5: Config Validation & CLI**
- Any Go files changed → **E6: Test Coverage** (always included)
- **E7: Cross-Item Integration** → ALWAYS included in wave review (this is what distinguishes wave-review from deep-review)
- Tier 3 → launch ALL experts regardless of package detection

**Never launch domain experts (E1-E5) for packages with zero changed files** (unless Tier 3).

## Step 4: Launch Selected Experts

Launch the selected expert subagents **in parallel** using the Agent tool with `model: "opus"`. Each expert writes findings to its designated output file.

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

> You are reviewing Go code changes in the **orc** project's state machine and flow control logic as part of a wave-level review.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {runner files from Step 1, plus any files they import}
>
> For each changed file, read the FULL file (not just the diff), then run `git diff main..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **Loop backward jumps:** When jumping from phase N to phase M, are loop counters for phases [M, N) cleared?
> - **Dispatch count tracking:** Is each phase invocation counted *before* error handling?
> - **PhaseSessionID persistence:** Is the session ID saved *immediately* after dispatch?
> - **Cost limit checks:** Run-level cost checked *before* dispatch; phase-level *after*. Is this asymmetry preserved?
> - **Loop.check timing:** Does check run *after* phase success but *before* min iteration enforcement?
> - **Output re-prompting:** Missing outputs trigger a *single* re-prompt, not a loop.
> - **Feedback clearing:** After a loop succeeds, is feedback cleared?
> - **Parallel phase ordering:** Does `parallel-with` partner have index > current phase?
> - **State.Save() after mutations:** Is every state mutation followed by Save()?
>
> Write findings to `$ARTIFACTS_DIR/reviews/state-machine.md`.
> Only flag issues in code introduced by this wave's diff.

---

### E2: Dispatch & Subprocess Management

**Output:** `$ARTIFACTS_DIR/reviews/dispatch.md`

> You are reviewing Go code changes in **orc**'s phase dispatch and subprocess management.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {dispatch files excluding stream.go and expand.go}
>
> For each changed file, read the FULL file, then run `git diff main..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **CLAUDECODE env stripping:** Does `FilteredEnv`/`BuildEnv` strip CLAUDECODE vars?
> - **Process group cleanup:** Do all dispatchers set `Setpgid: true` and use `syscall.Kill(-pid, syscall.SIGTERM)`?
> - **Resume fallback:** On resume failure, is there a guard against infinite recursion?
> - **Session ID management:** `--session-id` on first turn, `--resume` on continuation?
> - **Permission denial handling:** Denied tools re-prompted in attended mode? Approved tools deduplicated?
> - **User question forwarding:** AskUserQuestion properly forwarded to terminal?
> - **Timeout enforcement:** `context.WithTimeout` used correctly?
>
> Write findings to `$ARTIFACTS_DIR/reviews/dispatch.md`.
> Only flag issues in code introduced by this wave's diff.

---

### E3: Stream Parsing & Variable Expansion

**Output:** `$ARTIFACTS_DIR/reviews/stream-expand.md`

> You are reviewing Go code changes in **orc**'s stream JSON parser and variable expansion system.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {stream.go, expand.go, and related test files}
>
> For each changed file, read the FULL file, then run `git diff main..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **Malformed JSON handling:** Malformed lines silently skipped? Partial lines accumulated?
> - **Usage/cost tracking:** Multiple `result` events accumulate, not overwrite?
> - **Tool use summaries:** Correct field extraction per tool type?
> - **Variable expansion order:** Custom vars expanded in YAML declaration order?
> - **Builtin protection:** Custom vars cannot shadow TICKET, ARTIFACTS_DIR, etc.?
> - **Env fallback:** `os.Expand` falls through to env vars when custom var not found?
>
> Write findings to `$ARTIFACTS_DIR/reviews/stream-expand.md`.
> Only flag issues in code introduced by this wave's diff.

---

### E4: State Persistence & Atomicity

**Output:** `$ARTIFACTS_DIR/reviews/state-persistence.md`

> You are reviewing Go code changes in **orc**'s state persistence layer.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {state package files}
>
> For each changed file, read the FULL file, then run `git diff main..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **Atomic writes:** ALL state file writes use `writeFileAtomic` (write to `.tmp`, fsync, rename)?
> - **Thread-safety in CostData:** `CostData.mu` held for ALL reads and writes?
> - **Thread-safety in Timing:** `TotalElapsed()` safe for concurrent access?
> - **Timing entry matching:** `AddEnd()` backward search for unmatched entry correct?
> - **Loop count persistence:** Keys are phase names (strings)?
> - **Feedback file naming:** Safe with special characters in phase names?
> - **Output path flattening:** `filepath.Base()` collisions handled?
>
> Write findings to `$ARTIFACTS_DIR/reviews/state-persistence.md`.
> Only flag issues in code introduced by this wave's diff.

---

### E5: Config Validation & CLI

**Output:** `$ARTIFACTS_DIR/reviews/config-cli.md`

> You are reviewing Go code changes in **orc**'s config validation and CLI layer.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {config package files + cmd/orc files}
>
> For each changed file, read the FULL file, then run `git diff main..HEAD -- <file>`.
>
> Your domain-specific checklist:
> - **Loop.goto validation:** Must reference an *earlier* phase?
> - **Parallel-with + loop exclusion:** Rejected when both present?
> - **Prompt file existence:** Agent phases verify prompt file on disk?
> - **Ticket pattern anchoring:** Patterns wrapped in `^(?:pattern)$`?
> - **Builtin var override:** Custom vars shadowing builtins rejected?
> - **On-fail deprecation:** Old field rejected with migration hint?
> - **Default cascading:** Top-level defaults cascade to phases correctly?
> - **Documentation surfaces:** User-visible changes reflected in all doc surfaces?
>
> Write findings to `$ARTIFACTS_DIR/reviews/config-cli.md`.
> Only flag issues in code introduced by this wave's diff.

---

### E6: Test Coverage

**Output:** `$ARTIFACTS_DIR/reviews/test-coverage.md`

> You are reviewing Go test coverage for all changes in wave **$TICKET**.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> Changed files: {all changed Go files}
>
> For each changed `.go` file, read the full file and its corresponding `_test.go` file.
>
> Your checklist:
> - Every new exported function has a test?
> - Tests follow existing patterns (table-driven tests, `t.TempDir()`)?
> - Edge cases covered: empty input, nil, zero, missing files, error paths?
> - Tests verify behavior, not just "runs without panic"?
> - Test names descriptive (`TestFunctionName_Scenario`)?
> - For runner tests: `mockDispatcher` used correctly?
> - For config tests: validation error messages checked (not just error presence)?
> - For state tests: atomic write guarantees tested?
> - Are there untested error paths or concurrent code paths in the changed code? (Only flag gaps where the absence of a test could let a crash, data race, or silent wrong result ship — do not flag missing tests for rendering, formatting, or simple deterministic functions.)
>
> Write findings to `$ARTIFACTS_DIR/reviews/test-coverage.md`.
> Only flag issues in test code introduced by this wave's diff.

---

### E7: Cross-Item Integration (wave-review only)

**Output:** `$ARTIFACTS_DIR/reviews/integration.md`

> You are reviewing the full wave changeset for **cross-cutting integration issues** that per-item reviews could not catch.
>
> Read `$PROJECT_ROOT/CLAUDE.md` for project conventions.
>
> This wave implemented multiple work items independently. Your job is to find problems that emerge from the *combination* of changes, not from any single item.
>
> Read ALL changed files in full. Run `git log --oneline main..HEAD` to see the commit history and understand which items were implemented.
>
> Your checklist:
> - **Cross-feature consistency:** Do features implemented in different items work together correctly? Are there conflicting assumptions about shared state, interfaces, or data formats?
> - **Duplicate code across items:** Was similar logic written independently in multiple items? Should it be unified into a shared helper?
> - **Convention drift:** Do changes from different items follow the same patterns? Did one item introduce a new convention that contradicts another item's approach?
> - **API surface cohesion:** If multiple items added/modified CLI commands, flags, or config fields, are they consistent in naming, behavior, and error handling?
> - **Interface compatibility:** If one item changed an interface and another item added an implementation, do they match?
> - **Import graph:** Are there new circular dependencies or unnecessary coupling between packages?
> - **Error message consistency:** Do error messages from different items follow the same format and tone?
> - **Missing integration tests:** Are there scenarios where two features interact that no individual item's tests cover?
> - **Documentation coherence:** Do the combined doc changes tell a consistent story? Are there contradictions between sections updated by different items?
>
> Write findings to `$ARTIFACTS_DIR/reviews/integration.md`.
> Only flag issues in code introduced by this wave's diff.

---

## Step 5: Deduplicate Against Existing Beads

Before writing findings, check what beads already exist so you don't report findings that are already tracked:

```bash
bd list --parent $TICKET --all          # all children of this wave (open and closed)
bd list --type orphan           # search orphan beads too
```

For each candidate finding from the expert reports, check whether an existing bead already covers the same file, same issue, or same area — even if the title or wording differs. Drop any finding that is already tracked. This is critical to prevent duplicate bead creation downstream.

## Step 6: Synthesize Results

After deduplication:

1. Read all report files from `$ARTIFACTS_DIR/reviews/`
2. Compile all BLOCKING findings not already covered by existing beads → **Bugs**
3. Compile all WARNING findings not already covered → evaluate: bugs or improvements?
4. Compile all NOTE findings not already covered → **Improvements**
5. Deduplicate across experts: if 2+ experts flag the same issue, note it as "high confidence"
6. Identify test gaps from E6 and E7 not already covered

## Step 6: Write Findings

Write your findings to `$ARTIFACTS_DIR/wave-review-findings.md`:

```markdown
# Wave Review: $TICKET

**Tier:** {1|2|3}
**Experts launched:** {count} ({comma-separated list of expert names})

## Bugs (must fix — create beads in current wave)

Issues that represent incorrect behavior, crashes, data loss, race conditions, or security problems.

1. **[file:line]** Description of the bug.
   **Flagged by:** {expert name(s)}
   **Impact:** What goes wrong.
   **Suggested fix:** How to fix it.

(If none: "None found.")

## Improvements (future work — create standalone beads)

Issues that represent missed opportunities, tech debt, or enhancements — NOT bugs.

1. **[file:line]** Description of the improvement.
   **Flagged by:** {expert name(s)}
   **Rationale:** Why this would be valuable.

(If none: "None.")

## Test Gaps

Scenarios that should be tested but aren't covered by any existing test.

1. Description of the untested scenario.
   **Flagged by:** {expert name(s)}

## Summary

Overall assessment of the wave's changes: quality, cohesion, remaining risk.
```

## Rules

- **Bugs vs Improvements**: A bug is something that is *wrong* — incorrect behavior, crashes, security issues, data loss, race conditions. An improvement is something that *could be better* — performance, readability, missing features, tech debt. Be precise about this distinction because it determines whether the bead goes into the current wave (bugs) or future work (improvements).
- **Only flag issues in code changed by this wave's diff.** Pre-existing issues are out of scope.
- **Never launch domain experts (E1-E5) for packages with zero changed files** (unless Tier 3).
- **Always launch E7 (Integration)** — this is the wave-review's unique value.
- Be specific — cite file paths and line numbers.
- Every bug must include a suggested fix.
- Launch all selected experts in parallel for speed.
- Focus on cross-cutting issues. Per-file bugs should have been caught by per-item deep-reviews.
