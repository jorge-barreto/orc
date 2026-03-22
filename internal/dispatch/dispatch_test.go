package dispatch

import (
	"strings"
	"sync"
	"testing"

	"github.com/jorge-barreto/orc/internal/config"
)

func TestVars_AllKeys(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		Workflow:     "bugfix",
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
	if vars["WORKFLOW"] != "bugfix" {
		t.Fatalf("WORKFLOW = %q", vars["WORKFLOW"])
	}
	if len(vars) != 5 {
		t.Fatalf("expected 5 keys, got %d", len(vars))
	}
}

func TestDryRunVars_IncludesORCPrefix(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/proj",
		ArtifactsDir: "/proj/.orc/artifacts/TEST-1",
		Ticket:       "TEST-1",
		Workflow:     "default",
	}
	vars := env.DryRunVars()

	// Unprefixed keys (from Vars())
	if vars["ARTIFACTS_DIR"] != "/proj/.orc/artifacts/TEST-1" {
		t.Errorf("ARTIFACTS_DIR = %q", vars["ARTIFACTS_DIR"])
	}
	if vars["TICKET"] != "TEST-1" {
		t.Errorf("TICKET = %q", vars["TICKET"])
	}
	// ORC_-prefixed keys (added by DryRunVars)
	if vars["ORC_TICKET"] != "TEST-1" {
		t.Errorf("ORC_TICKET = %q", vars["ORC_TICKET"])
	}
	if vars["ORC_WORKFLOW"] != "default" {
		t.Errorf("ORC_WORKFLOW = %q", vars["ORC_WORKFLOW"])
	}
	if vars["ORC_ARTIFACTS_DIR"] != "/proj/.orc/artifacts/TEST-1" {
		t.Errorf("ORC_ARTIFACTS_DIR = %q", vars["ORC_ARTIFACTS_DIR"])
	}
	if vars["ORC_WORK_DIR"] != "/proj" {
		t.Errorf("ORC_WORK_DIR = %q", vars["ORC_WORK_DIR"])
	}
	if vars["ORC_PROJECT_ROOT"] != "/proj" {
		t.Errorf("ORC_PROJECT_ROOT = %q", vars["ORC_PROJECT_ROOT"])
	}
	// ORC_PHASE_INDEX and ORC_PHASE_COUNT must NOT be present
	if _, ok := vars["ORC_PHASE_INDEX"]; ok {
		t.Errorf("ORC_PHASE_INDEX should not be present in DryRunVars")
	}
	if _, ok := vars["ORC_PHASE_COUNT"]; ok {
		t.Errorf("ORC_PHASE_COUNT should not be present in DryRunVars")
	}
}

func TestBuildEnv_OrcVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		Workflow:     "bugfix",
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
	if v := find("ORC_WORKFLOW"); v != "bugfix" {
		t.Fatalf("ORC_WORKFLOW = %q", v)
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
	if len(vars) != 6 {
		t.Fatalf("expected 6 keys, got %d", len(vars))
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

func TestClone_CopiesVerbose(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		Verbose:      true,
	}
	cp := env.Clone()
	if !cp.Verbose {
		t.Fatal("Verbose not copied to clone")
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

func TestBuildAgentArgs_PromptViaStdin(t *testing.T) {
	phase := config.Phase{Model: "opus", Effort: "high"}
	env := &Environment{ProjectRoot: "/proj", WorkDir: "/work", ArtifactsDir: "/art", Ticket: "T-1"}
	args := buildAgentArgs(phase, env, "", true, nil)
	if args[0] != "-p" {
		t.Fatalf("first arg should be -p, got %q", args[0])
	}
	// -p should be a standalone flag; the next arg must not look like a prompt
	if args[1] == "" || args[1][0] != '-' {
		t.Fatalf("prompt should not be passed as CLI arg; args[1] = %q", args[1])
	}
}

func TestBuildAgentArgs_IncludesEffort(t *testing.T) {
	phase := config.Phase{Model: "opus", Effort: "high"}
	env := &Environment{ProjectRoot: "/proj", WorkDir: "/work", ArtifactsDir: "/art", Ticket: "T-1"}
	args := buildAgentArgs(phase, env, "", true, nil)
	found := false
	for i, a := range args {
		if a == "--effort" && i+1 < len(args) && args[i+1] == "high" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--effort high not found in args: %v", args)
	}
}

func TestBuildAgentArgs_IncludesDefaultTools(t *testing.T) {
	phase := config.Phase{Model: "opus", Effort: "high"}
	env := &Environment{ProjectRoot: "/proj", WorkDir: "/work", ArtifactsDir: "/art", Ticket: "T-1"}
	args := buildAgentArgs(phase, env, "", true, nil)
	tools := toolsFromArgs(args)
	for _, want := range defaultAllowTools {
		if !contains(tools, want) {
			t.Errorf("default tool %q not found in args; tools=%v", want, tools)
		}
	}
}

func TestBuildAgentArgs_MergesPhaseTools(t *testing.T) {
	phase := config.Phase{Model: "opus", Effort: "high", AllowTools: []string{"Bash", "NotebookEdit"}}
	env := &Environment{ProjectRoot: "/proj", WorkDir: "/work", ArtifactsDir: "/art", Ticket: "T-1"}
	args := buildAgentArgs(phase, env, "", true, nil)
	tools := toolsFromArgs(args)
	for _, want := range append(defaultAllowTools, "Bash", "NotebookEdit") {
		if !contains(tools, want) {
			t.Errorf("tool %q not found in args; tools=%v", want, tools)
		}
	}
}

func TestBuildAgentArgs_MergesConfigTools(t *testing.T) {
	phase := config.Phase{Model: "opus", Effort: "high"}
	env := &Environment{ProjectRoot: "/proj", WorkDir: "/work", ArtifactsDir: "/art", Ticket: "T-1",
		DefaultAllowTools: []string{"mcp__atlassian__*", "Bash"}}
	args := buildAgentArgs(phase, env, "", true, nil)
	tools := toolsFromArgs(args)
	for _, want := range append(defaultAllowTools, "mcp__atlassian__*", "Bash") {
		if !contains(tools, want) {
			t.Errorf("tool %q not found in args; tools=%v", want, tools)
		}
	}
}

func TestBuildAgentArgs_DeduplicatesTools(t *testing.T) {
	// Config, phase, and extra tools all overlap with defaults
	phase := config.Phase{Model: "opus", Effort: "high", AllowTools: []string{"Read", "Bash"}}
	env := &Environment{ProjectRoot: "/proj", WorkDir: "/work", ArtifactsDir: "/art", Ticket: "T-1",
		DefaultAllowTools: []string{"Read", "Bash"}}
	args := buildAgentArgs(phase, env, "", true, []string{"Read", "Write"})
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

func TestBuildAgentArgs_MCPConfig(t *testing.T) {
	phase := config.Phase{Model: "opus", Effort: "high", MCPConfig: "$ARTIFACTS_DIR/mcp.json"}
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
	}
	args := buildAgentArgs(phase, env, "", true, nil)
	// Find --mcp-config in args
	found := false
	for i, a := range args {
		if a == "--mcp-config" && i+1 < len(args) {
			if args[i+1] != "/art/mcp.json" {
				t.Fatalf("--mcp-config value = %q, want /art/mcp.json", args[i+1])
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("--mcp-config not found in args: %v", args)
	}
}

func TestBuildAgentArgs_MCPConfigEmpty(t *testing.T) {
	phase := config.Phase{Model: "opus", Effort: "high"}
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
	}
	args := buildAgentArgs(phase, env, "", true, nil)
	for _, a := range args {
		if a == "--mcp-config" {
			t.Fatalf("--mcp-config should not appear when MCPConfig is empty; args: %v", args)
		}
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

func TestBuildEnv_UnprefixedVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		Workflow:     "bugfix",
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

	if v := find("TICKET"); v != "T-1" {
		t.Fatalf("TICKET = %q", v)
	}
	if v := find("WORKFLOW"); v != "bugfix" {
		t.Fatalf("WORKFLOW = %q", v)
	}
	if v := find("ARTIFACTS_DIR"); v != "/art" {
		t.Fatalf("ARTIFACTS_DIR = %q", v)
	}
	if v := find("WORK_DIR"); v != "/work" {
		t.Fatalf("WORK_DIR = %q", v)
	}
	if v := find("PROJECT_ROOT"); v != "/proj" {
		t.Fatalf("PROJECT_ROOT = %q", v)
	}
}

func TestBuildEnv_UnprefixedCustomVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		CustomVars:   map[string]string{"MY_DIR": "/proj/sub"},
	}
	result := BuildEnv(env)
	foundPrefixed := false
	foundUnprefixed := false
	for _, e := range result {
		if e == "ORC_MY_DIR=/proj/sub" {
			foundPrefixed = true
		}
		if e == "MY_DIR=/proj/sub" {
			foundUnprefixed = true
		}
	}
	if !foundPrefixed {
		t.Fatal("ORC_MY_DIR not found in BuildEnv output")
	}
	if !foundUnprefixed {
		t.Fatal("MY_DIR not found in BuildEnv output")
	}
}

func TestBuildEnv_CustomVarShadowsParentEnv(t *testing.T) {
	t.Setenv("MY_DIR", "/from-parent")

	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		CustomVars:   map[string]string{"MY_DIR": "/from-config"},
	}
	result := BuildEnv(env)

	var found []string
	for _, e := range result {
		if strings.HasPrefix(e, "MY_DIR=") {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 MY_DIR entry, got %d: %v", len(found), found)
	}
	if found[0] != "MY_DIR=/from-config" {
		t.Fatalf("MY_DIR = %q, want MY_DIR=/from-config", found[0])
	}
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

func TestBuildEnv_ConcurrentSafe(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		Workflow:     "bugfix",
		PhaseCount:   5,
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := BuildEnv(env)
			if len(result) == 0 {
				t.Error("BuildEnv returned empty result")
			}
		}()
	}
	wg.Wait()
}

func TestDryRunVars_IncludesORCPrefixedCustomVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "T-1",
		Workflow:     "bugfix",
		CustomVars:   map[string]string{"MY_DIR": "/proj/sub"},
	}
	vars := env.DryRunVars()

	// Unprefixed custom var (from Vars())
	if vars["MY_DIR"] != "/proj/sub" {
		t.Errorf("MY_DIR = %q, want /proj/sub", vars["MY_DIR"])
	}
	// ORC_-prefixed custom var (new behavior)
	if vars["ORC_MY_DIR"] != "/proj/sub" {
		t.Errorf("ORC_MY_DIR = %q, want /proj/sub", vars["ORC_MY_DIR"])
	}
	// Built-in ORC_ keys must still be correct
	if vars["ORC_TICKET"] != "T-1" {
		t.Errorf("ORC_TICKET = %q, want T-1", vars["ORC_TICKET"])
	}
	// MY_DIR should appear exactly once (not duplicated by DryRunVars)
	count := 0
	for k := range vars {
		if k == "MY_DIR" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("MY_DIR appears %d times in DryRunVars, want 1", count)
	}
}

func TestDryRunVars_BuiltinORCKeysOverrideCustomVars(t *testing.T) {
	env := &Environment{
		ProjectRoot:  "/proj",
		WorkDir:      "/work",
		ArtifactsDir: "/art",
		Ticket:       "REAL-1",
		Workflow:     "bugfix",
		CustomVars:   map[string]string{"TICKET": "custom-val"},
	}
	vars := env.DryRunVars()
	// ORC_TICKET must be the built-in value, not the custom var
	if vars["ORC_TICKET"] != "REAL-1" {
		t.Errorf("ORC_TICKET = %q, want REAL-1", vars["ORC_TICKET"])
	}
}
