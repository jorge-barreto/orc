package eval

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFilteredEnv_StripsCLAUDECODE(t *testing.T) {
	t.Setenv("CLAUDECODE_TEST", "val")
	result := filteredEnv()
	for _, e := range result {
		if strings.HasPrefix(e, "CLAUDECODE") {
			t.Fatalf("CLAUDECODE var not stripped: %s", e)
		}
	}
}

func TestFilteredEnv_StripsORC(t *testing.T) {
	t.Setenv("ORC_PHASE_INDEX", "1")
	result := filteredEnv()
	for _, e := range result {
		if strings.HasPrefix(e, "ORC_") {
			t.Fatalf("ORC_ var not stripped: %s", e)
		}
	}
}

func TestFilteredEnv_StripsBuiltins(t *testing.T) {
	t.Setenv("TICKET", "T-1")
	t.Setenv("ARTIFACTS_DIR", "/a")
	t.Setenv("WORK_DIR", "/w")
	t.Setenv("PROJECT_ROOT", "/p")
	t.Setenv("WORKFLOW", "wf")
	result := filteredEnv()
	builtins := map[string]bool{
		"TICKET": true, "ARTIFACTS_DIR": true, "WORK_DIR": true,
		"PROJECT_ROOT": true, "WORKFLOW": true,
	}
	for _, e := range result {
		key := strings.SplitN(e, "=", 2)[0]
		if builtins[key] {
			t.Fatalf("builtin var not stripped: %s", e)
		}
	}
}

func TestFilteredEnv_PassthroughNonStripped(t *testing.T) {
	t.Setenv("MY_CUSTOM_VAR", "hello")
	result := filteredEnv()
	found := false
	for _, e := range result {
		if e == "MY_CUSTOM_VAR=hello" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("MY_CUSTOM_VAR=hello not found in filteredEnv output")
	}
}

func TestFilteredEnv_AppendsExtras(t *testing.T) {
	result := filteredEnv("FOO=bar", "BAZ=qux")
	foundFoo, foundBaz := false, false
	for _, e := range result {
		if e == "FOO=bar" {
			foundFoo = true
		}
		if e == "BAZ=qux" {
			foundBaz = true
		}
	}
	if !foundFoo {
		t.Fatal("FOO=bar not found in filteredEnv output")
	}
	if !foundBaz {
		t.Fatal("BAZ=qux not found in filteredEnv output")
	}
}

func TestEvaluateRubric_ScriptPass(t *testing.T) {
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "exits-zero", Check: "true", Expect: "exit 0", Weight: 1},
	}}
	dir := t.TempDir()
	results, err := EvaluateRubric(context.Background(), rubric, dir, dir, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if !r.Pass || r.Score != 1.0 || r.Name != "exits-zero" || r.Detail != "exit 0" {
		t.Fatalf("result = %+v, want pass=true score=1 detail='exit 0'", r)
	}
}

func TestEvaluateRubric_ScriptFail_WrongExitCode(t *testing.T) {
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "exits-zero", Check: "false", Expect: "exit 0", Weight: 1},
	}}
	dir := t.TempDir()
	results, err := EvaluateRubric(context.Background(), rubric, dir, dir, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	r := results[0]
	if r.Pass || r.Score != 0 || r.Detail != "exit 1" {
		t.Fatalf("result = %+v, want pass=false score=0 detail='exit 1'", r)
	}
}

func TestEvaluateRubric_ScriptPass_ExpectedNonzeroExit(t *testing.T) {
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "expects-2", Check: "exit 2", Expect: "exit 2", Weight: 1},
	}}
	dir := t.TempDir()
	results, err := EvaluateRubric(context.Background(), rubric, dir, dir, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].Pass || results[0].Detail != "exit 2" {
		t.Fatalf("result = %+v, want pass=true detail='exit 2'", results[0])
	}
}

func TestEvaluateRubric_ContextCancelledBeforeRun(t *testing.T) {
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "should-skip", Check: "true", Expect: "exit 0", Weight: 1},
	}}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results, err := EvaluateRubric(ctx, rubric, dir, dir, dir)
	if err == nil {
		t.Fatal("expected ctx error, got nil")
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0 (loop should exit before evaluating)", len(results))
	}
}

func TestEvaluateRubric_ContextCancelledMidRun(t *testing.T) {
	// Two criteria: the first runs a long-sleeping script, the second should
	// never execute because ctx is cancelled during/after the first.
	rubric := &Rubric{Criteria: []Criterion{
		{Name: "slow", Check: "sleep 30", Expect: "exit 0", Weight: 1},
		{Name: "never", Check: "true", Expect: "exit 0", Weight: 1},
	}}
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)

	start := time.Now()
	results, err := EvaluateRubric(ctx, rubric, dir, dir, dir)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected ctx error, got nil")
	}
	if elapsed > 6*time.Second {
		t.Fatalf("EvaluateRubric took %v after cancel, want <6s (WaitDelay=5s)", elapsed)
	}
	// First criterion may or may not be recorded depending on timing; second must
	// never have run (its Name must not appear in results).
	for _, r := range results {
		if r.Name == "never" {
			t.Fatalf("second criterion ran despite ctx cancel: %+v", r)
		}
	}
}

func TestEvaluateRubric_EmptyRubric(t *testing.T) {
	rubric := &Rubric{Criteria: nil}
	dir := t.TempDir()
	results, err := EvaluateRubric(context.Background(), rubric, dir, dir, dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("got %d results, want 0", len(results))
	}
}

func TestScoreRegex_ExtractsLastScore(t *testing.T) {
	// Mirrors the rubric.go judge-path logic that takes the final SCORE match.
	output := "reasoning...\nSCORE: 3\nmore text\nSCORE: 8\n"
	matches := scoreRegex.FindAllStringSubmatch(output, -1)
	if len(matches) != 2 {
		t.Fatalf("got %d matches, want 2", len(matches))
	}
	last := matches[len(matches)-1][1]
	if last != "8" {
		t.Fatalf("last score = %q, want %q", last, "8")
	}
}

func TestScoreRegex_NoMatch(t *testing.T) {
	output := "no numeric score here\n"
	matches := scoreRegex.FindAllStringSubmatch(output, -1)
	if len(matches) != 0 {
		t.Fatalf("got %d matches, want 0", len(matches))
	}
}
