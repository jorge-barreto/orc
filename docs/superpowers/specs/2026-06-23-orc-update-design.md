# `orc update` self-updater — Design

**Bead:** orc-29y.6 (Distribution & Packaging)

**Goal:** `orc update` upgrades the running binary in place from the latest
GitHub Release, verifying the download against `checksums.txt` before touching
anything, and refusing to clobber package-manager-managed installs.

## Problem

orc has no self-update path. Users on a binary install (`curl | sh` via
`scripts/install.sh`, or a downloaded tarball) must manually re-run the
installer or `go install …@latest` to upgrade. There is no in-tool "bring me to
the latest version" command.

## CLI surface

- `orc update` — resolve latest, download, verify, replace. **Non-interactive**
  (no y/n prompt; running it is the consent).
- `orc update --check` — report current vs. latest and whether an update is
  available; download and replace **nothing**.
- `orc upgrade` — alias for `update` (urfave/cli `Aliases`).
- **No version-pin argument** — install.sh already covers pinning a specific
  version via `ORC_VERSION`; YAGNI for the self-updater.

## Components

### `internal/selfupdate/` — reusable, testable core

All standard library: `net/http`, `crypto/sha256`, `archive/tar`,
`compress/gzip`, `runtime`, `os`, `io`. No new third-party dependency.

- `ResolveLatest(ctx) (tag string, err error)`
  GET `https://api.github.com/repos/jorge-barreto/orc/releases/latest`, parse
  `tag_name`. Same source install.sh uses.
- `Download(ctx, tag, goos, goarch) (tmpPath string, err error)`
  - Build the archive name `orc_<ver-without-v>_<goos>_<goarch>.tar.gz`,
    mirroring `.goreleaser.yaml`'s `name_template`
    (`orc_{{ .Version }}_{{ .Os }}_{{ .Arch }}`).
  - Download the archive and `checksums.txt` from the release's download URL.
  - **Verify** the archive's SHA-256 against its line in `checksums.txt`
    BEFORE extracting. A mismatch (or a missing line) returns an error and
    leaves nothing permanent behind.
  - Extract the `orc` entry from the tar.gz into a temp file; return its path.
- Helpers (unexported unless a test needs them): `archiveName(ver, os, arch)`,
  `parseChecksums(data, archive) (hex string, ok bool)`, and a version-compare
  used by both `--check` and the already-latest short-circuit.

The package boundary keeps HTTP / tar / checksum logic out of the CLI file and
makes the verify path unit-testable against an `httptest.Server`.

### `cmd/orc/update.go` — command wiring

Registers the `update` command (alias `upgrade`) and the `--check` bool flag.
Responsibilities:

1. `ResolveLatest`; if already on the latest version, print and exit 0 (no
   download). For `--check`, report the delta and exit here regardless.
2. Package-manager detection (below) → redirect message + exit 0 if managed.
3. `selfupdate.Download(...)` for the running platform.
4. Atomic self-replace.
5. Print `updated v<old> → v<new>`.

### Package-manager detection (redirect, don't clobber)

Resolve the real executable: `real := filepath.EvalSymlinks(os.Executable())`.
Then, using pure stdlib path-prefix checks (no shelling out to `brew`):

- Under a Homebrew prefix (`/opt/homebrew`, `/usr/local/Cellar`, or a
  `/usr/local/…/Cellar` layout) → print "installed via Homebrew; run
  `brew upgrade orc`", exit 0 without replacing.
- Under a Go install path (`go env GOBIN` / `$GOPATH/bin` / `~/go/bin`) →
  print "installed via go install; run
  `go install github.com/jorge-barreto/orc/cmd/orc@latest`", exit 0.
- Target directory not writable → error with a `sudo` / `ORC_BIN_DIR`-style
  hint, no replace.
- Otherwise → proceed to self-replace.

### Atomic self-replace

```
exe     := os.Executable()
realExe := filepath.EvalSymlinks(exe)
dir     := filepath.Dir(realExe)
tmp     := <dir>/.orc-update-<rand>     # SAME dir → rename is atomic, same FS
write verified binary bytes to tmp; chmod 0755
os.Rename(tmp, realExe)                 # atomic; running process keeps its inode
```

The temp file lives in the **same directory** as the target so the rename can
never be cross-device. On any failure before the rename, the live binary is
untouched and the temp file is removed. This mirrors orc's existing atomic
state-write pattern (write `.tmp`, fsync, rename) and install.sh's
verify-before-install ordering.

## Data flow

```
orc update
  └─ ResolveLatest() ─ tag (v0.3.0)
  └─ compare to main.version
        already latest? → "orc is up to date (v0.3.0)", exit 0
  └─ --check? → report current/latest/available, exit 0  (no download)
  └─ PM-managed? → redirect message (brew/go), exit 0     (no replace)
  └─ Download(tag, GOOS, GOARCH):
        fetch archive + checksums.txt
        sha256(archive) == checksums line?  ── no → abort, nothing changed
        extract orc → temp
  └─ atomic rename temp over real exe
  └─ "updated v0.2.0 → v0.3.0"
```

## Error handling

| Failure | Behavior |
|---------|----------|
| Network / GitHub API unreachable | Error, exit non-zero, binary untouched |
| Already on the latest version | Clean message, exit 0 (no download) |
| Archive missing for this os/arch | Error naming the platform |
| Checksum mismatch / missing line | **Abort before any replace**; binary untouched |
| PM-managed install (brew / go) | Redirect to the correct upgrade command; no replace |
| Target dir not writable | Error with sudo / `ORC_BIN_DIR` hint; no replace |
| `--check` | Report only; never downloads or replaces |

The command's failure exit code should follow orc's existing convention for
operational/runtime errors (not the config-error code). `--version` value comes
from `main.version` (ldflags / BuildInfo fallback), so a `dev` build with no
real version compares as "unknown" and is handled gracefully (report that a
dev build can't self-update meaningfully, or proceed to latest — resolved in
the plan).

## Testing

- `internal/selfupdate` unit tests with an `httptest.Server` serving a fake
  `releases/latest` JSON, a real gzipped tar containing a stub `orc` file, and
  matching / mismatching `checksums.txt`:
  - `ResolveLatest` parses the tag.
  - `Download` succeeds on a good checksum and returns a usable temp binary.
  - `Download` **errors and writes nothing** on a bad/missing checksum.
  - `archiveName` matches the GoReleaser template for sample os/arch pairs.
  - version-compare returns the right ordering (older / equal / newer).
- Atomic-replace test against a throwaway temp "binary" file (never the test
  binary itself): rename-over works; a simulated verify-failure leaves the
  original intact.
- PM-detection test, table-driven over representative paths (Homebrew cellar,
  `~/go/bin` / GOBIN, a plain writable dir), asserting the right branch and
  message.

## Docs

- `README.md` — add `orc update` to the install / upgrade section alongside
  Homebrew, `curl | sh`, and `go install`.
- `internal/docs/content.go` — `orc docs` coverage of `update` / `--check` and
  the package-manager redirect behavior.
- `orc --help` — `update` registered with a clear `Usage` / `Description`
  (alias `upgrade`).

## Dependency constraint

No new dependencies. `net/http`, `crypto/sha256`, `archive/tar`,
`compress/gzip`, `runtime`, `os`, `io` are all stdlib — within orc's
"stdlib + yaml.v3 + urfave/cli/v3 + google/uuid" rule.

## Beads

This spec implements **orc-29y.6** ("orc update — self-updater command"), a
child of the **orc-29y** Distribution & Packaging epic.

## Out of scope

- Pinning / downgrading to an arbitrary version (use install.sh `ORC_VERSION`).
- Windows support (orc ships linux/darwin only).
- Auto-update-on-startup or background update checks.
- Changing how releases are produced — that is the auto-tagging spec
  (`2026-06-23-version-file-autotag-design.md`, bead orc-29y.7).
