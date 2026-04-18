//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var orcBinary string

// TestMain builds the orc binary once for all e2e tests.
func TestMain(m *testing.M) {
	code := runTests(m)
	os.Exit(code)
}

func runTests(m *testing.M) int {
	tmp, err := os.MkdirTemp("", "orc-e2e-bin-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "mktemp: %v\n", err)
		return 1
	}
	defer os.RemoveAll(tmp)

	orcBinary = filepath.Join(tmp, "orc")
	cmd := exec.Command("go", "build", "-o", orcBinary, "./cmd/orc/")
	cmd.Dir = ".."
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "build orc: %v\n", err)
		return 1
	}

	return m.Run()
}

// Workspace is a temporary .orc/ workspace for a single test.
type Workspace struct {
	t            *testing.T
	Dir          string // project root (tempdir)
	ArtifactsDir string // .orc/artifacts/<ticket>/
	Ticket       string
}

// NewWorkspace creates a tempdir with .orc/config.yaml from the given YAML.
// Ticket defaults to "TEST-1".
func NewWorkspace(t *testing.T, configYAML string) *Workspace {
	t.Helper()
	dir := t.TempDir()
	orcDir := filepath.Join(dir, ".orc")
	if err := os.MkdirAll(orcDir, 0o755); err != nil {
		t.Fatalf("mkdir .orc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(orcDir, "config.yaml"), []byte(configYAML), 0o644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	ticket := "TEST-1"
	return &Workspace{
		t:            t,
		Dir:          dir,
		ArtifactsDir: filepath.Join(orcDir, "artifacts", ticket),
		Ticket:       ticket,
	}
}

// WritePrompt writes a prompt file at .orc/<relpath>.
func (w *Workspace) WritePrompt(relpath, body string) {
	w.t.Helper()
	full := filepath.Join(w.Dir, ".orc", relpath)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		w.t.Fatalf("mkdir prompt: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		w.t.Fatalf("write prompt: %v", err)
	}
}

// Result is the outcome of running the orc binary.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// RunOrc invokes the orc binary with the given args from the workspace dir.
// CLAUDECODE is stripped from the child env so the binary's guard does not trip.
func (w *Workspace) RunOrc(args ...string) *Result {
	w.t.Helper()
	cmd := exec.Command(orcBinary, args...)
	cmd.Dir = w.Dir
	env := os.Environ()
	filtered := env[:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDECODE") {
			continue
		}
		filtered = append(filtered, kv)
	}
	cmd.Env = filtered
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exit := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			w.t.Fatalf("run orc: %v (stderr: %s)", err, stderr.String())
		}
	}
	return &Result{ExitCode: exit, Stdout: stdout.String(), Stderr: stderr.String()}
}

// ReadRunResult reads and parses run-result.json from the live artifacts dir.
// This is the primary e2e assertion surface: it contains exit code, overall
// status, per-phase status/cost/duration, commits, and the artifacts path.
func (w *Workspace) ReadRunResult() map[string]any {
	w.t.Helper()
	path := filepath.Join(w.ArtifactsDir, "run-result.json")
	data, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatalf("read run-result.json at %s: %v", path, err)
	}
	var rr map[string]any
	if err := json.Unmarshal(data, &rr); err != nil {
		w.t.Fatalf("parse run-result.json: %v", err)
	}
	return rr
}

// ReadState reads state.json (live or latest history entry).
func (w *Workspace) ReadState() map[string]any {
	w.t.Helper()
	return w.readHistoryJSON("state.json")
}

// ReadCosts reads costs.json (live or latest history entry).
func (w *Workspace) ReadCosts() map[string]any {
	w.t.Helper()
	return w.readHistoryJSON("costs.json")
}

// ReadTiming reads timing.json (live or latest history entry).
func (w *Workspace) ReadTiming() map[string]any {
	w.t.Helper()
	return w.readHistoryJSON("timing.json")
}

// HistoryDir returns the path of the most recent history/<timestamp>/ dir.
func (w *Workspace) HistoryDir() string {
	w.t.Helper()
	histDir := filepath.Join(w.ArtifactsDir, "history")
	entries, err := os.ReadDir(histDir)
	if err != nil {
		w.t.Fatalf("read history dir %s: %v", histDir, err)
	}
	var latest string
	for _, e := range entries {
		if e.IsDir() && e.Name() > latest {
			latest = e.Name()
		}
	}
	if latest == "" {
		w.t.Fatalf("no history entries under %s", histDir)
	}
	return filepath.Join(histDir, latest)
}

// readHistoryJSON reads a JSON file by name, preferring live artifacts dir,
// falling back to the latest history entry (completed runs archive the file).
func (w *Workspace) readHistoryJSON(name string) map[string]any {
	w.t.Helper()
	livePath := filepath.Join(w.ArtifactsDir, name)
	if data, err := os.ReadFile(livePath); err == nil {
		return parseJSONMap(w.t, livePath, data)
	}
	path := filepath.Join(w.HistoryDir(), name)
	data, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatalf("read %s: %v", path, err)
	}
	return parseJSONMap(w.t, path, data)
}

// ReadHistoryFile reads a file by relative path (live dir first, then latest
// history entry). Returns raw bytes as a string. Fails the test if missing.
func (w *Workspace) ReadHistoryFile(relPath string) string {
	w.t.Helper()
	livePath := filepath.Join(w.ArtifactsDir, relPath)
	if data, err := os.ReadFile(livePath); err == nil {
		return string(data)
	}
	path := filepath.Join(w.HistoryDir(), relPath)
	data, err := os.ReadFile(path)
	if err != nil {
		w.t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func parseJSONMap(t *testing.T, path string, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}
