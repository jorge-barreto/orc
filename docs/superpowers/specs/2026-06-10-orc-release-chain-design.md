# orc release chain — design

**Date:** 2026-06-10
**Status:** Approved (brainstorming), pending spec review
**Repos:** `jorge-barreto/orc` (primary), `jorge-barreto/horde` (consumer)

## Problem

orc has **no release chain**: zero git tags, no CI, no version stamping, no
GoReleaser, no published binaries. It cannot even report its own version
(`cmd/orc/main.go` has no `Version:` field).

horde builds on top of orc and installs it into its worker image with:

```dockerfile
# horde docker/Dockerfile:4
go install github.com/jorge-barreto/orc/cmd/orc@latest
```

`@latest` resolves to orc's default-branch HEAD, so **every horde worker-image
rebuild silently picks up whatever orc commit is newest** — orc changes reach
production with no version gate, no pin, no intent. This is inconsistent with
horde itself, which already pins the Claude CLI to an exact version
(`CLAUDE_VERSION=2.1.114`, "Pinned … for reproducible builds. Bump with
intent.").

## Goal

Give orc the same proven release discipline horde has, then make horde pin a
real orc version:

- Manually-pushed `v*` git tags trigger releases (same model as horde's CLI).
- GoReleaser builds multi-OS/arch binaries, a GitHub Release, and a Homebrew
  formula in `jorge-barreto/homebrew-tap`.
- Basic CI gates releases (`go vet` + `make test` on every PR/push).
- First release is **v0.1.0** (pre-1.0: orc is actively evolving; the
  orc↔horde contract is not yet frozen).
- horde pins orc to **exact `@v0.1.0`**, mirroring its `CLAUDE_VERSION` pattern.

orc becomes independently installable (`brew install`, versioned `go install`,
downloadable binaries) and horde stops tracking orc's HEAD.

## Non-goals

- No version file / `tag-on-bump` automation. The git tag is the single source
  of truth (considered and rejected in favor of horde-CLI-style manual tags).
- No conventional-commits / release-please bot. orc keeps its `orc-<id>:`
  commit convention.
- No Go version bump. orc stays on `go 1.22` (its real minimum, set by
  urfave/cli/v3); CI pins `setup-go` to match `go.mod` so there is no toolchain
  skew.
- e2e tests stay developer-local (they need secrets + Docker), exactly as
  horde's e2e tests do. Only unit tests gate releases.
- `bd` (the other `@latest` tool in horde's Dockerfile, line 5) is **not** in
  scope. Flagged for a future task.

## This is a near-clone of horde's chain

horde's CLI release chain is proven (it shipped v0.5.0). orc's is *simpler*
because orc has a single artifact — horde juggles two (CLI on `v*`, CDK npm on
`cdk-v*`) and needs mutually-exclusive tag globs. orc has only `v*`, so the
glob is unambiguous and none of horde's dual-artifact gymnastics apply.

Reference files in horde: `Makefile`, `.goreleaser.yaml`,
`.github/workflows/release-cli.yml`, `.github/workflows/ci.yml`,
`cmd/horde/main.go` (version vars), `cmd/horde/update.go` (`versionString()`).

## Work breakdown — five pieces

### 1. Version stamping in orc (net-new code)

orc's `cmd/orc/main.go` has no version reporting. Add, mirroring horde:

```go
// cmd/orc/main.go — package-level, overridden via -ldflags
var (
	version   = "dev"
	commit    = "none"
	buildDate = "unknown"
)

func versionString() string {
	return fmt.Sprintf("%s (%s, built %s)", version, commit, buildDate)
}

// in the root cli.Command:
app := &cli.Command{
	Name:    "orc",
	Version: versionString(),  // urfave/cli/v3 wires up --version
	...
}
```

This matches horde's exact rendering: `orc version 0.1.0 (abc1234, built
2026-06-10T...)`. urfave/cli/v3 prints `Version` on `--version` automatically;
no separate `version` subcommand is required (horde has one only because it
also checks for updates — orc has no self-update, so the `--version` flag
suffices).

`Makefile` — replace the bare `build`/`install` recipes with horde's
ldflags-stamping block, retargeted to `./cmd/orc`:

```makefile
VERSION    := $(shell git describe --tags --match 'v*' --always --dirty 2>/dev/null || echo dev)
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

build:
	go build -ldflags '$(LDFLAGS)' -o orc ./cmd/orc/
install:
	go install -ldflags '$(LDFLAGS)' ./cmd/orc/
```

(`--match 'v*'` is harmless for orc even without a second tag prefix; it keeps
parity with horde and ignores any future non-`v` tags.)

### 2. `.goreleaser.yaml`

Adapted from horde's, retargeted:

- `project_name: orc`, `main: ./cmd/orc`, `binary: orc`.
- Same build matrix: `goos: [linux, darwin]`, `goarch: [amd64, arm64]`,
  `CGO_ENABLED=0`.
- Same ldflags injection (`-s -w -X main.version={{ .Version }} -X
  main.commit={{ .ShortCommit }} -X main.buildDate={{ .Date }}`).
- `archives` tar.gz, `name_template: orc_{{ .Version }}_{{ .Os }}_{{ .Arch }}`.
- `checksum: checksums.txt`.
- `release` to `owner: jorge-barreto, name: orc`,
  `name_template: "orc {{ .Tag }}"`.
- `changelog: use: github` (see Detail B below).
- `brews:` block → `jorge-barreto/homebrew-tap`, formula `name: orc`,
  `description`, `license: MIT`, `homepage`,
  `token: "{{ .Env.HOMEBREW_TAP_PAT }}"`, formula `test: system "#{bin}/orc",
  "--version"`.

GoReleaser (OSS) strips the leading `v` from the tag natively, so a `v0.1.0`
tag yields `{{ .Version }}` = `0.1.0` with no template workarounds — same as
horde.

### 3. `.github/workflows/release.yml`

Near-identical to horde's `release-cli.yml`:

- Trigger: `on: push: tags: ["v*"]`.
- `permissions: contents: write` (create the Release).
- Steps: `actions/checkout@v6` (`fetch-depth: 0` for changelog) →
  `actions/setup-go@v6` (`go-version: "1.22"` to match `go.mod`) →
  `goreleaser/goreleaser-action@v7` with the GoReleaser binary pinned to the
  same version horde uses (`2.12.7`) and `args: release --clean`.
- Env: `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`,
  `HOMEBREW_TAP_PAT: ${{ secrets.HOMEBREW_TAP_PAT }}`.

Action majors and the `setup-go`↔`go.mod` agreement follow the lesson learned
in horde's Node-24 PR: pin `setup-go` to the version `go.mod` declares, because
`setup-go@v6` defaults `GOTOOLCHAIN=local` and will fail fast on any skew.

### 4. `.github/workflows/ci.yml`

New (orc has none today):

- Trigger: `on: pull_request: [main]` + `push: [main]`.
- Concurrency group cancel-in-progress (like horde).
- One `test` job: `actions/checkout@v6` → `actions/setup-go@v6`
  (`go-version: "1.22"`, `cache: true`) → `go vet ./...` → `make test`.
- e2e tests (`make e2e` / `e2e-docker`) are **not** in CI — they need
  `CLAUDE_CODE_OAUTH_TOKEN` and Docker, developer-local only, matching horde.

### 5. horde pin (separate repo, separate PR — Phase 2)

In `jorge-barreto/horde`, change `docker/Dockerfile`:

```dockerfile
# before
RUN ... && go install github.com/jorge-barreto/orc/cmd/orc@latest ...
# after — mirror the CLAUDE_VERSION pattern
ARG ORC_VERSION=v0.1.0
RUN ... && go install github.com/jorge-barreto/orc/cmd/orc@${ORC_VERSION} ...
```

With a "Pinned for reproducible builds. Bump with intent." comment, matching
the existing `CLAUDE_VERSION` ARG. Rebuild the worker image so the pin takes
effect.

## Details / decisions

### Detail A — Go version

orc keeps `go 1.22`. CI pins `setup-go` to `1.22` so `go.mod` and the toolchain
agree. (Bumping Go was considered and rejected as scope creep.)

### Detail B — changelog from `orc-<id>:` commits

GoReleaser generates release notes from commits between tags. orc commits are
`orc-<id>: subject`. horde's changelog `filters.exclude` (`^docs:`, `^test:`,
`^ci:`) won't match `orc-<id>:`, so **every orc commit appears** in the
changelog with its bead id. This is acceptable and arguably useful
(traceable to beads). Cleaner release notes would require a commit-convention
change — out of scope.

### Detail C — bootstrap ordering (why two PRs, orc first)

horde pinning `@v0.1.0` **requires v0.1.0 to exist first**. Rollout order:

1. **PR #1 (orc):** pieces 1–4 (version stamp, goreleaser, release.yml, ci.yml).
   Merge to orc `main`.
2. **Manual prerequisite (you):** add the `HOMEBREW_TAP_PAT` secret to the
   **orc** repo's Actions secrets. (You already hold this PAT for horde; it
   just needs to exist on the orc repo too. Without it the `brews:` push fails
   — the binaries/Release still publish.)
3. **Tag:** `git tag v0.1.0 && git push origin v0.1.0` on orc → release fires →
   binaries + checksums + GitHub Release + Homebrew formula published.
4. **PR #2 (horde):** piece 5 — flip the Dockerfile to `@v0.1.0`, rebuild the
   worker image.

PR #1 + tag are orc-repo work; PR #2 is horde-repo work, gated on v0.1.0
existing.

### Docs

- orc `README.md` install section (currently `go install …@latest`): add the
  versioned `go install …@v0.1.0` form and a `brew install jorge-barreto/tap/orc`
  line once published.
- orc `CLAUDE.md`: add a short "Releases" section documenting the `v*`-tag
  scheme and the `HOMEBREW_TAP_PAT` requirement (mirrors horde's CLAUDE.md
  release notes), so the convention is discoverable.

## Manual prerequisites (human, not automatable)

1. **`HOMEBREW_TAP_PAT` secret on the orc repo** — required for the Homebrew
   formula push (step 2 above).
2. **Homebrew tap** `jorge-barreto/homebrew-tap` already exists (horde uses it);
   GoReleaser will add an `orc.rb` formula alongside `horde.rb`. No new tap
   repo needed.

## Success criteria

- `orc --version` prints `0.1.0 (<sha>, built <date>)` from a release binary;
  `dev (...)` from a local `make build`.
- Pushing `v0.1.0` produces a GitHub Release with linux/darwin × amd64/arm64
  tar.gz archives + `checksums.txt`, and an `orc.rb` in the tap.
- `brew install jorge-barreto/tap/orc` installs a working orc.
- CI runs `go vet` + `make test` on PRs and blocks merge on failure.
- horde's worker image installs `orc@v0.1.0` (verifiable: `orc --version` in
  the image reports `0.1.0`), and orc HEAD changes no longer reach horde
  without a deliberate `ORC_VERSION` bump.

## Risk / rollback

- If the `brews:` push fails (e.g. missing PAT on orc repo), the GitHub Release
  and binaries still publish — only the Homebrew formula is missing. Fixable by
  adding the secret and re-running the release job. Low blast radius.
- Pinning horde to `@v0.1.0` is trivially reversible (revert the Dockerfile
  ARG). The orc release chain is additive — it changes nothing about how orc
  runs, only how it's built and distributed.
