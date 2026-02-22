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
	}

	var missing []string
	for bin := range needed {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, bin)
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("required binaries not found in PATH: %s", strings.Join(missing, ", "))
	}
	return nil
}
