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
