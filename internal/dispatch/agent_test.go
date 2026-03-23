package dispatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
	"github.com/jorge-barreto/orc/internal/state"
)

type dispatchCall struct {
	prompt    string
	sessionID string
	isFirst   bool
}

func TestRenderAndSavePrompt_InjectsFeedback(t *testing.T) {
	dir := t.TempDir()
	artDir := filepath.Join(dir, "artifacts")
	if err := state.EnsureDir(artDir); err != nil {
		t.Fatal(err)
	}

	// Write a minimal prompt template file
	promptDir := filepath.Join(dir, ".orc", "phases")
	os.MkdirAll(promptDir, 0755)
	promptFile := filepath.Join(promptDir, "implement.md")
	os.WriteFile(promptFile, []byte("Implement ticket $TICKET in $WORK_DIR."), 0644)

	// Write feedback from a previous failed phase
	if err := state.WriteFeedback(artDir, "review", "review found bugs: missing error handling"); err != nil {
		t.Fatal(err)
	}

	env := &Environment{
		ProjectRoot:  dir,
		WorkDir:      "/work",
		ArtifactsDir: artDir,
		Ticket:       "TEST-42",
		PhaseIndex:   0,
	}
	phase := config.Phase{
		Name:   "implement",
		Type:   "agent",
		Prompt: ".orc/phases/implement.md",
	}

	rendered, err := RenderAndSavePrompt(phase, env)
	if err != nil {
		t.Fatal(err)
	}

	// Verify variable expansion worked
	if !strings.Contains(rendered, "TEST-42") {
		t.Fatalf("rendered prompt missing expanded TICKET; got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "/work") {
		t.Fatalf("rendered prompt missing expanded WORK_DIR; got:\n%s", rendered)
	}

	// Verify feedback was injected into the rendered prompt
	if !strings.Contains(rendered, "--- Feedback from review ---") {
		t.Fatalf("rendered prompt missing feedback header; got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "review found bugs: missing error handling") {
		t.Fatalf("rendered prompt missing feedback content; got:\n%s", rendered)
	}

	// Verify the prompt was saved to the correct artifacts/prompts/ path
	savedPath := state.PromptPath(artDir, 0)
	savedData, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("prompt file not saved: %v", err)
	}
	saved := string(savedData)
	if saved != rendered {
		t.Fatalf("saved prompt differs from returned prompt;\nsaved:\n%s\nreturned:\n%s", saved, rendered)
	}
	if !strings.Contains(saved, "review found bugs: missing error handling") {
		t.Fatalf("saved prompt file missing feedback; got:\n%s", saved)
	}
}

func TestRenderAndSavePrompt_NoFeedback(t *testing.T) {
	dir := t.TempDir()
	artDir := filepath.Join(dir, "artifacts")
	if err := state.EnsureDir(artDir); err != nil {
		t.Fatal(err)
	}

	promptDir := filepath.Join(dir, ".orc", "phases")
	os.MkdirAll(promptDir, 0755)
	promptFile := filepath.Join(promptDir, "plan.md")
	os.WriteFile(promptFile, []byte("Plan the work for $TICKET."), 0644)

	env := &Environment{
		ProjectRoot:  dir,
		WorkDir:      "/work",
		ArtifactsDir: artDir,
		Ticket:       "TEST-1",
		PhaseIndex:   0,
	}
	phase := config.Phase{
		Name:   "plan",
		Type:   "agent",
		Prompt: ".orc/phases/plan.md",
	}

	rendered, err := RenderAndSavePrompt(phase, env)
	if err != nil {
		t.Fatal(err)
	}

	// No feedback should be appended
	if strings.Contains(rendered, "Feedback") {
		t.Fatalf("rendered prompt should not contain feedback; got:\n%s", rendered)
	}
	if rendered != "Plan the work for TEST-1." {
		t.Fatalf("unexpected rendered prompt: %q", rendered)
	}
}

func TestRenderAndSavePrompt_MissingFile_ErrorContext(t *testing.T) {
	dir := t.TempDir()
	artDir := filepath.Join(dir, "artifacts")
	if err := state.EnsureDir(artDir); err != nil {
		t.Fatal(err)
	}
	env := &Environment{
		ProjectRoot:  dir,
		ArtifactsDir: artDir,
		PhaseIndex:   0,
	}
	phase := config.Phase{
		Name:   "implement",
		Type:   "agent",
		Prompt: ".orc/phases/nonexistent.md",
	}
	_, err := RenderAndSavePrompt(phase, env)
	if err == nil {
		t.Fatal("expected error for missing prompt file, got nil")
	}
	if !strings.Contains(err.Error(), "reading prompt template") {
		t.Errorf("error %q does not contain 'reading prompt template'", err.Error())
	}
}

func TestDispatchWithResume_FreshStart(t *testing.T) {
	var calls []dispatchCall
	renderCalled := false

	tr, sessionID, isFirst, err := dispatchWithResume(
		"",
		func() (string, error) {
			renderCalled = true
			return "hello", nil
		},
		func() string { return "new-sid" },
		func(prompt, sid string, first bool) (*turnResult, error) {
			calls = append(calls, dispatchCall{prompt, sid, first})
			return &turnResult{Stream: &StreamResult{Text: "ok"}, ExitCode: 0}, nil
		},
		func(err error) { t.Fatal("should not warn") },
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !renderCalled {
		t.Fatal("renderPrompt should have been called")
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0] != (dispatchCall{"hello", "new-sid", true}) {
		t.Errorf("unexpected call: %+v", calls[0])
	}
	if sessionID != "new-sid" {
		t.Errorf("expected sessionID=new-sid, got %q", sessionID)
	}
	if !isFirst {
		t.Error("expected isFirst=true")
	}
	if tr == nil || tr.Stream == nil || tr.Stream.Text != "ok" {
		t.Errorf("unexpected turn result: %+v", tr)
	}
}

func TestDispatchWithResume_ResumeSucceeds(t *testing.T) {
	var calls []dispatchCall

	tr, sessionID, isFirst, err := dispatchWithResume(
		"sess-123",
		func() (string, error) { t.Fatal("renderPrompt should not be called"); return "", nil },
		func() string { return "new-sid" },
		func(prompt, sid string, first bool) (*turnResult, error) {
			calls = append(calls, dispatchCall{prompt, sid, first})
			return &turnResult{Stream: &StreamResult{Text: "resumed"}, ExitCode: 0}, nil
		},
		func(err error) { t.Fatal("should not warn") },
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0] != (dispatchCall{resumePrompt, "sess-123", false}) {
		t.Errorf("unexpected call: %+v", calls[0])
	}
	if sessionID != "sess-123" {
		t.Errorf("expected sessionID=sess-123, got %q", sessionID)
	}
	if isFirst {
		t.Error("expected isFirst=false")
	}
	if tr == nil || tr.Stream == nil || tr.Stream.Text != "resumed" {
		t.Errorf("unexpected turn result: %+v", tr)
	}
}

func TestDispatchWithResume_ResumeFails_Error_FallsBack(t *testing.T) {
	var calls []dispatchCall
	var warnErr error
	callCount := 0

	_, sessionID, isFirst, err := dispatchWithResume(
		"sess-fail",
		func() (string, error) { return "fresh prompt", nil },
		func() string { return "new-sid" },
		func(prompt, sid string, first bool) (*turnResult, error) {
			calls = append(calls, dispatchCall{prompt, sid, first})
			callCount++
			if callCount == 1 {
				return nil, fmt.Errorf("connection refused")
			}
			return &turnResult{ExitCode: 0}, nil
		},
		func(e error) { warnErr = e },
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0] != (dispatchCall{resumePrompt, "sess-fail", false}) {
		t.Errorf("unexpected first call: %+v", calls[0])
	}
	if calls[1] != (dispatchCall{"fresh prompt", "new-sid", true}) {
		t.Errorf("unexpected second call: %+v", calls[1])
	}
	if warnErr == nil || !strings.Contains(warnErr.Error(), "connection refused") {
		t.Errorf("expected warn with 'connection refused', got: %v", warnErr)
	}
	if !isFirst {
		t.Error("expected isFirst=true")
	}
	if sessionID != "new-sid" {
		t.Errorf("expected sessionID=new-sid, got %q", sessionID)
	}
}

func TestDispatchWithResume_ResumeFails_NonZeroExit_FallsBack(t *testing.T) {
	var calls []dispatchCall
	var warnErr error
	callCount := 0

	_, _, isFirst, err := dispatchWithResume(
		"sess-exit1",
		func() (string, error) { return "fresh prompt", nil },
		func() string { return "new-sid" },
		func(prompt, sid string, first bool) (*turnResult, error) {
			calls = append(calls, dispatchCall{prompt, sid, first})
			callCount++
			if callCount == 1 {
				return &turnResult{ExitCode: 1}, nil
			}
			return &turnResult{ExitCode: 0}, nil
		},
		func(e error) { warnErr = e },
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].isFirst {
		t.Error("expected first call isFirst=false (resume attempt)")
	}
	if !calls[1].isFirst {
		t.Error("expected second call isFirst=true (fresh attempt)")
	}
	if warnErr == nil || !strings.Contains(warnErr.Error(), "exit code 1") {
		t.Errorf("expected warn with 'exit code 1', got: %v", warnErr)
	}
	if !isFirst {
		t.Error("expected isFirst=true")
	}
}

func TestDispatchWithResume_BothFail(t *testing.T) {
	var warnErr error
	callCount := 0

	_, _, _, err := dispatchWithResume(
		"sess-fail",
		func() (string, error) { return "fresh prompt", nil },
		func() string { return "new-sid" },
		func(prompt, sid string, first bool) (*turnResult, error) {
			callCount++
			if callCount == 1 {
				return nil, fmt.Errorf("resume failed")
			}
			return nil, fmt.Errorf("fresh failed")
		},
		func(e error) { warnErr = e },
	)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "fresh failed" {
		t.Errorf("expected 'fresh failed', got %q", err.Error())
	}
	if warnErr == nil || !strings.Contains(warnErr.Error(), "resume failed") {
		t.Errorf("expected warn with 'resume failed', got: %v", warnErr)
	}
}

func TestDispatchWithResume_RenderPromptFails(t *testing.T) {
	var calls []dispatchCall

	_, _, _, err := dispatchWithResume(
		"sess-fail",
		func() (string, error) { return "", fmt.Errorf("template not found") },
		func() string { return "new-sid" },
		func(prompt, sid string, first bool) (*turnResult, error) {
			calls = append(calls, dispatchCall{prompt, sid, first})
			return nil, fmt.Errorf("resume error")
		},
		func(e error) {},
	)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if err.Error() != "template not found" {
		t.Errorf("expected 'template not found', got %q", err.Error())
	}
	if len(calls) != 1 {
		t.Errorf("expected 1 call (resume attempt), got %d", len(calls))
	}
}

func setupFakeClaudeForResume(t *testing.T, alwaysSucceed bool) string {
	t.Helper()
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	os.MkdirAll(binDir, 0755)
	claudePath := filepath.Join(binDir, "claude")

	var script string
	if alwaysSucceed {
		script = `#!/bin/bash
echo '{"type":"result","total_cost_usd":0.02,"session_id":"kept-sess","usage":{"input_tokens":200,"output_tokens":100}}'
exit 0
`
	} else {
		script = `#!/bin/bash
for arg in "$@"; do
  if [ "$arg" = "--resume" ]; then
    echo "resume: session not found" >&2
    exit 1
  fi
done
echo '{"type":"result","total_cost_usd":0.01,"session_id":"fresh-sess","usage":{"input_tokens":100,"output_tokens":50}}'
exit 0
`
	}

	os.WriteFile(claudePath, []byte(script), 0755)
	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))
	return dir
}

func makeIntegrationEnv(t *testing.T, dir string, resumeSessionID string) (config.Phase, *Environment) {
	t.Helper()
	artDir := filepath.Join(dir, "artifacts")
	state.EnsureDir(artDir)

	promptDir := filepath.Join(dir, ".orc", "phases")
	os.MkdirAll(promptDir, 0755)
	os.WriteFile(filepath.Join(promptDir, "test.md"), []byte("Do the work."), 0644)

	phase := config.Phase{
		Name:   "test-agent",
		Type:   "agent",
		Prompt: ".orc/phases/test.md",
		Model:  "sonnet",
		Effort: "low",
	}
	env := &Environment{
		ProjectRoot:     dir,
		WorkDir:         dir,
		ArtifactsDir:    artDir,
		Ticket:          "TEST-1",
		PhaseIndex:      0,
		ResumeSessionID: resumeSessionID,
		AutoMode:        true,
	}
	return phase, env
}

func TestRunAgent_ResumeFallback_Integration(t *testing.T) {
	dir := setupFakeClaudeForResume(t, false)
	phase, env := makeIntegrationEnv(t, dir, "sess-to-fail")

	result, err := RunAgent(context.Background(), phase, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if result.CostUSD <= 0 {
		t.Errorf("expected CostUSD > 0, got %f", result.CostUSD)
	}
	if result.SessionID == "sess-to-fail" {
		t.Errorf("expected fallback to new session, got sess-to-fail")
	}
}

func TestRunAgent_ResumeSucceeds_Integration(t *testing.T) {
	dir := setupFakeClaudeForResume(t, true)
	phase, env := makeIntegrationEnv(t, dir, "sess-keep")

	result, err := RunAgent(context.Background(), phase, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SessionID != "sess-keep" {
		t.Errorf("expected sessionID=sess-keep, got %q", result.SessionID)
	}
}

func TestRunAgentWithPrompt_SetsSessionID(t *testing.T) {
	dir := setupFakeClaudeForResume(t, true)
	phase, env := makeIntegrationEnv(t, dir, "")

	result, err := RunAgentWithPrompt(context.Background(), phase, env, "test prompt", "test-session-id")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SessionID != "test-session-id" {
		t.Errorf("expected sessionID=test-session-id, got %q", result.SessionID)
	}
}

func TestRunAgentAttended_ResumeFallback_Integration(t *testing.T) {
	dir := setupFakeClaudeForResume(t, false)
	phase, env := makeIntegrationEnv(t, dir, "sess-to-fail")
	env.AutoMode = false

	origStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	w.Close() // EOF immediately
	defer func() { os.Stdin = origStdin; r.Close() }()

	result, err := RunAgentAttended(context.Background(), phase, env)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SessionID == "sess-to-fail" {
		t.Errorf("expected fallback to new session, got sess-to-fail")
	}
	if result.Turns != 1 {
		t.Errorf("expected 1 turn, got %d", result.Turns)
	}
}
