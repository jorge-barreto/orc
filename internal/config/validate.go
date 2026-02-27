package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var validModels = map[string]bool{
	"":       true,
	"opus":   true,
	"sonnet": true,
	"haiku":  true,
}

var varNameRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validate checks the config for errors and sets defaults.
func Validate(cfg *Config, projectRoot string) error {
	if cfg.Name == "" {
		return fmt.Errorf("config: 'name' is required")
	}
	if len(cfg.Phases) == 0 {
		return fmt.Errorf("config: at least one phase is required")
	}

	// Validate vars
	builtins := map[string]bool{
		"TICKET": true, "ARTIFACTS_DIR": true,
		"WORK_DIR": true, "PROJECT_ROOT": true,
		"PHASE_INDEX": true, "PHASE_COUNT": true,
	}
	seenVars := make(map[string]bool)
	for _, v := range cfg.Vars {
		if v.Key == "" {
			return fmt.Errorf("config: vars: empty variable name")
		}
		if !varNameRe.MatchString(v.Key) {
			return fmt.Errorf("config: vars: %q is not a valid variable name (must match [A-Za-z_][A-Za-z0-9_]*)", v.Key)
		}
		if builtins[v.Key] {
			return fmt.Errorf("config: vars: %q overrides a built-in variable", v.Key)
		}
		if seenVars[v.Key] {
			return fmt.Errorf("config: vars: duplicate variable %q", v.Key)
		}
		seenVars[v.Key] = true
	}

	for _, tool := range cfg.DefaultAllowTools {
		if strings.TrimSpace(tool) == "" {
			return fmt.Errorf("config: 'default-allow-tools' entries must be non-empty")
		}
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
			if p.Cwd != "" {
				return fmt.Errorf("config: gate phase %q: 'cwd' is not supported on gate phases", p.Name)
			}
		default:
			return fmt.Errorf("config: phase %q: unknown type %q (must be agent, script, or gate)", p.Name, p.Type)
		}

		if len(p.AllowTools) > 0 && p.Type != "agent" {
			return fmt.Errorf("config: phase %q: 'allow-tools' is only valid on agent phases", p.Name)
		}
		for _, tool := range p.AllowTools {
			if strings.TrimSpace(tool) == "" {
				return fmt.Errorf("config: phase %q: 'allow-tools' entries must be non-empty", p.Name)
			}
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
			if p.OnFail != nil {
				return fmt.Errorf("config: phase %q: parallel-with and on-fail cannot be combined", p.Name)
			}
		}
	}

	return nil
}

// ValidateTicket checks that the ticket string matches the configured pattern.
// If pattern is empty, any ticket is accepted.
func ValidateTicket(pattern, ticket string) error {
	if pattern == "" {
		return nil
	}
	// Enforce full-match semantics: anchor the pattern if not already anchored.
	anchored := pattern
	if !strings.HasPrefix(anchored, "^") {
		anchored = "^(?:" + anchored + ")$"
	}
	re, err := regexp.Compile(anchored)
	if err != nil {
		return fmt.Errorf("config: invalid ticket-pattern %q: %w", pattern, err)
	}
	if !re.MatchString(ticket) {
		return fmt.Errorf("ticket %q does not match pattern %q", ticket, pattern)
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
