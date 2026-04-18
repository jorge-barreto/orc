package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/dispatch"
	"github.com/jorge-barreto/orc/internal/state"
	"github.com/jorge-barreto/orc/internal/ux"
	"github.com/jorge-barreto/orc/internal/ux/uxtest"
	cli "github.com/urfave/cli/v3"
)

func TestCheckMissingArtifacts_NoPriorPhases(t *testing.T) {
	tmpDir := t.TempDir()
	phases := []config.Phase{{Name: "plan"}}

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	checkMissingArtifacts(phases, 0, tmpDir)
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	if got != "" {
		t.Errorf("expected empty output, got: %q", got)
	}
}

func TestCheckMissingArtifacts_AllPresent(t *testing.T) {
	tmpDir := t.TempDir()
	phases := []config.Phase{{Name: "plan", Outputs: []string{"plan.md"}}}
	if err := os.WriteFile(filepath.Join(tmpDir, "plan.md"), []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	checkMissingArtifacts(phases, 1, tmpDir)
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	if got != "" {
		t.Errorf("expected empty output, got: %q", got)
	}
}

func TestCheckMissingArtifacts_SomeMissing(t *testing.T) {
	tmpDir := t.TempDir()
	phases := []config.Phase{{Name: "plan", Outputs: []string{"plan.md"}}}

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	checkMissingArtifacts(phases, 1, tmpDir)
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	if !strings.Contains(got, "plan.md") {
		t.Errorf("expected output to contain %q, got: %q", "plan.md", got)
	}
	if !strings.Contains(got, "plan") {
		t.Errorf("expected output to contain %q, got: %q", "plan", got)
	}
}

func TestCheckMissingArtifacts_MultiplePriorPhases(t *testing.T) {
	tmpDir := t.TempDir()
	phases := []config.Phase{
		{Name: "plan", Outputs: []string{"plan.md"}},
		{Name: "code", Outputs: []string{"code.md"}},
		{Name: "review"},
	}

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	checkMissingArtifacts(phases, 2, tmpDir)
	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	got := buf.String()

	if !strings.Contains(got, "plan.md") {
		t.Errorf("expected output to contain %q, got: %q", "plan.md", got)
	}
	if !strings.Contains(got, "code.md") {
		t.Errorf("expected output to contain %q, got: %q", "code.md", got)
	}
	if !strings.Contains(got, "phase 1: plan") {
		t.Errorf("expected output to contain %q, got: %q", "phase 1: plan", got)
	}
	if !strings.Contains(got, "phase 2: code") {
		t.Errorf("expected output to contain %q, got: %q", "phase 2: code", got)
	}
}

func TestCheckMissingArtifacts_QuietMode_EmitsJSONL(t *testing.T) {
	origQuiet := ux.QuietMode
	t.Cleanup(func() { ux.QuietMode = origQuiet })
	ux.QuietMode = true

	tmpDir := t.TempDir()
	phases := []config.Phase{
		{Name: "plan", Outputs: []string{"plan.md"}},
		{Name: "code", Outputs: []string{"code.md"}},
		{Name: "review"},
	}

	// Capture stdout (QuietPhaseEvent writes there)
	oldStdout := os.Stdout
	rOut, wOut, _ := os.Pipe()
	os.Stdout = wOut

	// Capture stderr (should be empty in quiet mode)
	oldStderr := os.Stderr
	rErr, wErr, _ := os.Pipe()
	os.Stderr = wErr

	checkMissingArtifacts(phases, 2, tmpDir)

	wOut.Close()
	os.Stdout = oldStdout
	wErr.Close()
	os.Stderr = oldStderr

	var bufOut, bufErr bytes.Buffer
	io.Copy(&bufOut, rOut)
	io.Copy(&bufErr, rErr)

	// stderr must be empty — no raw text in quiet mode
	if bufErr.String() != "" {
		t.Errorf("expected no stderr output in quiet mode, got: %q", bufErr.String())
	}

	// stdout must contain 2 JSONL lines (plan.md + code.md both missing)
	lines := strings.Split(strings.TrimSpace(bufOut.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines (one per missing artifact), got %d: %q", len(lines), bufOut.String())
	}

	for i, line := range lines {
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\nline: %s", i, err, line)
		}
		if event["status"] != "warning" {
			t.Errorf("line %d: status = %v, want \"warning\"", i, event["status"])
		}
		if _, ok := event["artifact"]; !ok {
			t.Errorf("line %d: missing \"artifact\" key", i)
		}
	}

	var first, second map[string]interface{}
	json.Unmarshal([]byte(lines[0]), &first)
	json.Unmarshal([]byte(lines[1]), &second)
	if first["artifact"] != "plan.md" || first["phase"] != "plan" {
		t.Errorf("first event: got artifact=%v phase=%v, want plan.md/plan", first["artifact"], first["phase"])
	}
	if second["artifact"] != "code.md" || second["phase"] != "code" {
		t.Errorf("second event: got artifact=%v phase=%v, want code.md/code", second["artifact"], second["phase"])
	}
}

func TestOrcTest_WithHooks_PreRunFail(t *testing.T) {
	tmpDir := t.TempDir()
	dispatchSentinel := filepath.Join(tmpDir, "dispatch-ran")
	postRunSentinel := filepath.Join(tmpDir, "postrun-ran")

	phase := config.Phase{
		Name:    "check",
		Type:    "script",
		Run:     "touch " + dispatchSentinel,
		PreRun:  "exit 1",
		PostRun: "touch " + postRunSentinel,
	}

	artifactsDir := filepath.Join(tmpDir, "artifacts")
	if err := state.EnsureDir(artifactsDir); err != nil {
		t.Fatal(err)
	}

	env := &dispatch.Environment{
		ProjectRoot:  tmpDir,
		WorkDir:      tmpDir,
		ArtifactsDir: artifactsDir,
		Ticket:       "TEST-1",
		PhaseIndex:   0,
		PhaseCount:   1,
	}

	result, err := dispatch.DispatchWithHooks(context.Background(), phase, env, dispatch.Dispatch)
	if err != nil {
		t.Fatal(err)
	}

	if result.ExitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", result.ExitCode)
	}
	if _, err := os.Stat(dispatchSentinel); !os.IsNotExist(err) {
		t.Fatal("dispatch should not run when pre-run fails")
	}
	if _, err := os.Stat(postRunSentinel); os.IsNotExist(err) {
		t.Fatal("post-run should run even when pre-run fails")
	}
}

func TestOrcTest_WithHooks_DispatchFailPostRunStillRuns(t *testing.T) {
	tmpDir := t.TempDir()
	postRunSentinel := filepath.Join(tmpDir, "postrun-ran")

	phase := config.Phase{
		Name:    "check",
		Type:    "script",
		Run:     "exit 42",
		PreRun:  "echo pre-run-ok",
		PostRun: "touch " + postRunSentinel,
	}

	artifactsDir := filepath.Join(tmpDir, "artifacts")
	if err := state.EnsureDir(artifactsDir); err != nil {
		t.Fatal(err)
	}

	env := &dispatch.Environment{
		ProjectRoot:  tmpDir,
		WorkDir:      tmpDir,
		ArtifactsDir: artifactsDir,
		Ticket:       "TEST-1",
		PhaseIndex:   0,
		PhaseCount:   1,
	}

	result, err := dispatch.DispatchWithHooks(context.Background(), phase, env, dispatch.Dispatch)
	if err != nil {
		t.Fatal(err)
	}

	if result.ExitCode != 42 {
		t.Fatalf("expected exit code 42 from dispatch failure, got %d", result.ExitCode)
	}
	if _, err := os.Stat(postRunSentinel); os.IsNotExist(err) {
		t.Fatal("post-run should run even when dispatch fails")
	}
}

func TestOrcTest_WithHooks_PostRunFailOverridesSuccess(t *testing.T) {
	tmpDir := t.TempDir()

	phase := config.Phase{
		Name:    "check",
		Type:    "script",
		Run:     "echo ok",
		PostRun: "exit 7",
	}

	artifactsDir := filepath.Join(tmpDir, "artifacts")
	if err := state.EnsureDir(artifactsDir); err != nil {
		t.Fatal(err)
	}

	env := &dispatch.Environment{
		ProjectRoot:  tmpDir,
		WorkDir:      tmpDir,
		ArtifactsDir: artifactsDir,
		Ticket:       "TEST-1",
		PhaseIndex:   0,
		PhaseCount:   1,
	}

	result, err := dispatch.DispatchWithHooks(context.Background(), phase, env, dispatch.Dispatch)
	if err != nil {
		t.Fatal(err)
	}

	if result.ExitCode != 7 {
		t.Fatalf("expected exit code 7 (post-run overrides dispatch success), got %d", result.ExitCode)
	}
}

func TestOrcTest_HooksNotRun(t *testing.T) {
	sentinel := filepath.Join(t.TempDir(), "hook-ran")
	phase := config.Phase{
		Name:    "check",
		Type:    "script",
		Run:     "echo ok",
		PreRun:  "touch " + sentinel,
		PostRun: "touch " + sentinel + ".post",
	}

	artifactsDir := filepath.Join(t.TempDir(), "artifacts")
	if err := state.EnsureDir(artifactsDir); err != nil {
		t.Fatal(err)
	}

	env := &dispatch.Environment{
		ProjectRoot:  t.TempDir(),
		WorkDir:      t.TempDir(),
		ArtifactsDir: artifactsDir,
		Ticket:       "TEST-1",
		PhaseIndex:   0,
		PhaseCount:   1,
	}

	result, err := dispatch.Dispatch(context.Background(), phase, env)
	if err != nil {
		t.Fatalf("dispatch.Dispatch returned error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", result.ExitCode)
	}

	if _, err := os.Stat(sentinel); !os.IsNotExist(err) {
		t.Fatal("orc test calls dispatch.Dispatch directly, not dispatchWithHooks — pre-run hook must not run")
	}
	if _, err := os.Stat(sentinel + ".post"); !os.IsNotExist(err) {
		t.Fatal("orc test calls dispatch.Dispatch directly, not dispatchWithHooks — post-run hook must not run")
	}
}

func TestTestCmd_HeadlessFlagActivatesQuietMode(t *testing.T) {
	uxtest.SaveState(t)
	dir := t.TempDir()
	orcDir := filepath.Join(dir, ".orc")
	if err := os.MkdirAll(orcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orcDir, "config.yaml"), []byte("name: test\nphases:\n  - name: a\n    type: script\n    run: echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	t.Setenv("CLAUDECODE", "")

	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{testCmd()},
	}
	// Command may succeed or fail during dispatch — we only care that
	// --headless flipped ux.QuietMode before anything else ran.
	_ = app.Run(context.Background(), []string{"orc", "test", "--headless", "a", "TEST-1"})

	if !ux.QuietMode {
		t.Fatal("expected ux.QuietMode to be true when orc test --headless is passed")
	}
}

func TestTestCmd_OrcHeadlessEnvActivatesQuietMode(t *testing.T) {
	uxtest.SaveState(t)
	dir := t.TempDir()
	orcDir := filepath.Join(dir, ".orc")
	if err := os.MkdirAll(orcDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orcDir, "config.yaml"), []byte("name: test\nphases:\n  - name: a\n    type: script\n    run: echo ok\n"), 0644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	t.Setenv("CLAUDECODE", "")
	t.Setenv("ORC_HEADLESS", "1")

	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{testCmd()},
	}
	_ = app.Run(context.Background(), []string{"orc", "test", "a", "TEST-1"})

	if !ux.QuietMode {
		t.Fatal("expected ux.QuietMode to be true when ORC_HEADLESS=1 is set for orc test")
	}
}

func TestTestCmd_PositionalDisambiguationHint_HeadlessSuppressed(t *testing.T) {
	uxtest.SaveState(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	yaml := "name: bugfix\nphases:\n  - name: plan\n    type: script\n    run: echo ok\n"
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "bugfix.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	t.Setenv("CLAUDECODE", "")

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w

	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{testCmd()},
	}
	_ = app.Run(context.Background(), []string{"orc", "test", "--headless", "bugfix", "plan", "TICKET-1"})

	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck

	got := buf.String()
	if strings.Contains(got, "hint: treating") {
		t.Errorf("expected no disambiguation hint in headless mode, got stderr: %q", got)
	}
}

func TestTestCmd_PositionalDisambiguationHint(t *testing.T) {
	uxtest.SaveState(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".orc", "workflows"), 0755); err != nil {
		t.Fatal(err)
	}
	yaml := "name: bugfix\nphases:\n  - name: plan\n    type: script\n    run: echo ok\n"
	if err := os.WriteFile(filepath.Join(dir, ".orc", "workflows", "bugfix.yaml"), []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig) //nolint:errcheck

	t.Setenv("CLAUDECODE", "")

	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w

	app := &cli.Command{
		Name:     "orc",
		Commands: []*cli.Command{testCmd()},
	}
	_ = app.Run(context.Background(), []string{"orc", "test", "bugfix", "plan", "TICKET-1"})

	w.Close()
	os.Stderr = oldStderr
	var buf bytes.Buffer
	io.Copy(&buf, r) //nolint:errcheck

	got := buf.String()
	if !strings.Contains(got, `hint: treating "bugfix" as workflow name`) {
		t.Errorf("expected hint about treating %q as workflow name, got: %q", "bugfix", got)
	}
	if !strings.Contains(got, "matched .orc/workflows/bugfix.yaml") {
		t.Errorf("expected matched path in hint, got: %q", got)
	}
	if !strings.Contains(got, "use -w to be explicit") {
		t.Errorf("expected -w suggestion in hint, got: %q", got)
	}
}
