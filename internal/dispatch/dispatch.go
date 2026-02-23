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
	ProjectRoot  string
	WorkDir      string
	ArtifactsDir string
	Ticket       string
	PhaseIndex   int
	AutoMode     bool
	PhaseCount   int
	CustomVars   map[string]string
	filteredEnv  []string // lazily populated base env (os.Environ minus CLAUDECODE)
}

// Clone returns a deep copy of the Environment, including CustomVars and filteredEnv.
func (e *Environment) Clone() *Environment {
	cp := *e
	if e.CustomVars != nil {
		cp.CustomVars = make(map[string]string, len(e.CustomVars))
		for k, v := range e.CustomVars {
			cp.CustomVars[k] = v
		}
	}
	if e.filteredEnv != nil {
		cp.filteredEnv = make([]string, len(e.filteredEnv))
		copy(cp.filteredEnv, e.filteredEnv)
	}
	return &cp
}

// Vars returns the variable substitution map for prompts and commands.
// Custom vars are included first; built-ins always win (defense in depth).
func (e *Environment) Vars() map[string]string {
	m := make(map[string]string, 4+len(e.CustomVars))
	for k, v := range e.CustomVars {
		m[k] = v
	}
	m["TICKET"] = e.Ticket
	m["ARTIFACTS_DIR"] = e.ArtifactsDir
	m["WORK_DIR"] = e.WorkDir
	m["PROJECT_ROOT"] = e.ProjectRoot
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
	ExitCode int
	Output   string
}

// BuildEnv returns the environment variables for child processes.
// It inherits the current environment, adds ORC_ variables, and strips CLAUDECODE.
// The base environment is snapshotted once per Environment and reused across calls.
func BuildEnv(env *Environment) []string {
	if env.filteredEnv == nil {
		for _, e := range os.Environ() {
			key := strings.SplitN(e, "=", 2)[0]
			if strings.HasPrefix(key, "CLAUDECODE") {
				continue
			}
			env.filteredEnv = append(env.filteredEnv, e)
		}
	}
	result := make([]string, len(env.filteredEnv), len(env.filteredEnv)+6+len(env.CustomVars))
	copy(result, env.filteredEnv)
	for k, v := range env.CustomVars {
		result = append(result, "ORC_"+k+"="+v)
	}
	result = append(result,
		"ORC_TICKET="+env.Ticket,
		"ORC_ARTIFACTS_DIR="+env.ArtifactsDir,
		"ORC_WORK_DIR="+env.WorkDir,
		"ORC_PROJECT_ROOT="+env.ProjectRoot,
		fmt.Sprintf("ORC_PHASE_INDEX=%d", env.PhaseIndex),
		fmt.Sprintf("ORC_PHASE_COUNT=%d", env.PhaseCount),
	)
	return result
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
		return nil, fmt.Errorf("unknown phase type: %s", phase.Type)
	}
}
