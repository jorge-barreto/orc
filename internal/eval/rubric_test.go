package eval

import (
	"strings"
	"testing"
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
