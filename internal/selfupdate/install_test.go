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
