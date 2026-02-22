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
}

// Vars returns the variable substitution map for prompts and commands.
func (e *Environment) Vars() map[string]string {
	return map[string]string{
		"TICKET":       e.Ticket,
		"ARTIFACTS_DIR": e.ArtifactsDir,
		"WORK_DIR":     e.WorkDir,
		"PROJECT_ROOT": e.ProjectRoot,
	}
}

// Result holds the outcome of a phase dispatch.
type Result struct {
	ExitCode int
	Output   string
}

// BuildEnv returns the environment variables for child processes.
// It inherits the current environment, adds ORC_ variables, and strips CLAUDECODE.
func BuildEnv(env *Environment) []string {
	result := make([]string, 0, len(os.Environ())+10)
	for _, e := range os.Environ() {
		key := strings.SplitN(e, "=", 2)[0]
		if strings.HasPrefix(key, "CLAUDECODE") {
			continue
		}
		result = append(result, e)
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
func Dispatch(ctx context.Context, phase config.Phase, env *Environment) (*Result, error) {
	switch phase.Type {
	case "script":
		return RunScript(ctx, phase, env)
	case "agent":
		return RunAgent(ctx, phase, env)
	case "gate":
		return RunGate(ctx, phase, env)
	default:
		return nil, fmt.Errorf("unknown phase type: %s", phase.Type)
	}
}
