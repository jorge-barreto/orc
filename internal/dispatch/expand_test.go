package dispatch

import (
	"os"
	"testing"
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
	os.Setenv("ORC_TEST_VAR_XYZ", "from-env")
	defer os.Unsetenv("ORC_TEST_VAR_XYZ")

	vars := map[string]string{"TICKET": "t"}
	got := ExpandVars("$ORC_TEST_VAR_XYZ", vars)
	if got != "from-env" {
		t.Fatalf("got %q", got)
	}
}

func TestExpandVars_MissingEmpty(t *testing.T) {
	os.Unsetenv("TOTALLY_UNKNOWN_VAR_12345")
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
		"TICKET":       "T-1",
		"ARTIFACTS_DIR": "/art",
		"WORK_DIR":     "/work",
		"PROJECT_ROOT": "/proj",
	}
	got := ExpandVars("$TICKET $ARTIFACTS_DIR $WORK_DIR $PROJECT_ROOT", vars)
	want := "T-1 /art /work /proj"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
