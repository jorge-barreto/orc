# Version-file-driven auto-tagging — Design

**Bead:** orc-29y.7 (Distribution & Packaging)

**Goal:** Cutting a release becomes "bump `VERSION`, merge to main." CI creates
and pushes the matching `v*` tag, which runs the existing release pipeline. The
manual `git tag && push` path keeps working unchanged as an escape hatch.

## Problem

Today releases are manual-tag-only. Pushing a `v*` git tag fires
`.github/workflows/release.yml` → GoReleaser. There is no version file and no CI
step that creates a tag; merging to main does nothing release-wise. The release
version lives only in git tags, so cutting a release means remembering the exact
`git tag -a vX.Y.Z -m "..." && git push origin vX.Y.Z` dance.

We want a `merge → release` flow driven by a `VERSION` file in the repo, while
preserving the manual path.

## Key constraint: the GITHUB_TOKEN anti-recursion rule

GitHub deliberately does **not** fire workflows triggered by events whose actor
is the default `GITHUB_TOKEN` (prevents recursive Actions runs). So if an
auto-tag job pushes a tag using `GITHUB_TOKEN`, `release.yml`'s
`on: push: tags` would **not** trigger.

**Resolution:** the auto-tag workflow invokes `release.yml` directly via
`workflow_call` (a reusable-workflow call inside the same run), rather than
relying on the tag-push to trigger it. This sidesteps the token rule entirely —
no PAT is required merely to *trigger* the release. (A PAT,
`HOMEBREW_TAP_PAT`, is still needed by GoReleaser for the Homebrew formula push;
it is passed through with `secrets: inherit`.)

## Components

### 1. `VERSION` file (repo root)

Plain semver, one line, no leading `v`:

```
0.3.0
```

This is the release version of record. The tag CI creates is `v` + contents
(`v0.3.0`). Matches GoReleaser's stripped form (`{{ .Version }}` and the
`orc_0.3.0_<os>_<arch>.tar.gz` archive names).

### 2. `make build` reads `VERSION` (`Makefile`)

New version-resolution precedence in the Makefile's `VERSION` make-variable
(distinct from the file):

- **If the `VERSION` file exists:** base = `v` + file contents, with a
  git-derived suffix so non-pristine builds stay honest:
  - Clean working tree exactly at tag `v0.3.0` → `v0.3.0`
  - Commits ahead of the tag and/or a dirty tree → a dirtied form
    (e.g. `v0.3.0-<n>-g<sha>-dirty` or `v0.3.0-dev`) — local/dev builds read as
    dirty, never as the pristine release.
- **If the `VERSION` file is missing:** fall back to today's
  `git describe --tags --match 'v*' --always --dirty`.

GoReleaser is **unchanged** — it still reads `{{ .Version }}` from the tag and
only runs once the tag exists, so release artifacts and the `VERSION` file
always agree. The "disagreement window" (file bumped, tag not yet created)
only affects local `make build`, which now reports the version being worked
toward, marked dirty.

`cmd/orc/main.go` is unchanged: `version`/`commit`/`buildDate` are still
ldflags-injected, with the existing BuildInfo fallback for `go install @vX.Y.Z`.

### 3. `auto-tag.yml` (new workflow)

Trigger: `on: push: branches: [main]`.

Job steps (in order — each guard short-circuits cleanly):

1. **Skip if `VERSION` unchanged in this push** — `git diff HEAD^ HEAD -- VERSION`
   is empty ⇒ nothing to release, exit 0. Keeps the Actions log clean on
   unrelated merges.
2. **Validate strict semver** — file contents must match
   `^[0-9]+\.[0-9]+\.[0-9]+$`. A typo (`0.3`, trailing space) fails loudly,
   no tag created.
3. **Reject version going backward** — new version must be strictly greater
   than the latest existing `v*` tag (semver compare). Blocks an accidental
   downgrade.
4. **Skip if tag already exists** — `git rev-parse v$VERSION` succeeds ⇒
   idempotent no-op (safe re-runs).
5. **Create + push the annotated tag on the exact merge commit** being built
   (not a floating HEAD), so release artifacts correspond to that commit.
   Pushed with the default `GITHUB_TOKEN` (`contents: write`).
6. **Call `release.yml`** via `uses:` (workflow_call), with `secrets: inherit`
   so `HOMEBREW_TAP_PAT` reaches GoReleaser.

The semver-validate + version-compare logic is factored into a small,
independently testable shell script (`scripts/version-guard.sh`) invoked by the
workflow, rather than buried inline, so the comparison can be exercised
directly.

### 4. `release.yml` (modified)

Gains a second trigger; the existing one is preserved verbatim:

```yaml
on:
  push:
    tags: ["v*"]      # manual: git tag && push  (UNCHANGED)
  workflow_call:      # auto: invoked by auto-tag.yml
```

The job body is identical for both paths — the tag exists in git either way, so
GoReleaser reads it normally. No version-input branching. `secrets` referenced
by the job (`GITHUB_TOKEN`, `HOMEBREW_TAP_PAT`) resolve for both a direct push
and a `workflow_call` with `secrets: inherit`.

## Data flow

```
Manual (escape hatch, unchanged):
  git tag -a v0.3.0 -m "..." && git push origin v0.3.0
      └─> release.yml (on: push tags) ─> GoReleaser ─> Release + Homebrew

Auto (new):
  bump VERSION→0.3.0, merge to main
      └─> auto-tag.yml (on: push main):
            unchanged? skip · bad semver? fail · backward? fail · tag exists? skip
            └─> create + push tag v0.3.0 on the merge commit
                  └─> release.yml (workflow_call) ─> GoReleaser ─> Release + Homebrew
```

## Error handling / edge cases

| Situation | Behavior |
|-----------|----------|
| `VERSION` unchanged in the push | Job skips early, exit 0 (clean log) |
| Malformed semver in `VERSION` | Job fails loudly, no tag created |
| Version ≤ latest existing tag | Job fails (downgrade guard) |
| Tag `v$VERSION` already exists | No-op (idempotent re-run) |
| `VERSION` file missing | `make build` falls back to `git describe`; auto-tag job treats absent file as "nothing to release" |
| Manual tag pushed | `release.yml` `push:tags` path fires exactly as today |

## Testing

- `scripts/version-guard.sh` (semver validate + strictly-greater compare) is
  exercised directly: valid/invalid formats, equal/lower/higher than the
  "latest tag" argument.
- `make build` VERSION-precedence change verified locally: write `VERSION`,
  `make build`, `orc --version` shows `v<contents>` (dirty form on a dirty
  tree); remove `VERSION`, confirm `git describe` fallback.
- The workflow wiring itself (`workflow_call`, `secrets: inherit`,
  exact-commit tag) is validated by the first real release cut on this branch's
  successor — documented as a manual verification step, not an automated test
  (Actions workflows can't be unit-tested in this repo).

## Docs

- `CLAUDE.md` "Releases" section — document the bump-and-merge flow and the
  still-supported manual path; note the `workflow_call` wiring and that the
  `VERSION` file is the version of record.
- `README.md` — update any release/version note that implies manual-tag-only.

## Beads

This spec implements **orc-29y.7** ("Version-file-driven auto-tagging in CI"),
a child of the **orc-29y** Distribution & Packaging epic.

## Out of scope

- Changing GoReleaser's version source (still the tag).
- Any change to `cmd/orc/main.go` version handling.
- The `orc update` self-updater — separate spec
  (`2026-06-23-orc-update-design.md`), separate bead (orc-29y.6).
