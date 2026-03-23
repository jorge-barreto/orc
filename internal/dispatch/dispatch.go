package dispatch

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
)

// Environment holds the execution context for phase dispatch.
type Environment struct {
	ProjectRoot       string
	WorkDir           string
	ArtifactsDir      string
	Ticket            string
	Workflow          string
	PhaseIndex        int
	AutoMode          bool
	Verbose           bool
	ResumeSessionID   string // session ID from interrupted phase for --resume
	PhaseCount        int
	DefaultAllowTools []string
	CustomVars        map[string]string
}

// Clone returns a deep copy of the Environment, including CustomVars.
func (e *Environment) Clone() *Environment {
	cp := *e
	if e.DefaultAllowTools != nil {
		cp.DefaultAllowTools = make([]string, len(e.DefaultAllowTools))
		copy(cp.DefaultAllowTools, e.DefaultAllowTools)
	}
	if e.CustomVars != nil {
		cp.CustomVars = make(map[string]string, len(e.CustomVars))
		for k, v := range e.CustomVars {
			cp.CustomVars[k] = v
		}
	}
	return &cp
}

// Vars returns the variable substitution map for prompts and commands.
// Custom vars are included first; built-ins always win (defense in depth).
func (e *Environment) Vars() map[string]string {
	m := make(map[string]string, 5+len(e.CustomVars))
	for k, v := range e.CustomVars {
		m[k] = v
	}
	m["TICKET"] = e.Ticket
	m["WORKFLOW"] = e.Workflow
	m["ARTIFACTS_DIR"] = e.ArtifactsDir
	m["WORK_DIR"] = e.WorkDir
	m["PROJECT_ROOT"] = e.ProjectRoot
	return m
}

// DryRunVars returns the variable substitution map for dry-run display expansion.
// Includes both unprefixed (ARTIFACTS_DIR) and ORC_-prefixed (ORC_ARTIFACTS_DIR)
// keys, matching what BuildEnv provides to child processes at runtime.
// ORC_PHASE_INDEX and ORC_PHASE_COUNT are omitted — not meaningful in dry-run context.
func (e *Environment) DryRunVars() map[string]string {
	m := e.Vars()
	for k, v := range e.CustomVars {
		m["ORC_"+k] = v
	}
	m["ORC_TICKET"] = e.Ticket
	m["ORC_WORKFLOW"] = e.Workflow
	m["ORC_ARTIFACTS_DIR"] = e.ArtifactsDir
	m["ORC_WORK_DIR"] = e.WorkDir
	m["ORC_PROJECT_ROOT"] = e.ProjectRoot
	return m
}

// PhaseWorkDir returns the working directory for a phase.
// If the phase has a cwd field, it is expanded using the full vars map.
// Otherwise, the environment's WorkDir is used.
func PhaseWorkDir(phase config.Phase, env *Environment) string {
	if phase.Cwd != "" {
		expanded := ExpandVars(phase.Cwd, env.Vars())
		if expanded == "" {
			return env.WorkDir
		}
		return expanded
	}
	return env.WorkDir
}

// Result holds the outcome of a phase dispatch.
type Result struct {
	ExitCode                 int
	Output                   string
	TimedOut                 bool // true if killed by orc's phase timeout
	CostUSD                  float64
	InputTokens              int
	OutputTokens             int
	CacheCreationInputTokens int
	CacheReadInputTokens     int
	Turns                    int
	SessionID                string // agent session ID for --resume
	ToolsUsed                []string
	ToolsDenied              []string
}

// BuildEnv returns the environment variables for child processes.
// It inherits the current environment, adds ORC_ variables, and strips CLAUDECODE.
func BuildEnv(env *Environment) []string {
	// Strip vars we'll re-add with correct values: ORC_*, CLAUDECODE*, and
	// the unprefixed built-in aliases (TICKET, ARTIFACTS_DIR, etc.).
	overridden := map[string]bool{
		"TICKET": true, "ARTIFACTS_DIR": true,
		"WORK_DIR": true, "PROJECT_ROOT": true,
		"WORKFLOW": true,
	}
	for k := range env.CustomVars {
		overridden[k] = true
	}
	var filtered []string
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if strings.HasPrefix(key, "CLAUDECODE") || strings.HasPrefix(key, "ORC_") || overridden[key] {
			continue
		}
		filtered = append(filtered, e)
	}
	result := make([]string, len(filtered), len(filtered)+12+2*len(env.CustomVars))
	copy(result, filtered)
	for k, v := range env.CustomVars {
		result = append(result, "ORC_"+k+"="+v)
		result = append(result, k+"="+v)
	}
	result = append(result,
		"ORC_TICKET="+env.Ticket,
		"ORC_WORKFLOW="+env.Workflow,
		"ORC_ARTIFACTS_DIR="+env.ArtifactsDir,
		"ORC_WORK_DIR="+env.WorkDir,
		"ORC_PROJECT_ROOT="+env.ProjectRoot,
		fmt.Sprintf("ORC_PHASE_INDEX=%d", env.PhaseIndex),
		fmt.Sprintf("ORC_PHASE_COUNT=%d", env.PhaseCount),
		// Unprefixed aliases so external scripts can use $ARTIFACTS_DIR etc.
		"TICKET="+env.Ticket,
		"WORKFLOW="+env.Workflow,
		"ARTIFACTS_DIR="+env.ArtifactsDir,
		"WORK_DIR="+env.WorkDir,
		"PROJECT_ROOT="+env.ProjectRoot,
	)
	return result
}

// FilteredEnv returns os.Environ() with CLAUDECODE entries stripped.
// Used by scaffold, doctor, and improve when invoking claude directly (not via the runner).
func FilteredEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if strings.HasPrefix(key, "CLAUDECODE") {
			continue
		}
		env = append(env, e)
	}
	return env
}

// Dispatcher is the interface for dispatching phases. Tests can substitute a mock.
type Dispatcher interface {
	Dispatch(ctx context.Context, phase config.Phase, env *Environment) (*Result, error)
}

// DefaultDispatcher routes phases to the real executors.
type DefaultDispatcher struct{}

func (d *DefaultDispatcher) Dispatch(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	return Dispatch(ctx, phase, env)
}

// Dispatch routes a phase to the appropriate executor.
// Agent phases are routed to attended mode (with steering) unless AutoMode is set.
func Dispatch(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	switch phase.Type {
	case "script":
		return RunScript(ctx, phase, env)
	case "agent":
		if env.AutoMode {
			return RunAgent(ctx, phase, env)
		}
		return RunAgentAttended(ctx, phase, env)
	case "gate":
		return RunGate(ctx, phase, env)
	default:
		return nil, fmt.Errorf("unknown phase type %q for phase %q (must be agent, script, or gate)", phase.Type, phase.Name)
	}
}
