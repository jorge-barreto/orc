package dispatch

import (
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestExpandVars_Simple(t *testing.T) {
	vars := map[string]string{"TICKET": "ABC-123"}
	got := ExpandVars("ticket is $TICKET", vars)
	if got != "ticket is ABC-123" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandVars_Brace(t *testing.T) {
	vars := map[string]string{"TICKET": "ABC-123"}
	got := ExpandVars("${TICKET}_suffix", vars)
	if got != "ABC-123_suffix" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandVars_EnvFallback(t *testing.T) {
	t.Setenv("ORC_TEST_VAR_XYZ", "from-env")

	vars := map[string]string{"TICKET": "t"}
	got := ExpandVars("$ORC_TEST_VAR_XYZ", vars)
	if got != "from-env" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandVars_MissingEmpty(t *testing.T) {
	vars := map[string]string{}
	got := ExpandVars("$TOTALLY_UNKNOWN_VAR_12345", vars)
	if got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandVars_NoVars(t *testing.T) {
	vars := map[string]string{"TICKET": "t"}
	input := "no variables here"
	got := ExpandVars(input, vars)
	if got != input {
		t.Fatalf("got %q", got)
	}
}

func TestExpandVars_AllVars(t *testing.T) {
	vars := map[string]string{
		"TICKET":        "T-1",
		"ARTIFACTS_DIR": "/art",
		"WORK_DIR":      "/work",
		"PROJECT_ROOT":  "/proj",
	}
	got := ExpandVars("$TICKET $ARTIFACTS_DIR $WORK_DIR $PROJECT_ROOT", vars)
	want := "T-1 /art /work /proj"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpandConfigVars_SimpleBuiltinRef(t *testing.T) {
	builtins := map[string]string{"PROJECT_ROOT": "/proj", "TICKET": "T-1"}
	vars := config.OrderedVars{
		{Key: "WORKTREE_DIR", Value: "$PROJECT_ROOT/.worktrees/$TICKET"},
	}
	result := ExpandConfigVars(vars, builtins)
	want := "/proj/.worktrees/T-1"
	if result["WORKTREE_DIR"] != want {
		t.Fatalf("WORKTREE_DIR = %q, want %q", result["WORKTREE_DIR"], want)
	}
}

func TestExpandConfigVars_Chained(t *testing.T) {
	builtins := map[string]string{"PROJECT_ROOT": "/proj"}
	vars := config.OrderedVars{
		{Key: "BASE", Value: "$PROJECT_ROOT/base"},
		{Key: "SUB", Value: "$BASE/sub"},
	}
	result := ExpandConfigVars(vars, builtins)
	if result["BASE"] != "/proj/base" {
		t.Fatalf("BASE = %q", result["BASE"])
	}
	if result["SUB"] != "/proj/base/sub" {
		t.Fatalf("SUB = %q", result["SUB"])
	}
}

func TestExpandConfigVars_Empty(t *testing.T) {
	builtins := map[string]string{"PROJECT_ROOT": "/proj"}
	result := ExpandConfigVars(nil, builtins)
	if len(result) != 0 {
		t.Fatalf("expected empty map, got %v", result)
	}
}

func TestExpandConfigVars_StaticValue(t *testing.T) {
	builtins := map[string]string{"PROJECT_ROOT": "/proj"}
	vars := config.OrderedVars{
		{Key: "MODE", Value: "production"},
	}
	result := ExpandConfigVars(vars, builtins)
	if result["MODE"] != "production" {
		t.Fatalf("MODE = %q", result["MODE"])
	}
}
