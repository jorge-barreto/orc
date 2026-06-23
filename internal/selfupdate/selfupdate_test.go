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
