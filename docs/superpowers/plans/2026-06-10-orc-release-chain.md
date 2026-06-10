# orc Release Chain Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give orc a stable release chain — manually-pushed `v*` git tags trigger GoReleaser to build multi-OS/arch binaries, a GitHub Release, and a Homebrew formula; basic CI gates it — then pin horde's worker image to the released version.

**Architecture:** Near-clone of horde's proven CLI release chain (orc has a single artifact, so it's simpler than horde's dual `v*`/`cdk-v*` scheme). Five pieces: version stamping in `cmd/orc/main.go` + Makefile, `.goreleaser.yaml`, `release.yml` workflow, `ci.yml` workflow, and a horde Dockerfile pin. Executed as two PRs (orc first, then horde) because horde cannot pin a version that does not yet exist.

**Tech Stack:** Go 1.22 (urfave/cli/v3), GoReleaser (OSS, pinned `2.12.7`), GitHub Actions (`checkout@v6`, `setup-go@v6`, `goreleaser-action@v7`), Homebrew tap `jorge-barreto/homebrew-tap`.

**Spec:** `docs/superpowers/specs/2026-06-10-orc-release-chain-design.md`

---

## File Structure

**orc repo (PR #1 — Phase 1):**
- Modify: `cmd/orc/main.go` — add `version/commit/buildDate` vars, `versionString()`, wire `Version:` into the root command.
- Modify: `Makefile` — add ldflags stamping to `build`/`install`; add a `vet` target for CI.
- Create: `.goreleaser.yaml` — build matrix, archives, checksums, GitHub Release, Homebrew formula.
- Create: `.github/workflows/ci.yml` — `go vet` + `make test` on PR/push to main.
- Create: `.github/workflows/release.yml` — GoReleaser on `v*` tag.
- Modify: `README.md` — versioned install + brew instructions.
- Modify: `CLAUDE.md` — a "Releases" section documenting the tag scheme + PAT requirement.

**Manual, between PRs (human):**
- Add `HOMEBREW_TAP_PAT` secret to the orc repo (reuse horde's PAT value).
- Push `v0.1.0` tag.

**horde repo (PR #2 — Phase 2, gated on v0.1.0 existing):**
- Modify: `docker/Dockerfile:3-5` — pin orc to `@v0.1.0` via an `ORC_VERSION` ARG.

---

# Phase 1 — orc repo (PR #1)

Work on a branch off orc `main` (e.g. `release-chain`). All paths below are relative to `~/work/orc`.

## Task 1: Version stamping in `cmd/orc/main.go`

**Files:**
- Modify: `cmd/orc/main.go:1-63`
- Test: `cmd/orc/main_test.go` (exists)

- [ ] **Step 1: Write the failing test**

Add to `cmd/orc/main_test.go`:

```go
func TestVersionString(t *testing.T) {
	// Format contract — downstream tooling/bug reports parse this line.
	origV, origC, origD := version, commit, buildDate
	t.Cleanup(func() { version, commit, buildDate = origV, origC, origD })

	version, commit, buildDate = "v0.1.0", "abc1234", "2026-06-10T12:00:00Z"
	got := versionString()
	want := "v0.1.0 (abc1234, built 2026-06-10T12:00:00Z)"
	if got != want {
		t.Errorf("versionString() = %q, want %q", got, want)
	}
}

func TestVersionDefaults(t *testing.T) {
	// The unset-ldflags fallback for `go build` without make. If these
	// regress, dev builds lose version metadata silently.
	if version != "dev" || commit != "none" || buildDate != "unknown" {
		t.Fatalf("defaults drifted: version=%q commit=%q buildDate=%q", version, commit, buildDate)
	}
}
```

Note: horde additionally has a `version_test.go` that runs the app and asserts
the full `orc version <...>` line, guarding the `Version:` wiring itself. orc's
`main.go` builds its `cli.Command` inline in `main()` (no `newApp()` factory),
so that stronger test would require extracting a `newApp()` helper. That
refactor is **optional** — the `Version: versionString()` wiring in Step 4 is a
one-liner verified manually in Step 6. Skip the refactor unless you want the
automated contract test.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/orc/ -run 'TestVersion' -count=1`
Expected: FAIL — build error `undefined: versionString` / `undefined: version` (the vars and helper don't exist yet).

- [ ] **Step 3: Add version vars and `versionString()`**

In `cmd/orc/main.go`, after the `import` block (after line 23) add:

```go
// Overridden at build time via -ldflags
// '-X main.version=... -X main.commit=... -X main.buildDate=...'.
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func versionString() string {
	return fmt.Sprintf("%s (%s, built %s)", version, commit, buildDate)
}
```

(`fmt` is already imported — line 5.)

- [ ] **Step 4: Wire `Version:` into the root command**

In `cmd/orc/main.go`, in the `&cli.Command{` literal, add a `Version` field right after `Name` (line 27):

```go
	app := &cli.Command{
		Name:        "orc",
		Version:     versionString(),
		Usage:       "Deterministic agent orchestrator",
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./cmd/orc/ -run 'TestVersion' -count=1`
Expected: PASS (both `TestVersionString` and `TestVersionDefaults`).

- [ ] **Step 6: Verify `--version` works end-to-end**

Run: `go build -o /tmp/orc-vtest ./cmd/orc/ && /tmp/orc-vtest --version`
Expected: prints a line containing `dev (none, built unknown)` (urfave/cli/v3 renders `orc version dev (none, built unknown)`).
Cleanup: `rm -f /tmp/orc-vtest`

- [ ] **Step 7: Commit**

```bash
git add cmd/orc/main.go cmd/orc/main_test.go
git commit -m "orc: add version stamping (--version)"
```

## Task 2: Makefile ldflags + `vet` target

**Files:**
- Modify: `Makefile:1-13`

- [ ] **Step 1: Add ldflags variables and a `vet` target, stamp `build`/`install`**

Replace lines 1–12 of `Makefile` (the `.PHONY` line through the `install` recipe) with:

```makefile
.PHONY: build install clean test vet e2e e2e-docker

# Version metadata embedded via -ldflags. `version` falls back to git describe
# (tag + offset + SHA) so dev builds are self-identifying; release builds get
# the clean tag from GoReleaser. `--match 'v*'` keeps parity with horde.
VERSION    := $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

# Load .env if present. Variables become available to recipes below.
# Missing .env is fine — the script-phase smoke test does not need the secret.
-include .env
export

build:
	go build -ldflags '$(LDFLAGS)' -o orc ./cmd/orc/

install:
	go install -ldflags '$(LDFLAGS)' ./cmd/orc/

vet:
	go vet ./...
```

(Leave the existing `test`, `e2e`, `e2e-docker`, `clean` recipes — lines 14–26 — unchanged.)

- [ ] **Step 2: Verify build stamps a real version**

Run: `make build && ./orc --version`
Expected: prints `orc version <git-describe-output> (<sha>, built <date>)` — NOT `dev`, since orc now has commits (e.g. `v0.1.0-...` after the first tag, or a bare SHA before any tag).

- [ ] **Step 3: Verify `make vet` passes**

Run: `make vet`
Expected: no output, exit 0.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "orc: stamp version via ldflags; add vet target"
```

## Task 3: `.goreleaser.yaml`

**Files:**
- Create: `.goreleaser.yaml`

- [ ] **Step 1: Create `.goreleaser.yaml`**

```yaml
# GoReleaser builds the orc CLI on a v* tag: multi-OS/arch binaries, a GitHub
# Release with checksums, and a Homebrew formula. Version metadata is injected
# into the same main.* vars that `make build` sets.
version: 2

project_name: orc

before:
  hooks:
    - go mod download

builds:
  - id: orc
    main: ./cmd/orc
    binary: orc
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
      - -X main.version={{ .Version }}
      - -X main.commit={{ .ShortCommit }}
      - -X main.buildDate={{ .Date }}

archives:
  - id: orc
    formats: [tar.gz]
    name_template: "orc_{{ .Version }}_{{ .Os }}_{{ .Arch }}"

checksum:
  name_template: "checksums.txt"

release:
  github:
    owner: jorge-barreto
    name: orc
  name_template: "orc {{ .Tag }}"

changelog:
  use: github
  sort: asc

brews:
  - name: orc
    repository:
      owner: jorge-barreto
      name: homebrew-tap
      token: "{{ .Env.HOMEBREW_TAP_PAT }}"
    homepage: "https://github.com/jorge-barreto/orc"
    description: "Deterministic agent orchestrator"
    license: "MIT"
    commit_author:
      name: github-actions[bot]
      email: github-actions[bot]@users.noreply.github.com
    test: |
      system "#{bin}/orc", "--version"
```

- [ ] **Step 2: Validate the config without releasing**

Requires the `goreleaser` binary locally. If installed:
Run: `goreleaser check`
Expected: `config is valid` (or no errors).

If `goreleaser` is NOT installed locally, skip — the release workflow's pinned `goreleaser-action` validates it on first tag. Note in the commit that validation is deferred to CI.

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yaml
git commit -m "orc: add GoReleaser config (binaries, checksums, Homebrew)"
```

## Task 4: CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Create `.github/workflows/ci.yml`**

```yaml
name: CI

on:
  pull_request:
    branches: [main]
  push:
    branches: [main]

# Cancel superseded runs on the same branch.
concurrency:
  group: ci-${{ github.ref }}
  cancel-in-progress: true

jobs:
  test:
    name: test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - uses: actions/setup-go@v6
        with:
          go-version: "1.22"
          cache: true

      - run: make vet

      - run: make test
```

Note: `go-version: "1.22"` matches orc's `go.mod` (`go 1.22.10`). Pinning to the version `go.mod` declares avoids the `setup-go@v6` + `GOTOOLCHAIN=local` skew failure seen in horde's Node-24 PR. e2e tests are intentionally excluded (need secrets + Docker; developer-local only).

- [ ] **Step 2: Validate the YAML parses**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml')); print('OK')"`
Expected: `OK`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "orc: add CI (vet + test on PR/push to main)"
```

## Task 5: Release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Create `.github/workflows/release.yml`**

```yaml
name: release

# Build and publish orc CLI binaries when a v* tag is pushed. The tag is the
# version source (GoReleaser reads {{ .Version }} from it). Cut a release with:
# git tag -a v0.1.0 -m "..." && git push origin v0.1.0
on:
  push:
    tags:
      - "v*"

permissions:
  contents: write # create the GitHub Release + upload assets

jobs:
  release:
    name: release
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
        with:
          fetch-depth: 0 # GoReleaser needs full history for the changelog

      - uses: actions/setup-go@v6
        with:
          go-version: "1.22"
          cache: true

      - uses: goreleaser/goreleaser-action@v7
        with:
          # pinned (not floating) so a future major can't change behavior silently
          version: "2.12.7"
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          HOMEBREW_TAP_PAT: ${{ secrets.HOMEBREW_TAP_PAT }}
```

- [ ] **Step 2: Validate the YAML parses**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/release.yml')); print('OK')"`
Expected: `OK`.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "orc: add release workflow (GoReleaser on v* tag)"
```

## Task 6: Docs — README install + CLAUDE.md releases section

**Files:**
- Modify: `README.md` (install section, around line 61–75)
- Modify: `CLAUDE.md` (append a Releases section)

- [ ] **Step 1: Update README install section**

In `README.md`, find the `## Installation` section. Replace the `go install …@latest` line with both a versioned go-install and a brew option:

```markdown
## Installation

**Homebrew** (macOS / Linux):

```bash
brew install jorge-barreto/tap/orc
```

**Go install** (pin a released version):

```bash
go install github.com/jorge-barreto/orc/cmd/orc@v0.1.0
```

**From source:**

```bash
make install  # installs to $GOPATH/bin, stamped with the git-describe version
```
```

(Preserve any surrounding prose about the Claude CLI prerequisite.)

- [ ] **Step 2: Append a Releases section to CLAUDE.md**

Add to `CLAUDE.md` (after the Conventions section):

```markdown
## Releases

orc ships as a single Go binary, released on **plain `v*` git tags** (e.g.
`v0.1.0`). Pushing one fires `.github/workflows/release.yml` → GoReleaser
builds the linux/darwin × amd64/arm64 binaries + checksums + a GitHub Release,
and pushes a Homebrew formula to `jorge-barreto/homebrew-tap`.

- Cut a release: `git tag -a v0.1.0 -m "..." && git push origin v0.1.0`.
- GoReleaser strips the leading `v`, so tags are plain semver.
- `make build` stamps the version via `git describe --tags --match 'v*'`.
- Requires the `HOMEBREW_TAP_PAT` repo secret (shared with horde) for the
  formula push; without it the binaries/Release still publish.
```

- [ ] **Step 3: Commit**

```bash
git add README.md CLAUDE.md
git commit -m "orc: document the v* release scheme and versioned install"
```

## Task 7: Open PR #1 and verify CI

- [ ] **Step 1: Push the branch**

```bash
git push -u origin release-chain
```

- [ ] **Step 2: Open the PR**

```bash
gh pr create --base main --head release-chain \
  --title "Add release chain (v* tags, GoReleaser, Homebrew, CI)" \
  --body "Adds orc's release chain per docs/superpowers/specs/2026-06-10-orc-release-chain-design.md: version stamping, GoReleaser config, CI (vet+test), and the release workflow. First release will be v0.1.0 (pushed after merge). See spec for the bootstrap ordering."
```

- [ ] **Step 3: Wait for CI green**

Run: `gh pr checks --watch --interval 20`
Expected: the `test` job passes. If it fails fast on a Go-version error, confirm `go-version: "1.22"` matches `go.mod` (root-cause, not a retry).

- [ ] **Step 4: Merge**

```bash
gh pr merge --squash
```

---

# Manual bootstrap (human — between PRs)

These are not automatable and must happen in order before Phase 2.

- [ ] **Step 1: Add the `HOMEBREW_TAP_PAT` secret to the orc repo**

Reuse the same PAT value horde uses. Via UI: orc repo → Settings → Secrets and variables → Actions → New repository secret, name `HOMEBREW_TAP_PAT`. Or via CLI (paste the token when prompted):

```bash
gh secret set HOMEBREW_TAP_PAT --repo jorge-barreto/orc
```

- [ ] **Step 2: Tag and push v0.1.0**

From orc `main` (after PR #1 merged):

```bash
git checkout main && git pull
git tag -a v0.1.0 -m "orc 0.1.0"
git push origin v0.1.0
```

- [ ] **Step 3: Watch the release workflow**

```bash
gh run watch --repo jorge-barreto/orc --exit-status
```

Verify afterward:
- `gh release view v0.1.0 --repo jorge-barreto/orc` shows 4 tar.gz archives + `checksums.txt`.
- The tap got an `orc.rb`: `gh api repos/jorge-barreto/homebrew-tap/commits --jq '.[0].commit.message'` mentions orc v0.1.0.

---

# Phase 2 — horde repo (PR #2, gated on v0.1.0 existing)

Work in `~/work/horde` on a fresh branch off `main`.

## Task 8: Pin orc to @v0.1.0 in horde's worker image

**Files:**
- Modify: `docker/Dockerfile:1-5`

- [ ] **Step 1: Pin orc via an ORC_VERSION ARG**

In `~/work/horde/docker/Dockerfile`, replace lines 1–5 (the builder stage) with:

```dockerfile
FROM golang:1.25-bookworm AS builder
ENV GOBIN=/out
# Pinned for reproducible builds. Bump with intent (mirrors CLAUDE_VERSION).
ARG ORC_VERSION=v0.1.0
RUN mkdir -p /out \
    && go install github.com/jorge-barreto/orc/cmd/orc@${ORC_VERSION} \
    && go install github.com/jorge-barreto/bd/cmd/bd@latest
```

(`bd` stays `@latest` — out of scope, flagged in the spec.)

- [ ] **Step 2: Verify the image builds and orc reports the pinned version**

Run (from `~/work/horde`):

```bash
docker build -f docker/Dockerfile -t horde-worker-pintest docker/ \
  && docker run --rm --entrypoint orc horde-worker-pintest --version
```

Expected: the `--version` output reports `0.1.0` (not `dev`, not a HEAD SHA), confirming the pin resolved to the released tag. Cleanup: `docker rmi horde-worker-pintest`.

(If a full worker-image build is too heavy locally, a lighter check is to build just the builder stage: `docker build -f docker/Dockerfile --target builder -t orc-pintest docker/ && docker run --rm orc-pintest /out/orc --version`.)

- [ ] **Step 3: Commit**

```bash
git add docker/Dockerfile
git commit -m "docker: pin orc to v0.1.0 instead of @latest"
```

- [ ] **Step 4: Open PR #2 and let CI run**

```bash
git push -u origin pin-orc-v0.1.0
gh pr create --base main --head pin-orc-v0.1.0 \
  --title "Pin orc to v0.1.0 in the worker image" \
  --body "Stops the worker image from silently tracking orc HEAD. Mirrors the CLAUDE_VERSION pin pattern. Requires orc v0.1.0 (released separately)."
gh pr checks --watch --interval 20
```

Expected: the three required jobs (`unit-test`, `integration-test`, `cdk-test`) pass. Merge with `gh pr merge --squash` when green.

---

## Notes for the implementer

- **Commit messages:** orc uses an `orc-<id>:` bead-prefix convention for tracked work, but this release-infra work has no bead; plain descriptive prefixes (`orc:`, `docs:`, `docker:`) are used above. If a bead is created for this, prefix accordingly.
- **Stage specific files** — never `git add .` / `-A` (horde convention; apply to orc too).
- **No AI attribution** in commit messages (orc and horde convention).
- The orc↔horde contract (exit codes, `costs.json`, `run-result.json`) is documented in horde's `ORC_CONTRACT_EXPECTATIONS.md`. This plan changes nothing about that contract — only how orc is built and distributed.
