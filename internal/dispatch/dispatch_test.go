package dispatch

import (
	"strings"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
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

// Fix 6: Clone deep-copies

func TestClone_DeepCopiesCustomVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		CustomVars:   map[string]string{"A": "1", "B": "2"},
	}
	cp := env.Clone()
	cp.CustomVars["A"] = "changed"
	cp.CustomVars["C"] = "new"
	if env.CustomVars["A"] != "1" {
		t.Fatalf("original CustomVars mutated: A = %q", env.CustomVars["A"])
	}
	if _, ok := env.CustomVars["C"]; ok {
		t.Fatal("original CustomVars gained key C from clone")
	}
}

func TestClone_NilCustomVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
	}
	cp := env.Clone()
	if cp.CustomVars != nil {
		t.Fatalf("expected nil CustomVars in clone, got %v", cp.CustomVars)
	}
	// Scalar fields should still be copied
	if cp.ProjectRoot != "/proj" {
		t.Fatalf("ProjectRoot = %q", cp.ProjectRoot)
	}
}

func TestClone_DeepCopiesDefaultAllowTools(t *testing.T) {
	env := &Environment{
		ProjectRoot:       "/proj",
		WorkDir:           "/work",
		ArtifactsDir:      "/art",
		Ticket:            "T-1",
		DefaultAllowTools: []string{"Bash", "mcp__atlassian__*"},
	}
	cp := env.Clone()
	cp.DefaultAllowTools[0] = "changed"
	cp.DefaultAllowTools = append(cp.DefaultAllowTools, "extra")
	if env.DefaultAllowTools[0] != "Bash" {
		t.Fatalf("original DefaultAllowTools mutated: [0] = %q", env.DefaultAllowTools[0])
	}
	if len(env.DefaultAllowTools) != 2 {
		t.Fatalf("original DefaultAllowTools grew: len = %d", len(env.DefaultAllowTools))
	}
}

func TestClone_DeepCopiesFilteredEnv(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
	}
	// Trigger filteredEnv population
	BuildEnv(env)
	if env.filteredEnv == nil {
		t.Fatal("filteredEnv should be populated after BuildEnv")
	}

	cp := env.Clone()
	origLen := len(env.filteredEnv)
	cp.filteredEnv = append(cp.filteredEnv, "EXTRA=val")
	if len(env.filteredEnv) != origLen {
		t.Fatalf("original filteredEnv mutated: len went from %d to %d", origLen, len(env.filteredEnv))
	}
}

// Fix 8: PhaseWorkDir fallback for undefined var

func TestPhaseWorkDir_UndefinedVarFallsBack(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
	}
	phase := config.Phase{Cwd: "$UNDEFINED"}
	dir := PhaseWorkDir(phase, env)
	if dir != "/work" {
		t.Fatalf("expected fallback to WorkDir /work, got %q", dir)
	}
}

func TestPhaseWorkDir_ExpandedCwd(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		CustomVars:   map[string]string{"MY_DIR": "/custom"},
	}
	phase := config.Phase{Cwd: "$MY_DIR/sub"}
	dir := PhaseWorkDir(phase, env)
	if dir != "/custom/sub" {
		t.Fatalf("expected /custom/sub, got %q", dir)
	}
}

func TestBuildAgentArgs_IncludesDefaultTools(t *testing.T) {
	phase := config.Phase{Model: "opus"}
	args := buildAgentArgs(phase, "hello", "", true, nil, nil)
	tools := toolsFromArgs(args)
	for _, want := range defaultAllowTools {
		if !contains(tools, want) {
			t.Errorf("default tool %q not found in args; tools=%v", want, tools)
		}
	}
}

func TestBuildAgentArgs_MergesPhaseTools(t *testing.T) {
	phase := config.Phase{Model: "opus", AllowTools: []string{"Bash", "NotebookEdit"}}
	args := buildAgentArgs(phase, "hello", "", true, nil, nil)
	tools := toolsFromArgs(args)
	for _, want := range append(defaultAllowTools, "Bash", "NotebookEdit") {
		if !contains(tools, want) {
			t.Errorf("tool %q not found in args; tools=%v", want, tools)
		}
	}
}

func TestBuildAgentArgs_MergesConfigTools(t *testing.T) {
	phase := config.Phase{Model: "opus"}
	configTools := []string{"mcp__atlassian__*", "Bash"}
	args := buildAgentArgs(phase, "hello", "", true, configTools, nil)
	tools := toolsFromArgs(args)
	for _, want := range append(defaultAllowTools, "mcp__atlassian__*", "Bash") {
		if !contains(tools, want) {
			t.Errorf("tool %q not found in args; tools=%v", want, tools)
		}
	}
}

func TestBuildAgentArgs_DeduplicatesTools(t *testing.T) {
	// Config, phase, and extra tools all overlap with defaults
	phase := config.Phase{Model: "opus", AllowTools: []string{"Read", "Bash"}}
	configTools := []string{"Read", "Bash"}
	args := buildAgentArgs(phase, "hello", "", true, configTools, []string{"Read", "Write"})
	tools := toolsFromArgs(args)
	count := 0
	for _, t := range tools {
		if t == "Read" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Read appeared %d times, want 1; tools=%v", count, tools)
	}
}

// toolsFromArgs extracts the tool names following --allowedTools in args.
func toolsFromArgs(args []string) []string {
	for i, a := range args {
		if a == "--allowedTools" {
			return args[i+1:]
		}
	}
	return nil
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func TestPhaseWorkDir_NoCwd(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
	}
	phase := config.Phase{}
	dir := PhaseWorkDir(phase, env)
	if dir != "/work" {
		t.Fatalf("expected /work, got %q", dir)
	}
}
