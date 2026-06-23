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
			return io.ReadAll(tr)
		}
	}
}
