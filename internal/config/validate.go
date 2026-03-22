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

var validEfforts = map[string]bool{
	"":       true,
	"low":    true,
	"medium": true,
	"high":   true,
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
		"WORKFLOW": true,
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

	if !validModels[cfg.Model] {
		return fmt.Errorf("config: unknown model %q (must be opus, sonnet, or haiku)", cfg.Model)
	}
	if !validEfforts[cfg.Effort] {
		return fmt.Errorf("config: unknown effort %q (must be low, medium, or high)", cfg.Effort)
	}

	if cfg.MaxCost < 0 {
		return fmt.Errorf("config: 'max-cost' must not be negative (got %.2f)", cfg.MaxCost)
	}
	if cfg.HistoryLimit < 0 {
		return fmt.Errorf("config: 'history-limit' must not be negative (got %d)", cfg.HistoryLimit)
	}
	if cfg.HistoryLimit == 0 {
		cfg.HistoryLimit = 10 // default
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

		if p.Name != filepath.Base(p.Name) || p.Name == ".." || p.Name == "." {
			return fmt.Errorf("config: phase %d: name %q must not contain path separators", i+1, p.Name)
		}

		switch p.Type {
		case "agent":
			if p.Prompt == "" {
				return fmt.Errorf("config: agent phase %q: 'prompt' is required", p.Name)
			}
			promptPath := filepath.Join(projectRoot, p.Prompt)
			if _, err := os.Stat(promptPath); err != nil {
				return fmt.Errorf("config: agent phase %q: prompt file %q not found", p.Name, promptPath)
			}
			if p.Model == "" && cfg.Model != "" {
				p.Model = cfg.Model
			}
			if p.Model == "" {
				p.Model = "opus"
			}
			if p.Effort == "" && cfg.Effort != "" {
				p.Effort = cfg.Effort
			}
			if p.Effort == "" {
				p.Effort = "high"
			}
			if p.Cwd == "" && cfg.Cwd != "" {
				p.Cwd = cfg.Cwd
			}
			if p.Timeout == 0 {
				p.Timeout = 30
			}
		case "script":
			if p.Run == "" {
				return fmt.Errorf("config: script phase %q: 'run' is required", p.Name)
			}
			if p.Cwd == "" && cfg.Cwd != "" {
				p.Cwd = cfg.Cwd
			}
			if p.Timeout == 0 {
				p.Timeout = 10
			}
		case "gate":
			if p.Cwd != "" && p.Run == "" {
				return fmt.Errorf("config: gate phase %q: 'cwd' requires 'run' on gate phases", p.Name)
			}
			if p.Cwd == "" && cfg.Cwd != "" && p.Run != "" {
				p.Cwd = cfg.Cwd
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

		if p.MCPConfig != "" && p.Type != "agent" {
			return fmt.Errorf("config: phase %q: 'mcp-config' is only valid on agent phases", p.Name)
		}

		if !validModels[p.Model] {
			return fmt.Errorf("config: phase %q: unknown model %q (must be opus, sonnet, or haiku)", p.Name, p.Model)
		}

		if !validEfforts[p.Effort] {
			return fmt.Errorf("config: phase %q: unknown effort %q (must be low, medium, or high)", p.Name, p.Effort)
		}

		if p.Timeout < 0 {
			return fmt.Errorf("config: phase %q: timeout must be >= 0", p.Name)
		}

		if p.MaxCost < 0 {
			return fmt.Errorf("config: phase %q: 'max-cost' must not be negative (got %.2f)", p.Name, p.MaxCost)
		}
		if p.MaxCost > 0 && p.Type != "agent" {
			return fmt.Errorf("config: phase %q: 'max-cost' is only valid on agent phases", p.Name)
		}

		for _, o := range p.Outputs {
			if o != filepath.Base(o) || o == ".." || o == "." {
				return fmt.Errorf("config: phase %q: output %q must be a simple filename", p.Name, o)
			}
		}

		// Reject deprecated on-fail with migration hint
		if p.OnFail != nil {
			return fmt.Errorf("config: phase %q: 'on-fail' has been replaced by 'loop'. "+
				"Use loop: {goto: %q, max: %d} (note: loop.max is total iterations, not retries)",
				p.Name, p.OnFail.Goto, p.OnFail.Max+1)
		}

		// Validate loop
		if p.Loop != nil {
			if p.Loop.Goto == "" {
				return fmt.Errorf("config: phase %q: loop.goto is required", p.Name)
			}
			gotoIdx := -1
			for j := 0; j < i; j++ {
				if cfg.Phases[j].Name == p.Loop.Goto {
					gotoIdx = j
					break
				}
			}
			if gotoIdx < 0 {
				return fmt.Errorf("config: phase %q: loop.goto %q must reference an earlier phase", p.Name, p.Loop.Goto)
			}
			if p.Loop.Min <= 0 {
				p.Loop.Min = 1
			}
			if p.Loop.Max <= 0 {
				return fmt.Errorf("config: phase %q: loop.max is required and must be > 0", p.Name)
			}
			if p.Loop.Max < p.Loop.Min {
				return fmt.Errorf("config: phase %q: loop.max (%d) must be >= loop.min (%d)", p.Name, p.Loop.Max, p.Loop.Min)
			}
			if p.Loop.OnExhaust != nil {
				if p.Loop.OnExhaust.Goto == "" {
					return fmt.Errorf("config: phase %q: loop.on-exhaust.goto is required", p.Name)
				}
				exhaustIdx := -1
				for j := 0; j < i; j++ {
					if cfg.Phases[j].Name == p.Loop.OnExhaust.Goto {
						exhaustIdx = j
						break
					}
				}
				if exhaustIdx < 0 {
					return fmt.Errorf("config: phase %q: loop.on-exhaust.goto %q must reference an earlier phase", p.Name, p.Loop.OnExhaust.Goto)
				}
				if p.Loop.OnExhaust.Max <= 0 {
					p.Loop.OnExhaust.Max = 1
				}
			}
		}

		if p.ParallelWith != "" {
			if !seen[p.ParallelWith] && !phaseExists(cfg.Phases, p.ParallelWith) {
				return fmt.Errorf("config: phase %q: parallel-with %q references unknown phase", p.Name, p.ParallelWith)
			}
			if p.Loop != nil {
				return fmt.Errorf("config: phase %q: parallel-with and loop cannot be combined", p.Name)
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
	// Enforce full-match semantics: wrap the pattern unless it already
	// has unescaped ^ at start AND unescaped $ at end.
	anchored := pattern
	fullyAnchored := strings.HasPrefix(pattern, "^") && hasUnescapedSuffix(pattern, '$')
	if !fullyAnchored {
		anchored = "^(?:" + pattern + ")$"
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

// hasUnescapedSuffix reports whether s ends with an unescaped instance of ch.
// A character is escaped if preceded by an odd number of backslashes.
func hasUnescapedSuffix(s string, ch byte) bool {
	if len(s) == 0 || s[len(s)-1] != ch {
		return false
	}
	// Count consecutive backslashes immediately before the final character.
	n := 0
	for i := len(s) - 2; i >= 0 && s[i] == '\\'; i-- {
		n++
	}
	return n%2 == 0
}
