# orc eval redesign — held-out grader, clean stage, separable grading

**Date:** 2026-06-23
**Status:** Design — pending implementation plan
**Related issues:** #8 (re-grade without re-run), #9 (no agent-input/grader separation; check var access; injected-prompt dirty tree; var-in-var expansion)
**Supersedes the mechanism of:** R-034 (`orc-9zj.4`), `internal/eval/`

## Problem

`orc eval` was built (R-034) as a workflow quality-regression tracker: change a
prompt, re-run, see whether the score moved and at what cost. The measurement is
only trustworthy if the grader (tests, rubric) is **held out** from the workflow
under test — otherwise the score measures "can the agent see and satisfy the
test," not "did the workflow produce good work." Running held-out tests (plus a
soft agent-as-judge fallback) is the only real way to measure quality. So
held-out separation is not a new use case bolted onto eval; it is the missing
piece that makes the *original* regression-tracking goal honest.

The current implementation collapses four distinct concerns into a single
`git checkout <ref>`:

1. the **workflow under test** (should be live),
2. the **target codebase** (should be pinned to `ref`),
3. **agent input** — the ticket/spec the agent must read,
4. the **held-out grader** — rubric + tests the agent must NOT see.

Because `.orc/evals/<case>/` lives in the repo, it is part of the `ref`
checkout, so no choice of `ref` can both expose the spec and hide the grader.
The current code also *injects live prompt files over the ref checkout*
(`copyOrcDir` in `internal/eval/eval.go`), which dirties the worktree and trips
the very common "abort if the tree is dirty" workflow guard.

### Concrete gaps this design closes

- **#9 P1** — no separation of agent-visible input vs held-out grader.
- **#9 P2** — rubric `check:` scripts can't read fixture `vars`
  (`internal/eval/rubric.go` builds the check env without them).
- **#9 P3** — injected live prompts (and untracked `artifacts/`/`audit/` dirs)
  make `git status` dirty inside the worktree; clean-tree guards trip on orc's
  own scaffolding.
- **#9 P4** — fixture `vars` injected into child env are passed literally, so
  `TICKET_FILE: $LIVE_ROOT/x` arrives unexpanded.
- **#8** — no way to re-grade a finished run against an edited rubric without
  paying a full workflow execution.

## Key insight / design philosophy

Two failed directions were considered and rejected during design, and the
rejections define the principle:

- **"Subtract the grader from the checkout at runtime"** dirties the tree (the
  reintroduction of #9 P3). Dead.
- **"Build a smart commit that reconstructs a realistic repo state / synthetic
  repo"** is undesignable: it makes orc compete with every project's git
  conventions and lose on the long tail. Dead.

The principle that survives: **orc should do *less* and never try to be smart
about the repo.** orc applies only edits it *already knows exactly* — its own
live config, its own removal of its own `.orc/evals/` dir, and the case's own
spec file. It never infers repo conventions.

The resolution of the central contention (the grader must be both
version-controlled in the repo *and* invisible to the agent) is **temporal, not
spatial**: the grader lives in `.orc/evals/` in real git history (shared with
the team), but is *physically absent from the agent's stage* and read by orc
directly from the live project only at grade time.

Likewise, ticket-fetching is the **workflow's** job, not orc's. orc must not
invent a "ticket source" abstraction. It provides two dumb primitives — a spec
file at a known path, and an eval-mode flag — and the workflow opts in with one
conditional in its ticket-fetch phase. This is the only generalizable contract;
how each project resolves a real ticket (Jira, etc.) is deliberately out of
orc's hands.

## Architecture

`orc eval` becomes a **stage builder + separable grader**:

```
                  ┌─────────────────────── live project ───────────────────────┐
                  │  .orc/<workflow files>   .orc/evals/<case>/{fixture,spec,    │
                  │  (live, under test)        rubric, held-out tests}           │
                  └───────────────┬───────────────────────────┬─────────────────┘
                                  │ (1) build stage            │ (3) grade reads
                                  ▼                            │     grader from
   git worktree add <ref>  ──►  curate + commit               │     LIVE project
   (pinned target)              · copy spec → $ORC_SPEC_FILE   │     (never the
                                · rm -rf .orc/evals/           │      worktree)
                                · write live workflow files    │
                                · git add -A && git commit     │
                                  ▼                            │
                        CLEAN, GRADER-FREE STAGE               │
                                  │ (2) run workflow           │
                                  ▼                            │
                              artifacts ─────────────────────► grade step
                                  │                            (artifacts + grader)
                              saved for re-grade ◄─────────────────────┘ (#8)
```

### Components

- **Case dir `.orc/evals/<case>/`** — version-controlled, shared with the team:
  - `fixture.yaml` — `ref`, `ticket`, `vars`, `description`, **new required
    `spec:`** field naming the agent-visible spec file (relative to the case
    dir). Required (hard migration — see Open Questions #4).
  - the **spec** file (e.g. `spec.md`) — the only thing extracted onto the stage.
  - `rubric.yaml` + any held-out test scripts / judge prompt files — held out by
    virtue of the whole `.orc/evals/` dir being removed from the stage. **There
    is no per-file holdout rule; the rule is simply ".orc/evals/ is not on the
    stage."**

- **Stage builder** (replaces `CreateWorktree` + `copyOrcDir`):
  1. `git worktree add <tmp> <ref>` — vanilla checkout of the pinned target.
  2. Copy the case spec from the live project to `$ORC_SPEC_FILE` inside the
     stage (a known path; see Open Questions for exact path).
  3. `rm -rf <stage>/.orc/evals/` — `.orc` is always at repo root, so this is
     unambiguous and requires no repo intelligence.
  4. Write the **live** workflow/phase/prompt files into the stage (the thing
     under test), overwriting the ref's committed copies.
  5. `git add -A && git commit -m "orc eval stage: <case>"` on the worktree's
     branch.
  - Result: `git status` is empty (clean-tree guards pass — **#9 P3**), the
    grader/evals dir is absent (**#9 P1**), the live workflow runs against the
    pinned target.
  - `.orc/artifacts/` and `.orc/audit/` are produced *during* the run; they must
    be `.gitignore`d on the stage (or otherwise excluded) so they don't re-dirty
    the tree mid-run. (See Open Questions.)

- **Eval-mode contract** (the spec-injection seam — **#9 P1 / Q1**):
  - orc sets **two primitives** in the workflow's environment:
    - `ORC_EVAL=1` — the eval-mode flag the workflow branches on.
    - `ORC_SPEC_FILE=<abs path in stage>` — absolute path to a file on the stage
      containing this case's spec (the "ticket body"). Its contents are the
      case's authored `spec.md`, copied onto the stage during stage build (the
      one file extracted from the case dir before `.orc/evals/` is removed). It
      is a *plain file path*, not a ticket abstraction — orc deliberately does
      not model ticketing.
  - The workflow's ticket-fetch phase is expected to branch:
    `if [ -n "$ORC_EVAL" ]; then cat "$ORC_SPEC_FILE"; else <fetch from Jira>; fi`.
  - orc owns the two primitives; the workflow owns ticketing. This leak of "this
    is an eval" into the workflow is intentional and minimal (one conditional in
    one phase). **It is opt-in, not required.** The branch is a QOL convenience
    only for workflows that resolve tickets via an *external* store (Jira, etc.)
    and thus need a local stand-in under eval. A workflow whose ticket is already
    self-contained needs no branch at all. orc neither enforces nor needs the
    opt-in; a workflow that hits an external store with a fake eval ticket id and
    omits the branch will simply fail — that is the author's choice to make.

- **Grade step** (extracted from the run path; this is what makes #8 possible):
  - A pure function of `(run artifacts, grader)`.
  - The grader (rubric + checks + judge prompts) is read from the **live
    project**, never from the worktree.
  - `check:` scripts and judge invocations receive the fixture `vars` (**#9
    P2**), expanded so var-in-var references resolve (**#9 P4**).
  - Path-resolution base is documented and consistent (the prior surprise of
    judge `prompt:` resolving from project root while `check:` relative paths
    resolved from the worktree is removed / documented — see Open Questions).

- **Re-grade** (**#8**):
  - `orc eval <case> --regrade [<run-id>]` runs only the grade step against a
    saved run's artifacts; default targets the most recent run for the case.
  - Writes a new history row so `--report` shows the score delta.
  - Requires run artifacts to be retained/locatable after a run (the existing
    history-archive logic in `RunWorkflow` already moves artifacts to
    `history/<run-id>/`; re-grade reads from there).

- **History & fingerprints** (**two fingerprints**):
  - Each history row records a **workflow fingerprint** (config + prompt/run
    files, as today) AND a separate **rubric fingerprint** (rubric.yaml + held-out
    test files + judge prompts).
  - This lets `--report` distinguish "score moved because the workflow changed"
    (workflow-fp changed) from "score moved because the ruler changed"
    (rubric-fp changed) — and makes a re-grade legible as same workflow-fp / new
    rubric-fp.

### Data flow

1. `orc eval <case>` → load fixture + rubric from live project.
2. Build stage (worktree at `ref` + curation commit).
3. Run workflow on stage with `ORC_EVAL` / `ORC_SPEC_FILE` set; collect artifacts,
   cost, duration; save artifacts under a run id.
4. Grade: evaluate rubric (read from live project) against artifacts; checks/judge
   get fixture `vars`.
5. Compute score; append history row (workflow-fp + rubric-fp); render report.
6. `orc eval <case> --regrade [run-id]` repeats only steps 4–5 against saved
   artifacts.

### Error handling

- Stage build failures (worktree add, curation commit) abort the case with a
  wrapped error; the worktree is pruned/removed (existing teardown logic).
- A missing or absent `spec:` field/file in the fixture is a config error at
  load time (hard migration — `spec:` is required).
- A workflow that does not honor the eval-mode contract (never reads
  `$ORC_SPEC_FILE`) is not orc's failure to detect — the contract is opt-in QOL,
  not required. Document it clearly. (Optional future: a doctor check that warns
  when a workflow fetches tickets externally but has no eval branch.)
- Grade-step failures (bad check, judge error) are recorded per-criterion as
  today (fail with detail), never crashing the whole eval.
- `--regrade` with no saved run for the case is a clear error listing available
  run ids.

### Testing

- Stage builder: assert the stage is git-clean after build, `.orc/evals/` is
  absent, live workflow files are present, spec is at `$ORC_SPEC_FILE`.
- Eval-mode contract: a fixture workflow whose ticket-fetch phase reads
  `$ORC_SPEC_FILE` produces artifacts derived from the spec.
- Grade step purity: same artifacts + same rubric → same score; editing the
  rubric and re-grading changes the score without re-running.
- `vars` reach checks (**#9 P2**) and expand (**#9 P4**) — unit + integration.
- Two-fingerprint history: editing only the rubric changes rubric-fp not
  workflow-fp; `--report` renders both.
- `--regrade` targets latest run by default and a named run by id.

## What stays the same

- Case discovery (`.orc/evals/<case>/`), `--list`, `--report`, `--json`.
- Judge mechanism (`claude -p`, `SCORE: N` extraction, normalization, `expect`
  operators), script `check`/`expect` semantics, weighted composite scoring.
- Cost/duration as first-class outputs.
- Worktree isolation and process-group teardown.

## Open questions (to resolve in the implementation plan)

1. **Exact `$ORC_SPEC_FILE` path on the stage.** Leaning to a dedicated,
   stable, orc-owned path: `.orc/eval-spec/spec.md`. Rejected `.orc/artifacts/`
   (per-run, orc-churned — a poor home for a stable input). Confirm the exact
   path and whether it should be fixture-overridable.
2. **Excluding `artifacts/`/`audit/` from dirtying the stage mid-run.** Append to
   `.gitignore` in the curation commit, vs. a documented expectation, vs.
   writing artifacts outside the worktree.
3. **Path-resolution base for grader `check:`/`prompt:`.** Pick one consistent
   base now that the grader is read from the live project; document it.
4. ~~**Backward compatibility.**~~ **Resolved: hard migration.** `spec:` is
   required; there is no fallback to "spec lives in the ref." The feature has no
   real-world users yet, so carrying optionality isn't worth it. Existing cases
   (if any) must add a `spec:` field. A clear config-load error names the missing
   field.
5. **Re-grade history semantics.** Does a re-grade row replace or append? (Design
   leans append, so deltas are visible.) How is a re-grade row marked in
   `--report`?
6. **Adversarial reachability (accepted as out of scope for now).** The grader
   remains reachable via `git show <ref>:.orc/evals/...` because `ref` is an
   ancestor of the curation commit. Accepted: the threat model is accidental
   context-pollution, not a cheating agent. Revisit only if that changes.
