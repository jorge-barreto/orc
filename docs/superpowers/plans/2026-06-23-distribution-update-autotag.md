# Distribution: Auto-tagging + `orc update` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add version-file-driven auto-tagging in CI (bump `VERSION`, merge to main → tagged release) and an `orc update` self-updater that upgrades the running binary from the latest GitHub Release.

**Architecture:** Two independent subsystems shipped on one branch (`distribution-update-autotag`), one PR. Group A (auto-tagging) is pure CI/build config: a root `VERSION` file, a Makefile change to read it, a new `auto-tag.yml` workflow that creates+pushes the tag and invokes `release.yml` via `workflow_call`, and a `release.yml` gaining a second trigger. Group B (`orc update`) is a new `internal/selfupdate` package (download + checksum-verify + tar-extract, all stdlib) plus a `cmd/orc/update.go` command doing package-manager detection and atomic self-replace.

**Tech Stack:** Go 1.22+, GitHub Actions, GoReleaser (existing), urfave/cli/v3. New code uses only stdlib (`net/http`, `crypto/sha256`, `archive/tar`, `compress/gzip`, `runtime`, `os`, `io`).

**Specs:**
- `docs/superpowers/specs/2026-06-23-version-file-autotag-design.md` (orc-29y.7)
- `docs/superpowers/specs/2026-06-23-orc-update-design.md` (orc-29y.6)

## Global Constraints

- **Dependencies:** stdlib only for new code, beyond the existing `gopkg.in/yaml.v3`, `github.com/urfave/cli/v3`, `github.com/google/uuid`. Do NOT add a third-party dependency.
- **Go formatting:** tabs, never spaces. Idiomatic gofmt. (A PostToolUse hook runs `gofmt -w`, but write correct formatting from the start.)
- **Errors:** wrap with `%w` for error chains.
- **`VERSION` file format:** plain semver, one line, no leading `v` (e.g. `0.3.0`). The git tag is `v` + contents.
- **Archive name template:** `orc_<version-without-v>_<goos>_<goarch>.tar.gz` — must stay in sync with `.goreleaser.yaml`'s `name_template: "orc_{{ .Version }}_{{ .Os }}_{{ .Arch }}"`. Checksums file is `checksums.txt`.
- **Repo / release source:** GitHub repo `jorge-barreto/orc`. Latest-release API: `https://api.github.com/repos/jorge-barreto/orc/releases/latest`. Release asset download base: `https://github.com/jorge-barreto/orc/releases/download/<tag>/`.
- **Platforms:** linux and darwin only, amd64 and arm64 only. No Windows.
- **Manual tagging MUST keep working** as an escape hatch: `git tag -a vX.Y.Z && git push origin vX.Y.Z` fires `release.yml`'s `push: tags` path exactly as today.
- **Exit codes:** orc uses `runner.ExitError{Code, Err}`. Setup/config errors use `runner.ExitConfigError` (3); operational/runtime failures fall through to the default (1 via `ExitCodeFrom`). `orc update` failures (network, checksum, not-writable) are operational → return a plain wrapped error (exit 1). Invalid usage (bad flag combination) → `ExitConfigError`.
- **Command pattern:** new commands are `func xCmd() *cli.Command` registered in `main.go`'s `Commands` slice. Use `cmd.Bool("flag")` / `cmd.Args()`. Print user output to stdout; warnings/errors to stderr.
- **Build version source of truth:** the `VERSION` file. `make build` reads it first; `git describe` is the fallback. GoReleaser is unchanged (reads the tag).

---

# GROUP A — Version-file-driven auto-tagging (build first)

### Task A1: Add the `VERSION` file and make `make build` read it

**Files:**
- Create: `VERSION` (repo root)
- Modify: `Makefile:4-9` (the `VERSION` make-variable block)

**Interfaces:**
- Produces: a root `VERSION` file containing `0.3.0` (the next release version). `make build` stamps `main.version` from it.

The current Makefile (lines 4-9):
```makefile
# Version metadata embedded via -ldflags. `version` falls back to git describe
# (tag + offset + SHA) so dev builds are self-identifying; release builds get
# the clean tag from GoReleaser. `--match 'v*'` keeps parity with horde.
VERSION    := $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
```

The current latest release tag is `v0.2.0`, so the next version is `0.3.0`.

- [ ] **Step 1: Create the `VERSION` file**

Create `VERSION` at the repo root with exactly this content (one line, trailing newline, no leading `v`):
```
0.3.0
```

- [ ] **Step 2: Change the Makefile `VERSION` variable to prefer the file**

Replace the comment + `VERSION :=` line (Makefile:4-6) with this. The rule: if the `VERSION` file exists, base the version on `v<contents>` and append a git-derived dirty/offset suffix so non-pristine builds are honest; otherwise fall back to `git describe`.

```makefile
# Version metadata embedded via -ldflags. When the root VERSION file exists it is
# the source of truth: builds report v<VERSION> with a git-derived suffix so a
# local/dev build (commits past the tag, or a dirty tree) is marked, never
# claiming to be the pristine release. A clean tree exactly at tag v<VERSION>
# reports a bare v<VERSION>. With no VERSION file, fall back to git describe
# (tag + offset + SHA). GoReleaser is unaffected — it reads {{ .Version }} from
# the tag at release time. `--match 'v*'` keeps parity with horde.
VERSION := $(shell \
	if [ -f VERSION ]; then \
		base="v$$(tr -d '[:space:]' < VERSION)"; \
		suffix=$$(git describe --tags --match 'v*' --always --dirty 2>/dev/null | sed -n 's/^v\?[0-9][0-9.]*\(-.*\)$$/\1/p'); \
		printf '%s%s' "$$base" "$$suffix"; \
	else \
		git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev; \
	fi)
```

Explanation of the suffix extraction: `git describe --dirty` prints e.g. `v0.2.0-3-gabc1234` (3 commits past v0.2.0) or `v0.2.0-dirty` or `v0.2.0` (clean, on the tag). The `sed` keeps only the trailing `-...` part (offset/sha/dirty), which is empty when the tree is clean and exactly on a tag. So: clean-on-tag → `v0.3.0`; 3 commits ahead → `v0.3.0-3-gabc1234`; dirty → `v0.3.0-...-dirty`. When `VERSION`=`0.3.0` but the live tag is still `v0.2.0`, the base comes from the file (`v0.3.0`) and the suffix from git (`-N-gSHA`), giving `v0.3.0-N-gSHA` — correctly marked as not the release.

Leave `COMMIT`, `BUILD_DATE`, and `LDFLAGS` exactly as they are.

- [ ] **Step 3: Verify a clean build reports the file version (dirty, since the tag doesn't exist yet)**

Run:
```bash
make build && ./orc --version
```
Expected: version starts with `v0.3.0` and (because `v0.3.0` is not yet a tag and/or the tree has uncommitted changes during development) carries a `-...` suffix — e.g. `v0.3.0-2-gXXXXXXX` or `v0.3.0-...-dirty`. It must NOT be a bare `v0.3.0` while uncommitted/un-tagged. It must NOT report `v0.2.0`.

- [ ] **Step 4: Verify the fallback still works when VERSION is absent**

Run:
```bash
mv VERSION /tmp/VERSION.bak && make build && ./orc --version; mv /tmp/VERSION.bak VERSION
```
Expected: version is a `git describe` string based on the real latest tag (e.g. `v0.2.0-...`), proving the fallback branch. Then `VERSION` is restored.

- [ ] **Step 5: Commit**

```bash
git add VERSION Makefile
git commit -m "build: VERSION file as version source of truth for make build

Bead orc-29y.7. Adds a root VERSION file (plain semver, no leading v) and
makes 'make build' prefer it: v<VERSION> plus a git-derived suffix so dev
builds are marked dirty while a clean tag build is bare. Falls back to git
describe when the file is absent. GoReleaser unchanged (reads the tag).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task A2: `version-guard.sh` — semver validate + strictly-greater compare

**Files:**
- Create: `scripts/version-guard.sh`
- Test: manual shell assertions (documented in Step 2; no Go test harness for shell)

**Interfaces:**
- Produces: `scripts/version-guard.sh` with two subcommands used by `auto-tag.yml` (Task A3):
  - `version-guard.sh validate <version>` — exit 0 if `<version>` is strict semver `MAJOR.MINOR.PATCH` (digits only), else print an error to stderr and exit 1.
  - `version-guard.sh newer <candidate> <latest>` — exit 0 if `<candidate>` is strictly greater than `<latest>` by semver compare (each component numeric). Exit 1 otherwise. `<latest>` may be empty (no prior tag) — then any valid `<candidate>` is "newer". Both args are the plain semver form (no leading `v`); the caller strips `v`.

- [ ] **Step 1: Write the script**

Create `scripts/version-guard.sh`:
```sh
#!/bin/sh
# version-guard.sh — release-version guards for the auto-tag workflow.
#
#   version-guard.sh validate <version>
#       Exit 0 if <version> is strict semver MAJOR.MINOR.PATCH (digits only).
#
#   version-guard.sh newer <candidate> <latest>
#       Exit 0 if <candidate> is strictly greater than <latest> by semver
#       compare. <latest> may be empty (no prior release) — any valid
#       <candidate> is then "newer". Both args are plain semver (no leading v).
#
# All versions are the GoReleaser/VERSION-file form: no leading "v".

set -eu

err() {
	printf 'version-guard: %s\n' "$1" >&2
	exit 1
}

is_semver() {
	# Strict MAJOR.MINOR.PATCH, digits only, no pre-release/build metadata.
	printf '%s' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'
}

cmd="${1:-}"
case "$cmd" in
	validate)
		v="${2:-}"
		[ -n "$v" ] || err "validate: missing version argument"
		is_semver "$v" || err "validate: '$v' is not strict semver MAJOR.MINOR.PATCH"
		;;
	newer)
		cand="${2:-}"
		latest="${3:-}"
		[ -n "$cand" ] || err "newer: missing candidate argument"
		is_semver "$cand" || err "newer: candidate '$cand' is not strict semver"
		# No prior release: any valid candidate is newer.
		[ -n "$latest" ] || exit 0
		is_semver "$latest" || err "newer: latest '$latest' is not strict semver"
		# Compare component by component.
		cand_major=$(printf '%s' "$cand" | cut -d. -f1)
		cand_minor=$(printf '%s' "$cand" | cut -d. -f2)
		cand_patch=$(printf '%s' "$cand" | cut -d. -f3)
		lat_major=$(printf '%s' "$latest" | cut -d. -f1)
		lat_minor=$(printf '%s' "$latest" | cut -d. -f2)
		lat_patch=$(printf '%s' "$latest" | cut -d. -f3)
		if [ "$cand_major" -gt "$lat_major" ]; then exit 0; fi
		if [ "$cand_major" -lt "$lat_major" ]; then err "candidate $cand <= latest $latest"; fi
		if [ "$cand_minor" -gt "$lat_minor" ]; then exit 0; fi
		if [ "$cand_minor" -lt "$lat_minor" ]; then err "candidate $cand <= latest $latest"; fi
		if [ "$cand_patch" -gt "$lat_patch" ]; then exit 0; fi
		err "candidate $cand <= latest $latest"
		;;
	*)
		err "usage: version-guard.sh {validate <version> | newer <candidate> <latest>}"
		;;
esac
```

- [ ] **Step 2: Make it executable and run the assertion suite**

Run each and confirm the expected exit code (`$?` after each):
```bash
chmod +x scripts/version-guard.sh
scripts/version-guard.sh validate 0.3.0;        echo "expect 0: $?"
scripts/version-guard.sh validate 0.3;          echo "expect 1: $?"
scripts/version-guard.sh validate v0.3.0;       echo "expect 1: $?"
scripts/version-guard.sh validate "0.3.0 ";     echo "expect 1: $?"
scripts/version-guard.sh newer 0.3.0 0.2.0;     echo "expect 0: $?"
scripts/version-guard.sh newer 0.3.0 0.3.0;     echo "expect 1: $?"
scripts/version-guard.sh newer 0.2.0 0.3.0;     echo "expect 1: $?"
scripts/version-guard.sh newer 0.10.0 0.9.0;    echo "expect 0: $?"
scripts/version-guard.sh newer 1.0.0 0.99.99;   echo "expect 0: $?"
scripts/version-guard.sh newer 0.3.0 "";        echo "expect 0: $?"
```
Expected: the printed exit codes match the `expect` annotations exactly (0,1,1,1, 0,1,1,0,0,0).

- [ ] **Step 3: Commit**

```bash
git add scripts/version-guard.sh
git commit -m "ci: version-guard.sh — semver validate + strictly-greater compare

Bead orc-29y.7. Pure-POSIX-sh guards used by the auto-tag workflow:
'validate' rejects non-strict-semver; 'newer' blocks a version that is not
strictly greater than the latest tag (empty latest = first release).

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task A3: `auto-tag.yml` workflow + `release.yml` `workflow_call` trigger

**Files:**
- Create: `.github/workflows/auto-tag.yml`
- Modify: `.github/workflows/release.yml:6-9` (add `workflow_call` alongside `push: tags`)

**Interfaces:**
- Consumes: `VERSION` (Task A1), `scripts/version-guard.sh` (Task A2).
- Produces: on a push to `main` that changes `VERSION`, an annotated `v<VERSION>` tag on the merge commit, then a call to `release.yml`.

The current `release.yml` trigger (lines 6-9):
```yaml
on:
  push:
    tags:
      - "v*"
```

- [ ] **Step 1: Add `workflow_call` to `release.yml`**

Change `release.yml` lines 6-9 to keep the manual `push: tags` path AND add `workflow_call`:
```yaml
on:
  push:
    tags:
      - "v*" # manual escape hatch: git tag -a vX.Y.Z && git push origin vX.Y.Z
  workflow_call: # invoked by auto-tag.yml after it creates+pushes the tag
```
Leave the rest of `release.yml` (permissions, the `release` job, GoReleaser step, env) unchanged — the tag exists in git in both paths, so GoReleaser reads `{{ .Version }}` identically.

- [ ] **Step 2: Create `auto-tag.yml`**

Create `.github/workflows/auto-tag.yml`:
```yaml
name: auto-tag

# When VERSION changes on a push to main, create and push the matching v* tag on
# the merge commit, then invoke release.yml via workflow_call. This sidesteps the
# GITHUB_TOKEN anti-recursion rule (a tag pushed by GITHUB_TOKEN would NOT fire
# release.yml's push:tags trigger). Manual 'git tag && push' still works via
# release.yml's own push:tags trigger.
on:
  push:
    branches: [main]

permissions:
  contents: write # create + push the tag

jobs:
  tag:
    name: tag
    runs-on: ubuntu-latest
    outputs:
      tagged: ${{ steps.guard.outputs.tagged }}
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0 # need full history + tags for git describe / tag checks

      - name: Decide whether to tag
        id: guard
        run: |
          set -euo pipefail
          # Skip if VERSION did not change in this push.
          if git rev-parse HEAD^ >/dev/null 2>&1; then
            if git diff --quiet HEAD^ HEAD -- VERSION; then
              echo "VERSION unchanged; nothing to release."
              echo "tagged=false" >> "$GITHUB_OUTPUT"
              exit 0
            fi
          fi
          version="$(tr -d '[:space:]' < VERSION)"
          # Validate strict semver.
          scripts/version-guard.sh validate "$version"
          tag="v$version"
          # Idempotent: skip if the tag already exists.
          if git rev-parse "$tag" >/dev/null 2>&1; then
            echo "Tag $tag already exists; no-op."
            echo "tagged=false" >> "$GITHUB_OUTPUT"
            exit 0
          fi
          # Reject a version that is not strictly greater than the latest v* tag.
          latest="$(git tag -l 'v*' | sed 's/^v//' | sort -t. -k1,1n -k2,2n -k3,3n | tail -n1)"
          scripts/version-guard.sh newer "$version" "$latest"
          # Create the annotated tag on the exact commit being built.
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git tag -a "$tag" -m "Release $tag" "$GITHUB_SHA"
          git push origin "$tag"
          echo "Created and pushed $tag on $GITHUB_SHA"
          echo "tagged=true" >> "$GITHUB_OUTPUT"

  release:
    name: release
    needs: tag
    if: needs.tag.outputs.tagged == 'true'
    uses: ./.github/workflows/release.yml
    secrets: inherit
```

Note: `version-guard.sh` must be executable in the checkout. It was committed with the executable bit in Task A2; if the checkout drops it, the workflow can `sh scripts/version-guard.sh ...` instead — but the committed mode should carry through, so the direct invocation above is correct.

- [ ] **Step 3: Validate YAML syntax locally**

Run (Python is available on the dev machine; this only checks well-formedness, not Actions semantics):
```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/auto-tag.yml')); yaml.safe_load(open('.github/workflows/release.yml')); print('yaml ok')"
```
Expected: `yaml ok` with no traceback.

- [ ] **Step 4: Sanity-check the guard wiring against the committed VERSION**

Run (simulates the guard steps locally against the real repo state):
```bash
version="$(tr -d '[:space:]' < VERSION)"
scripts/version-guard.sh validate "$version" && echo "validate ok"
latest="$(git tag -l 'v*' | sed 's/^v//' | sort -t. -k1,1n -k2,2n -k3,3n | tail -n1)"
echo "latest tag (no v): $latest"
scripts/version-guard.sh newer "$version" "$latest" && echo "newer ok"
```
Expected: `validate ok`, `latest tag (no v): 0.2.0`, `newer ok` (since VERSION is `0.3.0`).

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/auto-tag.yml .github/workflows/release.yml
git commit -m "ci: auto-tag on VERSION bump, invoke release via workflow_call

Bead orc-29y.7. New auto-tag.yml fires on push to main: if VERSION changed,
validates semver, rejects backward versions, skips if the tag exists, then
creates+pushes v<VERSION> on the merge commit and calls release.yml via
workflow_call (secrets: inherit). release.yml gains a workflow_call trigger
beside its existing push:tags path, so the manual tag escape hatch still
works unchanged.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task A4: Document the release flow

**Files:**
- Modify: `CLAUDE.md` (the `## Releases` section)
- Modify: `README.md` (Installation section — add an upgrade note; `orc update` itself is documented in Group B, but the release mechanics belong here)

**Interfaces:**
- Consumes: the behavior established in A1-A3.

- [ ] **Step 1: Update the `## Releases` section in `CLAUDE.md`**

Find the `## Releases` section in `CLAUDE.md`. Replace its first paragraph and the "Cut a release" bullet with text describing the new dual path. The section currently opens:

> orc ships as a single Go binary, released on **plain `v*` git tags** (e.g. `v0.1.0`). Pushing one fires `.github/workflows/release.yml` → GoReleaser ...

Update it to read (preserve the existing GoReleaser/Homebrew/install.sh bullets that follow; only the lead paragraph and the cut-a-release bullet change):

```markdown
orc ships as a single Go binary. There are two ways to cut a release; both end
in `.github/workflows/release.yml` → GoReleaser building the linux/darwin ×
amd64/arm64 binaries + checksums + a GitHub Release, and pushing a Homebrew
formula to `jorge-barreto/homebrew-tap`.

- **Version-file flow (preferred):** the root `VERSION` file (plain semver, no
  leading `v`) is the version of record. Bump it and merge to `main`;
  `.github/workflows/auto-tag.yml` validates the bump (strict semver, strictly
  greater than the latest tag, not already tagged), creates and pushes the
  matching `v<VERSION>` tag on the merge commit, then invokes `release.yml` via
  `workflow_call`. This avoids the GITHUB_TOKEN anti-recursion rule (a tag
  pushed by `GITHUB_TOKEN` does NOT fire `release.yml`'s `push: tags` trigger).
- **Manual flow (escape hatch):** `git tag -a v0.3.0 -m "..." && git push origin
  v0.3.0`. The pushed tag fires `release.yml`'s `push: tags` trigger directly.
  Keep `VERSION` in sync when you tag manually.
- `make build` reads `VERSION` first (stamping `v<VERSION>` plus a git-derived
  suffix so dev builds are marked dirty); it falls back to `git describe` when
  the file is absent. GoReleaser still reads the version from the tag.
```

- [ ] **Step 2: Add an upgrade pointer to the README Installation section**

In `README.md`, in the Installation section (around the `**Go install**` / `make install` block near line 87-96), add a short upgrade note after the install methods:

```markdown
**Upgrading:** if you installed the release binary (via the install script or a
tarball), run `orc update` to fetch and verify the latest release in place. For
Homebrew use `brew upgrade orc`; for a Go install re-run `go install
github.com/jorge-barreto/orc/cmd/orc@latest`.
```

(This forward-references `orc update`, which Group B implements. That is intentional — both ship in the same PR.)

- [ ] **Step 3: Verify docs render / no broken markdown**

Run:
```bash
grep -n "VERSION file" CLAUDE.md && grep -n "orc update" README.md
```
Expected: both greps return at least one line (the new content is present).

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md README.md
git commit -m "docs: document version-file release flow + upgrade note

Bead orc-29y.7. CLAUDE.md Releases section now covers the version-file flow
(bump + merge -> auto-tag -> release) and the manual tag escape hatch, plus
the make-build VERSION precedence. README gains an upgrade pointer.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

# GROUP B — `orc update` self-updater

### Task B1: `internal/selfupdate` — archive name, checksum parse, version compare

**Files:**
- Create: `internal/selfupdate/selfupdate.go`
- Test: `internal/selfupdate/selfupdate_test.go`

**Interfaces:**
- Produces (used by B2, B3):
  - `func ArchiveName(version, goos, goarch string) string` — builds `orc_<ver-no-v>_<goos>_<goarch>.tar.gz`. Strips a leading `v` from `version`.
  - `func ParseChecksums(data []byte, archive string) (sum string, ok bool)` — finds the hex sha256 for `archive` in a `checksums.txt` body (lines of `"<hex>  <filename>"`).
  - `func CompareVersions(a, b string) int` — semver compare of `a` vs `b` (leading `v` ignored on each). Returns -1, 0, or 1. Non-numeric/short inputs compare as best-effort by zero-padding missing components; a fully unparseable component sorts as 0.

- [ ] **Step 1: Write the failing tests**

Create `internal/selfupdate/selfupdate_test.go`:
```go
package selfupdate

import "testing"

func TestArchiveName(t *testing.T) {
	cases := []struct {
		version, goos, goarch, want string
	}{
		{"v0.3.0", "linux", "amd64", "orc_0.3.0_linux_amd64.tar.gz"},
		{"0.3.0", "darwin", "arm64", "orc_0.3.0_darwin_arm64.tar.gz"},
		{"v1.2.10", "linux", "arm64", "orc_1.2.10_linux_arm64.tar.gz"},
	}
	for _, c := range cases {
		if got := ArchiveName(c.version, c.goos, c.goarch); got != c.want {
			t.Errorf("ArchiveName(%q,%q,%q) = %q, want %q", c.version, c.goos, c.goarch, got, c.want)
		}
	}
}

func TestParseChecksums(t *testing.T) {
	body := []byte(
		"aaaa1111  orc_0.3.0_linux_amd64.tar.gz\n" +
			"bbbb2222  orc_0.3.0_darwin_arm64.tar.gz\n")
	sum, ok := ParseChecksums(body, "orc_0.3.0_darwin_arm64.tar.gz")
	if !ok || sum != "bbbb2222" {
		t.Errorf("ParseChecksums darwin = (%q,%v), want (bbbb2222,true)", sum, ok)
	}
	if _, ok := ParseChecksums(body, "orc_9.9.9_linux_amd64.tar.gz"); ok {
		t.Error("ParseChecksums for absent archive should return ok=false")
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.3.0", "v0.2.0", 1},
		{"0.2.0", "0.3.0", -1},
		{"v0.3.0", "0.3.0", 0},
		{"v0.10.0", "v0.9.0", 1},
		{"v1.0.0", "v0.99.99", 1},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/selfupdate/ -run 'TestArchiveName|TestParseChecksums|TestCompareVersions' -count=1`
Expected: FAIL — package `selfupdate` does not compile (undefined `ArchiveName`, etc.).

- [ ] **Step 3: Implement the three helpers**

Create `internal/selfupdate/selfupdate.go`:
```go
// Package selfupdate downloads, verifies, and extracts orc release binaries
// from GitHub Releases. It is the reusable core behind the `orc update` command:
// resolve the latest tag, download the platform tarball + checksums.txt, verify
// the SHA-256 before extracting, and hand back a verified temp binary. All
// standard library — no third-party dependency.
package selfupdate

import (
	"strconv"
	"strings"
)

// ArchiveName builds the GoReleaser archive name for a release, mirroring
// .goreleaser.yaml's name_template "orc_{{ .Version }}_{{ .Os }}_{{ .Arch }}".
// A leading "v" on version is stripped (GoReleaser uses the bare semver).
func ArchiveName(version, goos, goarch string) string {
	v := strings.TrimPrefix(version, "v")
	return "orc_" + v + "_" + goos + "_" + goarch + ".tar.gz"
}

// ParseChecksums returns the hex SHA-256 for archive from a checksums.txt body.
// Each line is "<hex>  <filename>" (two spaces, GNU coreutils format). ok is
// false when archive is not listed.
func ParseChecksums(data []byte, archive string) (sum string, ok bool) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == archive {
			return fields[0], true
		}
	}
	return "", false
}

// CompareVersions compares two semver strings (leading "v" ignored on each).
// Returns -1 if a < b, 0 if equal, 1 if a > b. Missing components are treated
// as 0; a non-numeric component is treated as 0.
func CompareVersions(a, b string) int {
	pa := parseSemver(a)
	pb := parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// Drop any pre-release/build suffix after the first '-' or '+'.
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	var out [3]int
	for i := 0; i < 3 && i < len(parts); i++ {
		n, _ := strconv.Atoi(parts[i])
		out[i] = n
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/selfupdate/ -run 'TestArchiveName|TestParseChecksums|TestCompareVersions' -count=1`
Expected: PASS (ok).

- [ ] **Step 5: Commit**

```bash
git add internal/selfupdate/selfupdate.go internal/selfupdate/selfupdate_test.go
git commit -m "selfupdate: archive name, checksum parse, semver compare

Bead orc-29y.6. Pure helpers for the orc update core: ArchiveName mirrors
the GoReleaser name_template, ParseChecksums reads the GNU coreutils
checksums.txt format, CompareVersions does leading-v-tolerant semver compare.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task B2: `internal/selfupdate` — resolve latest + download/verify/extract

**Files:**
- Modify: `internal/selfupdate/selfupdate.go` (add HTTP + download/verify/extract)
- Test: `internal/selfupdate/download_test.go`

**Interfaces:**
- Consumes: `ArchiveName`, `ParseChecksums` (B1).
- Produces (used by B3):
  - `func ResolveLatest(ctx context.Context) (tag string, err error)` — GET the releases/latest API, return `tag_name` (e.g. `v0.3.0`).
  - `func Download(ctx context.Context, tag, goos, goarch string) (binPath string, err error)` — download archive + checksums.txt from the release, verify SHA-256, extract `orc` to a temp file, return its path. The caller is responsible for removing the temp file. On any verify/extract error, no usable temp binary is returned (any partial temp is removed) and the error is returned.
  - Both use package-level overridable base URLs so tests can point at an `httptest.Server`:
    - `var APIBase = "https://api.github.com"`
    - `var DownloadBase = "https://github.com"`

- [ ] **Step 1: Write the failing tests**

Create `internal/selfupdate/download_test.go`:
```go
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// makeArchive returns a gzipped tar containing a single file "orc" with body.
func makeArchive(t *testing.T, body []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "orc", Mode: 0o755, Size: int64(len(body))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// testSHA256 is the test's own checksum helper; the production sha256hex is
// unexported in the same package and added in B2 Step 3, so the test must not
// redefine it.
func testSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func TestResolveLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			fmt.Fprint(w, `{"tag_name":"v0.3.0","name":"orc v0.3.0"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	old := APIBase
	APIBase = srv.URL
	defer func() { APIBase = old }()

	tag, err := ResolveLatest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v0.3.0" {
		t.Errorf("tag = %q, want v0.3.0", tag)
	}
}

func TestDownload_GoodChecksum(t *testing.T) {
	binBody := []byte("#!/bin/sh\necho fake orc\n")
	archive := makeArchive(t, binBody)
	archiveName := ArchiveName("v0.3.0", "linux", "amd64")
	checksums := fmt.Sprintf("%s  %s\n", testSHA256(archive), archiveName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, archiveName):
			w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			fmt.Fprint(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := DownloadBase
	DownloadBase = srv.URL
	defer func() { DownloadBase = old }()

	path, err := Download(context.Background(), "v0.3.0", "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, binBody) {
		t.Errorf("extracted binary body mismatch")
	}
}

func TestDownload_BadChecksum(t *testing.T) {
	binBody := []byte("fake orc")
	archive := makeArchive(t, binBody)
	archiveName := ArchiveName("v0.3.0", "linux", "amd64")
	// Wrong checksum on purpose.
	checksums := fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), archiveName)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, archiveName):
			w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			fmt.Fprint(w, checksums)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := DownloadBase
	DownloadBase = srv.URL
	defer func() { DownloadBase = old }()

	path, err := Download(context.Background(), "v0.3.0", "linux", "amd64")
	if err == nil {
		os.Remove(path)
		t.Fatal("expected checksum mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "checksum") {
		t.Errorf("error = %v, want a checksum mismatch", err)
	}
	if path != "" {
		t.Errorf("expected no temp path on failure, got %q", path)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/selfupdate/ -run 'TestResolveLatest|TestDownload' -count=1`
Expected: FAIL — undefined `ResolveLatest`, `Download`, `APIBase`, `DownloadBase`.

- [ ] **Step 3: Implement resolve + download/verify/extract**

Add to `internal/selfupdate/selfupdate.go`. Add the new imports to the existing import block (`archive/tar`, `compress/gzip`, `context`, `crypto/sha256`, `encoding/hex`, `encoding/json`, `fmt`, `io`, `net/http`, `os`, `path/filepath`, `time`) and append the code below:

```go
// APIBase and DownloadBase are the GitHub endpoints, overridable in tests.
var (
	APIBase      = "https://api.github.com"
	DownloadBase = "https://github.com"
)

const repoPath = "jorge-barreto/orc"

// httpClient bounds every network call.
var httpClient = &http.Client{Timeout: 60 * time.Second}

// ResolveLatest returns the latest release tag (e.g. "v0.3.0").
func ResolveLatest(ctx context.Context) (string, error) {
	url := APIBase + "/repos/" + repoPath + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("selfupdate: building request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("selfupdate: querying latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("selfupdate: latest-release request returned %s", resp.Status)
	}
	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("selfupdate: decoding latest release: %w", err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("selfupdate: latest release has no tag_name")
	}
	return payload.TagName, nil
}

// Download fetches the release tarball for tag/goos/goarch, verifies it against
// checksums.txt, extracts the orc binary to a temp file, and returns the temp
// path. The caller must remove the returned file. On any error no usable temp
// binary is returned and any partial temp file is removed.
func Download(ctx context.Context, tag, goos, goarch string) (string, error) {
	archive := ArchiveName(tag, goos, goarch)
	base := DownloadBase + "/" + repoPath + "/releases/download/" + tag

	archiveBytes, err := fetch(ctx, base+"/"+archive)
	if err != nil {
		return "", fmt.Errorf("selfupdate: downloading %s: %w", archive, err)
	}
	checksumBytes, err := fetch(ctx, base+"/checksums.txt")
	if err != nil {
		return "", fmt.Errorf("selfupdate: downloading checksums.txt: %w", err)
	}

	want, ok := ParseChecksums(checksumBytes, archive)
	if !ok {
		return "", fmt.Errorf("selfupdate: %s not listed in checksums.txt", archive)
	}
	got := sha256hex(archiveBytes)
	if got != want {
		return "", fmt.Errorf("selfupdate: checksum mismatch for %s (want %s, got %s)", archive, want, got)
	}

	binBytes, err := extractBinary(archiveBytes)
	if err != nil {
		return "", fmt.Errorf("selfupdate: extracting orc from %s: %w", archive, err)
	}

	tmp, err := os.CreateTemp("", "orc-update-*")
	if err != nil {
		return "", fmt.Errorf("selfupdate: creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(binBytes); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return "", fmt.Errorf("selfupdate: writing temp binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("selfupdate: closing temp binary: %w", err)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return "", fmt.Errorf("selfupdate: chmod temp binary: %w", err)
	}
	return tmpName, nil
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s returned %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// extractBinary reads a gzipped tar and returns the bytes of the "orc" entry.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytesReader(archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("no orc entry in archive")
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == "orc" && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}
```

Add one tiny helper near the top of the file (after the imports) so `extractBinary` can wrap the byte slice without pulling `bytes` into the production import set just for this (or simply import `bytes` and use `bytes.NewReader`):
```go
import "bytes"
// ...
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
```
(If you prefer, drop `bytesReader` and call `bytes.NewReader(archive)` directly in `extractBinary` — either is fine; just keep the import set gofmt-clean.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/selfupdate/ -count=1`
Expected: PASS — all of `TestResolveLatest`, `TestDownload_GoodChecksum`, `TestDownload_BadChecksum`, plus the B1 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/selfupdate/selfupdate.go internal/selfupdate/download_test.go
git commit -m "selfupdate: resolve latest + download/verify/extract

Bead orc-29y.6. ResolveLatest hits the releases/latest API; Download fetches
the platform tarball + checksums.txt, verifies SHA-256 before extracting orc
to a temp file, and removes any partial temp on error. Endpoints are
package-level vars so tests drive them against httptest. All stdlib.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task B3: `internal/selfupdate` — install-method detection + atomic replace

**Files:**
- Modify: `internal/selfupdate/selfupdate.go` (add detection + replace)
- Test: `internal/selfupdate/install_test.go`

**Interfaces:**
- Produces (used by B4):
  - `type InstallMethod int` with values `MethodDirect`, `MethodHomebrew`, `MethodGo`.
  - `func DetectInstall(exePath string) InstallMethod` — classify a resolved executable path by where it lives. Homebrew paths (`/opt/homebrew`, contains `/Cellar/`, `/home/linuxbrew/`) → `MethodHomebrew`; Go paths (contains `/go/bin/`, or under `$GOBIN`/`$GOPATH/bin`) → `MethodGo`; else `MethodDirect`.
  - `func ReplaceBinary(targetPath, newBinary string) error` — atomically replace `targetPath` with the file at `newBinary` via a same-directory temp + `os.Rename`. Returns an error (without modifying `targetPath`) if the target's directory is not writable.

- [ ] **Step 1: Write the failing tests**

Create `internal/selfupdate/install_test.go`:
```go
package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectInstall(t *testing.T) {
	cases := []struct {
		path string
		want InstallMethod
	}{
		{"/opt/homebrew/bin/orc", MethodHomebrew},
		{"/usr/local/Cellar/orc/0.2.0/bin/orc", MethodHomebrew},
		{"/home/linuxbrew/.linuxbrew/bin/orc", MethodHomebrew},
		{"/home/jb/go/bin/orc", MethodGo},
		{"/usr/local/bin/orc", MethodDirect},
		{"/home/jb/.local/bin/orc", MethodDirect},
	}
	for _, c := range cases {
		if got := DetectInstall(c.path); got != c.want {
			t.Errorf("DetectInstall(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestReplaceBinary(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "orc")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	newBin := filepath.Join(t.TempDir(), "neworc")
	if err := os.WriteFile(newBin, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceBinary(target, newBin); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "NEW" {
		t.Errorf("target body = %q, want NEW", got)
	}
}

func TestReplaceBinary_UnwritableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "orc")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil { // r-x: not writable
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700) // restore so t.TempDir cleanup works
	newBin := filepath.Join(t.TempDir(), "neworc")
	if err := os.WriteFile(newBin, []byte("NEW"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceBinary(target, newBin); err == nil {
		t.Fatal("expected error replacing into an unwritable dir, got nil")
	}
	// Target must be untouched.
	got, _ := os.ReadFile(target)
	if string(got) != "OLD" {
		t.Errorf("target body = %q, want OLD (unchanged)", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/selfupdate/ -run 'TestDetectInstall|TestReplaceBinary' -count=1`
Expected: FAIL — undefined `InstallMethod`, `DetectInstall`, `ReplaceBinary`.

- [ ] **Step 3: Implement detection + atomic replace**

Add to `internal/selfupdate/selfupdate.go` (ensure `os`, `path/filepath`, `strings`, `fmt`, `io` are imported — most already are):
```go
// InstallMethod classifies how the running binary was installed.
type InstallMethod int

const (
	MethodDirect   InstallMethod = iota // standalone binary we can replace in place
	MethodHomebrew                      // managed by Homebrew
	MethodGo                            // installed via `go install`
)

// DetectInstall classifies a resolved executable path by where it lives.
// exePath should already be symlink-resolved (filepath.EvalSymlinks).
func DetectInstall(exePath string) InstallMethod {
	p := filepath.ToSlash(exePath)
	if strings.HasPrefix(p, "/opt/homebrew/") ||
		strings.Contains(p, "/Cellar/") ||
		strings.Contains(p, "/linuxbrew/") {
		return MethodHomebrew
	}
	if strings.Contains(p, "/go/bin/") {
		return MethodGo
	}
	if gobin := os.Getenv("GOBIN"); gobin != "" && strings.HasPrefix(p, filepath.ToSlash(gobin)+"/") {
		return MethodGo
	}
	if gopath := os.Getenv("GOPATH"); gopath != "" {
		if strings.HasPrefix(p, filepath.ToSlash(filepath.Join(gopath, "bin"))+"/") {
			return MethodGo
		}
	}
	return MethodDirect
}

// ReplaceBinary atomically replaces targetPath with the contents of newBinary.
// It writes a temp file in the SAME directory as targetPath (so os.Rename is
// atomic and never cross-device), then renames over the target. The running
// process keeps its open inode, so replacing a live binary is safe on
// linux/darwin. On any error before the rename, targetPath is untouched.
func ReplaceBinary(targetPath, newBinary string) error {
	dir := filepath.Dir(targetPath)
	src, err := os.Open(newBinary)
	if err != nil {
		return fmt.Errorf("selfupdate: opening new binary: %w", err)
	}
	defer src.Close()

	tmp, err := os.CreateTemp(dir, ".orc-update-*")
	if err != nil {
		return fmt.Errorf("selfupdate: cannot write to %s (need write permission; use sudo or set ORC_BIN_DIR): %w", dir, err)
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("selfupdate: copying new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("selfupdate: closing temp binary: %w", err)
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("selfupdate: chmod temp binary: %w", err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("selfupdate: replacing %s: %w", targetPath, err)
	}
	return nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/selfupdate/ -count=1`
Expected: PASS — all selfupdate tests (B1, B2, B3).

- [ ] **Step 5: Commit**

```bash
git add internal/selfupdate/selfupdate.go internal/selfupdate/install_test.go
git commit -m "selfupdate: install-method detection + atomic binary replace

Bead orc-29y.6. DetectInstall classifies Homebrew / go-install / direct
paths. ReplaceBinary writes a same-dir temp and os.Rename's over the target
(atomic; live process keeps its inode), failing without touching the target
when the dir is not writable.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task B4: `cmd/orc/update.go` — the `update` command

**Files:**
- Create: `cmd/orc/update.go`
- Modify: `cmd/orc/main.go:73-89` (register `updateCmd()` in the `Commands` slice)

**Interfaces:**
- Consumes: `selfupdate.ResolveLatest`, `selfupdate.Download`, `selfupdate.DetectInstall`, `selfupdate.ReplaceBinary`, `selfupdate.CompareVersions` (B1-B3); the package-level `version` var and `resolveVersion`/`versionString` helpers in `main.go`.
- Produces: `func updateCmd() *cli.Command`.

This task has no Go unit test of its own (it is thin CLI wiring over the tested `selfupdate` package and does real network/exec work); it is verified by a build + `--help` smoke check. The risky logic all lives in `selfupdate`, which is unit-tested.

- [ ] **Step 1: Create `cmd/orc/update.go`**

```go
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/jorge-barreto/orc/internal/selfupdate"
	cli "github.com/urfave/cli/v3"
)

func updateCmd() *cli.Command {
	return &cli.Command{
		Name:    "update",
		Aliases: []string{"upgrade"},
		Usage:   "Update orc to the latest release",
		Description: "Downloads the latest released orc binary, verifies its checksum, and " +
			"replaces the running binary in place. Use --check to only report whether an " +
			"update is available. Binaries installed via Homebrew or 'go install' are not " +
			"replaced — the matching package-manager command is suggested instead.",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "check", Usage: "Report whether an update is available without installing"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return runUpdate(ctx, cmd.Bool("check"))
		},
	}
}

func runUpdate(ctx context.Context, checkOnly bool) error {
	current := currentVersion()

	latest, err := selfupdate.ResolveLatest(ctx)
	if err != nil {
		return err
	}

	cmp := selfupdate.CompareVersions(latest, current)
	if checkOnly {
		switch {
		case current == "dev":
			fmt.Printf("current: dev build\nlatest:  %s\n", latest)
		case cmp <= 0:
			fmt.Printf("orc is up to date (%s)\n", current)
		default:
			fmt.Printf("current: %s\nlatest:  %s  (update available)\nrun 'orc update' to install\n", current, latest)
		}
		return nil
	}

	if current != "dev" && cmp <= 0 {
		fmt.Printf("orc is already up to date (%s)\n", current)
		return nil
	}

	// Resolve the real executable path and check how it was installed.
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating the running binary: %w", err)
	}
	realExe, err := filepath.EvalSymlinks(exe)
	if err != nil {
		realExe = exe // fall back to the unresolved path
	}
	switch selfupdate.DetectInstall(realExe) {
	case selfupdate.MethodHomebrew:
		fmt.Printf("orc was installed via Homebrew. Update it with:\n  brew upgrade orc\n")
		return nil
	case selfupdate.MethodGo:
		fmt.Printf("orc was installed via 'go install'. Update it with:\n  go install github.com/jorge-barreto/orc/cmd/orc@latest\n")
		return nil
	}

	fmt.Printf("Updating orc %s -> %s ...\n", current, latest)
	binPath, err := selfupdate.Download(ctx, latest, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	defer os.Remove(binPath)

	if err := selfupdate.ReplaceBinary(realExe, binPath); err != nil {
		return err
	}
	fmt.Printf("Updated orc to %s\n", latest)
	return nil
}

// currentVersion returns the resolved running version (e.g. "v0.2.0" or "dev"),
// using the same ldflags/BuildInfo resolution as `orc --version`.
func currentVersion() string {
	if version != "dev" {
		return version
	}
	// Mirror versionString()'s BuildInfo fallback for go-install builds.
	return resolveVersion(buildInfoVersion())
}
```

Note: `resolveVersion` already exists in `main.go`. You need a small helper `buildInfoVersion()` that returns `debug.ReadBuildInfo().Main.Version` (or `""`). Either add it to `update.go` (importing `runtime/debug`) or reuse the inline logic. Add to `update.go`:
```go
import "runtime/debug"

func buildInfoVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		return bi.Main.Version
	}
	return ""
}
```
(Combine the imports into the single import block — do not write two import statements.)

- [ ] **Step 2: Register the command in `main.go`**

In `cmd/orc/main.go`, add `updateCmd(),` to the `Commands` slice (lines 73-89), after `debugCmd(),`:
```go
			testCmd(),
			debugCmd(),
			updateCmd(),
		},
```

- [ ] **Step 3: Build and smoke-test the command surface**

Run:
```bash
make build
./orc update --help
```
Expected: `--help` prints the `update` usage with the `upgrade` alias, the description, and the `--check` flag. No panic.

- [ ] **Step 4: Smoke-test `--check` against the real API (network-dependent)**

Run:
```bash
./orc update --check
```
Expected: with the current build version `v0.3.0-...` (a dev/ahead build) and the latest published release `v0.2.0`, it prints either "up to date" or a current/latest report — it must NOT download or modify the binary, and must exit 0. (If offline, it errors cleanly with a network message and non-zero exit; that is acceptable and not a failure of this task.)

- [ ] **Step 5: Verify the full suite still builds and passes**

Run:
```bash
go build ./... && go test ./... -count=1
```
Expected: build succeeds; all tests pass.

- [ ] **Step 6: Commit**

```bash
git add cmd/orc/update.go cmd/orc/main.go
git commit -m "cmd: orc update self-updater command

Bead orc-29y.6. 'orc update' (alias 'upgrade') resolves the latest release,
and unless --check is given, downloads + verifies it and atomically replaces
the running binary. Homebrew/go-install binaries are detected and the user is
redirected to the right upgrade command. Non-interactive; --check reports
without installing.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task B5: Document `orc update`

**Files:**
- Modify: `internal/docs/content.go` (the `topicDevtools` content string, starting at line 1360)
- Modify: `README.md` (CLI Reference section — add an `### \`orc update\`` entry)

**Interfaces:**
- Consumes: the command behavior from B4.

- [ ] **Step 1: Add `orc update` to the `devtools` docs topic**

Open `internal/docs/content.go`, find `const topicDevtools = ` (line 1360). Add a section documenting `update` within that topic's content (match the existing formatting of the other command sections in the string — plain text, the same heading style the topic already uses). Insert text equivalent to:
```
orc update
  Update orc to the latest release. Downloads the matching release tarball,
  verifies it against the published checksums, and atomically replaces the
  running binary. Non-interactive.

    orc update            Install the latest release in place
    orc update --check    Report whether an update is available; install nothing
    orc upgrade           Alias for 'orc update'

  Binaries installed via Homebrew or 'go install' are not replaced — orc detects
  them and points you at 'brew upgrade orc' or
  'go install github.com/jorge-barreto/orc/cmd/orc@latest' instead.
```
Match the surrounding indentation/heading conventions already used inside `topicDevtools` (read the first ~30 lines of the string to mirror its style exactly).

- [ ] **Step 2: Update the `devtools` topic Summary to mention update**

In the `topics` slice (around line 53-57), the `devtools` entry's `Summary` is `"orc test, debug, report, improve, eval, flow, and doctor"`. Change it to include update:
```go
		Summary: "orc test, debug, report, improve, eval, flow, doctor, and update",
```

- [ ] **Step 3: Add a CLI Reference entry to README**

In `README.md`'s `## CLI Reference` section, add a new subsection (place it after the `### \`orc debug\`` entry, before `## Configuration Reference`):
```markdown
### `orc update`

Update orc to the latest release. Downloads the matching release tarball, verifies it against the published `checksums.txt`, and atomically replaces the running binary.

```bash
orc update            # install the latest release in place
orc update --check    # report whether an update is available; install nothing
orc upgrade           # alias for orc update
```

Binaries installed via Homebrew or `go install` aren't replaced — orc detects them and points you at `brew upgrade orc` or `go install github.com/jorge-barreto/orc/cmd/orc@latest`.
```

- [ ] **Step 4: Verify the docs topic still loads and tests pass**

Run:
```bash
make build && ./orc docs devtools | grep -n "orc update" && go test ./internal/docs/ -count=1
```
Expected: `orc docs devtools` output contains the `orc update` section; docs tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/docs/content.go README.md
git commit -m "docs: document orc update in devtools topic and README

Bead orc-29y.6. orc docs devtools and the README CLI reference now cover
'orc update' / '--check' / the 'upgrade' alias and the package-manager
redirect behavior.

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task B6: Final integration gate

**Files:** none (verification only).

- [ ] **Step 1: Full build, vet, and test**

Run:
```bash
make vet && make test && make build
```
Expected: vet clean, all tests pass, build succeeds and stamps `v0.3.0-...`.

- [ ] **Step 2: Confirm no new dependencies crept in**

Run:
```bash
git diff main -- go.mod go.sum
```
Expected: empty diff (no new module requirements).

- [ ] **Step 3: Confirm the command set**

Run:
```bash
./orc --help | grep -E "update|upgrade"
```
Expected: `update` listed (with its usage); the `upgrade` alias is reachable via `./orc upgrade --help`.

- [ ] **Step 4: No commit** — this task only verifies. If anything fails, fix it under the owning task.

---

## Notes for the executor

- **Build order matters:** do Group A fully before Group B only insofar as A4/B5 both touch README — keep their edits to different sections (A4 edits Installation; B5 edits CLI Reference) to avoid conflicts. They do not overlap.
- **`VERSION` = `0.3.0`** is the release this branch is working toward. Do not change it mid-implementation.
- The `auto-tag.yml` workflow cannot be exercised in a unit test; its guards (`version-guard.sh`) are tested directly (A2) and the wiring is validated by the first real release after merge — note this in the final review, it is expected.
- `orc update`'s network/exec paths (B4) are deliberately thin over the unit-tested `selfupdate` package; the command itself is smoke-tested, not unit-tested.
