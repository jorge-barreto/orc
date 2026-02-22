package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var validModels = map[string]bool{
	"":       true,
	"opus":   true,
	"sonnet": true,
	"haiku":  true,
}

// Validate checks the config for errors and sets defaults.
func Validate(cfg *Config, projectRoot string) error {
	if cfg.Name == "" {
		return fmt.Errorf("config: 'name' is required")
	}
	if len(cfg.Phases) == 0 {
		return fmt.Errorf("config: at least one phase is required")
	}

	seen := make(map[string]bool)
	for i := range cfg.Phases {
		p := &cfg.Phases[i]

		if p.Name == "" {
			return fmt.Errorf("config: phase %d: 'name' is required", i+1)
		}
		if p.Type == "" {
			return fmt.Errorf("config: phase %q: 'type' is required", p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("config: duplicate phase name %q", p.Name)
		}
		seen[p.Name] = true

		switch p.Type {
		case "agent":
			if p.Prompt == "" {
				return fmt.Errorf("config: agent phase %q: 'prompt' is required", p.Name)
			}
			promptPath := filepath.Join(projectRoot, p.Prompt)
			if _, err := os.Stat(promptPath); err != nil {
				return fmt.Errorf("config: agent phase %q: prompt file %q not found", p.Name, promptPath)
			}
			if p.Model == "" {
				p.Model = "opus"
			}
			if p.Timeout == 0 {
				p.Timeout = 30
			}
		case "script":
			if p.Run == "" {
				return fmt.Errorf("config: script phase %q: 'run' is required", p.Name)
			}
			if p.Timeout == 0 {
				p.Timeout = 10
			}
		case "gate":
			// gates have no required fields beyond name+type
			if p.Timeout == 0 {
				p.Timeout = 0 // no timeout for gates
			}
		default:
			return fmt.Errorf("config: phase %q: unknown type %q (must be agent, script, or gate)", p.Name, p.Type)
		}

		if !validModels[p.Model] {
			return fmt.Errorf("config: phase %q: unknown model %q (must be opus, sonnet, or haiku)", p.Name, p.Model)
		}

		if p.Timeout < 0 {
			return fmt.Errorf("config: phase %q: timeout must be >= 0", p.Name)
		}

		for _, o := range p.Outputs {
			if strings.Contains(o, "/") || strings.Contains(o, string(filepath.Separator)) {
				return fmt.Errorf("config: phase %q: output %q must not contain path separators", p.Name, o)
			}
		}

		if p.OnFail != nil {
			if p.OnFail.Goto == "" {
				return fmt.Errorf("config: phase %q: on-fail.goto is required", p.Name)
			}
			gotoIdx := -1
			for j := 0; j < i; j++ {
				if cfg.Phases[j].Name == p.OnFail.Goto {
					gotoIdx = j
					break
				}
			}
			if gotoIdx < 0 {
				return fmt.Errorf("config: phase %q: on-fail.goto %q must reference an earlier phase", p.Name, p.OnFail.Goto)
			}
			if p.OnFail.Max <= 0 {
				p.OnFail.Max = 2
			}
		}

		if p.ParallelWith != "" {
			if !seen[p.ParallelWith] && !phaseExists(cfg.Phases, p.ParallelWith) {
				return fmt.Errorf("config: phase %q: parallel-with %q references unknown phase", p.Name, p.ParallelWith)
			}
		}
	}

	return nil
}

func phaseExists(phases []Phase, name string) bool {
	for _, p := range phases {
		if p.Name == name {
			return true
		}
	}
	return false
}
