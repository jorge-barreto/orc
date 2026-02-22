package dispatch

import (
	"strings"
	"testing"
)

func TestVars_AllKeys(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
	}
	vars := env.Vars()
	if vars["TICKET"] != "T-1" {
		t.Fatalf("TICKET = %q", vars["TICKET"])
	}
	if vars["ARTIFACTS_DIR"] != "/art" {
		t.Fatalf("ARTIFACTS_DIR = %q", vars["ARTIFACTS_DIR"])
	}
	if vars["WORK_DIR"] != "/work" {
		t.Fatalf("WORK_DIR = %q", vars["WORK_DIR"])
	}
	if vars["PROJECT_ROOT"] != "/proj" {
		t.Fatalf("PROJECT_ROOT = %q", vars["PROJECT_ROOT"])
	}
	if len(vars) != 4 {
		t.Fatalf("expected 4 keys, got %d", len(vars))
	}
}

func TestBuildEnv_OrcVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		PhaseIndex:   2,
		PhaseCount:   5,
	}
	result := BuildEnv(env)

	find := func(prefix string) string {
		for _, e := range result {
			if strings.HasPrefix(e, prefix+"=") {
				return strings.TrimPrefix(e, prefix+"=")
			}
		}
		return ""
	}

	if v := find("ORC_TICKET"); v != "T-1" {
		t.Fatalf("ORC_TICKET = %q", v)
	}
	if v := find("ORC_ARTIFACTS_DIR"); v != "/art" {
		t.Fatalf("ORC_ARTIFACTS_DIR = %q", v)
	}
	if v := find("ORC_WORK_DIR"); v != "/work" {
		t.Fatalf("ORC_WORK_DIR = %q", v)
	}
	if v := find("ORC_PROJECT_ROOT"); v != "/proj" {
		t.Fatalf("ORC_PROJECT_ROOT = %q", v)
	}
	if v := find("ORC_PHASE_INDEX"); v != "2" {
		t.Fatalf("ORC_PHASE_INDEX = %q", v)
	}
	if v := find("ORC_PHASE_COUNT"); v != "5" {
		t.Fatalf("ORC_PHASE_COUNT = %q", v)
	}
}

func TestVars_IncludesCustomVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		CustomVars:   map[string]string{"MY_DIR": "/proj/sub"},
	}
	vars := env.Vars()
	if vars["MY_DIR"] != "/proj/sub" {
		t.Fatalf("MY_DIR = %q", vars["MY_DIR"])
	}
	// Built-ins still present
	if vars["TICKET"] != "T-1" {
		t.Fatalf("TICKET = %q", vars["TICKET"])
	}
	if len(vars) != 5 {
		t.Fatalf("expected 5 keys, got %d", len(vars))
	}
}

func TestBuildEnv_IncludesCustomVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		CustomVars:   map[string]string{"MY_DIR": "/proj/sub"},
	}
	result := BuildEnv(env)
	found := false
	for _, e := range result {
		if e == "ORC_MY_DIR=/proj/sub" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("ORC_MY_DIR not found in BuildEnv output")
	}
}

func TestBuildEnv_StripsCLAUDECODE(t *testing.T) {
	t.Setenv("CLAUDECODE_TEST", "should-be-stripped")

	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
	}
	result := BuildEnv(env)
	for _, e := range result {
		if strings.HasPrefix(e, "CLAUDECODE") {
			t.Fatalf("CLAUDECODE var not stripped: %s", e)
		}
	}
}
