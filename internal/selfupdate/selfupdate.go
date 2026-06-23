// Package selfupdate downloads, verifies, and extracts orc release binaries
// from GitHub Releases. It is the reusable core behind the `orc update` command:
// resolve the latest tag, download the platform tarball + checksums.txt, verify
// the SHA-256 before extracting, and hand back a verified temp binary. All
// standard library — no third-party dependency.
package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
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

// APIBase and DownloadBase are the GitHub endpoints, overridable in tests.
var (
	APIBase      = "https://api.github.com"
	DownloadBase = "https://github.com"
)

const repoPath = "jorge-barreto/orc"

// tagPattern anchors the release-tag shape so a tag_name from the GitHub API
// (which flows into the download URL) can't carry path-traversal or other junk.
// orc tags are plain semver with an optional "v" and optional pre-release/build
// suffix (e.g. v0.3.0, 1.2.3, v0.3.0-rc1).
var tagPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+([-+][0-9A-Za-z.-]+)?$`)

// maxArtifactBytes caps how much selfupdate will read from a release artifact
// (archive download and extracted binary) to bound disk/memory use on a
// malformed or hostile-but-checksum-consistent release. orc binaries are a few
// MB; 200 MiB is far above any real release and well below an OOM/disk risk.
// It is a var (not a const) only so tests can lower it to a tiny value; do not
// mutate it in production code.
var maxArtifactBytes int64 = 200 << 20 // 200 MiB

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
	if !tagPattern.MatchString(payload.TagName) {
		return "", fmt.Errorf("selfupdate: unexpected release tag %q from GitHub", payload.TagName)
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
	b, err := readCapped(resp.Body)
	if err == errCapExceeded {
		return nil, fmt.Errorf("selfupdate: response from %s exceeds %d bytes", url, maxArtifactBytes)
	}
	return b, err
}

// errCapExceeded signals that readCapped hit maxArtifactBytes. Callers wrap it
// with a context-specific message; it is never returned to callers directly.
var errCapExceeded = fmt.Errorf("read exceeds cap")

// readCapped reads from r up to maxArtifactBytes. If the source has more bytes
// than the cap it returns errCapExceeded rather than allocating without bound.
// It reads one extra byte past the cap to distinguish "exactly the cap" from
// "over the cap".
func readCapped(r io.Reader) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, maxArtifactBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxArtifactBytes {
		return nil, errCapExceeded
	}
	return b, nil
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

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
		return fmt.Errorf("selfupdate: cannot write to %s (need write permission — re-run with elevated privileges, e.g. sudo): %w", dir, err)
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

// extractBinary reads a gzipped tar and returns the bytes of the "orc" entry.
func extractBinary(archive []byte) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
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
			b, err := readCapped(tr)
			if err == errCapExceeded {
				return nil, fmt.Errorf("orc entry exceeds %d bytes", maxArtifactBytes)
			}
			return b, err
		}
	}
}
