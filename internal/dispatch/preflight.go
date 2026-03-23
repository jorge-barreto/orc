package dispatch

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/jorge-barreto/orc/internal/config"
)

// Preflight checks that all binaries required by the workflow phases are available on PATH.
func Preflight(phases []config.Phase) error {
	needed := make(map[string]bool)
	for _, p := range phases {
		switch p.Type {
		case "script":
			needed["bash"] = true
		case "agent":
			needed["claude"] = true
		}
		if p.PreRun != "" || p.PostRun != "" {
			needed["bash"] = true
		}
	}

	var hints []string
	for bin := range needed {
		if _, err := exec.LookPath(bin); err != nil {
			switch bin {
			case "claude":
				hints = append(hints, "claude (install: npm install -g @anthropic-ai/claude-code)")
			default:
				hints = append(hints, bin)
			}
		}
	}

	if len(hints) > 0 {
		return fmt.Errorf("required binaries not found in PATH: %s", strings.Join(hints, ", "))
	}
	return nil
}
