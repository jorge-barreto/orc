package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// OnFail is kept for YAML parsing so we can provide a migration error.
// It is rejected at validation time — use Loop instead.
type OnFail struct {
	Goto string `yaml:"goto"`
	Max  int    `yaml:"max"`
}

// OnExhaust defines outer recovery when a loop exhausts.
// Accepts both string form (on-exhaust: plan) and object form (on-exhaust: {goto: plan, max: 2}).
type OnExhaust struct {
	Goto string `yaml:"goto"`
	Max  int    `yaml:"max"`
}

// UnmarshalYAML allows on-exhaust to be a simple string (phase name) or an object.
func (oe *OnExhaust) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		oe.Goto = value.Value
		return nil // Max defaulted in validation
	}
	type plain OnExhaust
	return value.Decode((*plain)(oe))
}

// Loop defines a backward jump for convergent iteration or simple retry.
type Loop struct {
	Goto      string     `yaml:"goto"`
	Min       int        `yaml:"min"`
	Max       int        `yaml:"max"`
	Check     string     `yaml:"check"`
	OnExhaust *OnExhaust `yaml:"on-exhaust"`
}

type Phase struct {
	Name         string   `yaml:"name"`
	Type         string   `yaml:"type"`
	Description  string   `yaml:"description"`
	Prompt       string   `yaml:"prompt"`
	Run          string   `yaml:"run"`
	Model        string   `yaml:"model"`
	Effort       string   `yaml:"effort"`
	Timeout      int      `yaml:"timeout"`
	MaxCost      float64  `yaml:"max-cost"`
	Outputs      []string `yaml:"outputs"`
	AllowTools   []string `yaml:"allow-tools"`
	MCPConfig    string   `yaml:"mcp-config"`
	Condition    string   `yaml:"condition"`
	ParallelWith string   `yaml:"parallel-with"`
	OnFail       *OnFail  `yaml:"on-fail"`
	Loop         *Loop    `yaml:"loop"`
	Cwd          string   `yaml:"cwd"`
	PreRun       string   `yaml:"pre-run"`
	PostRun      string   `yaml:"post-run"`
}

// VarEntry holds a single key-value pair from the vars map.
type VarEntry struct {
	Key   string
	Value string
}

// OrderedVars preserves YAML declaration order for variable entries.
type OrderedVars []VarEntry

// UnmarshalYAML reads a YAML mapping node and preserves key order.
func (ov *OrderedVars) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("config: vars: must be a mapping")
	}
	for i := 0; i < len(value.Content)-1; i += 2 {
		keyNode := value.Content[i]
		valNode := value.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("config: vars: key at position %d is not a scalar", i/2+1)
		}
		if valNode.Kind != yaml.ScalarNode {
			return fmt.Errorf("config: vars: value for %q is not a scalar (nested maps/sequences are not supported)", keyNode.Value)
		}
		*ov = append(*ov, VarEntry{
			Key:   keyNode.Value,
			Value: valNode.Value,
		})
	}
	return nil
}

type Config struct {
	Name              string      `yaml:"name"`
	TicketPattern     string      `yaml:"ticket-pattern"`
	DefaultAllowTools []string    `yaml:"default-allow-tools"`
	Model             string      `yaml:"model"`
	Cwd               string      `yaml:"cwd"`
	Effort            string      `yaml:"effort"`
	MaxCost           float64     `yaml:"max-cost"`
	Vars              OrderedVars `yaml:"vars"`
	Phases            []Phase     `yaml:"phases"`
}

// Load reads a YAML config file and returns a validated Config.
func Load(path, projectRoot string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	if err := Validate(&cfg, projectRoot); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// PhaseIndex returns the index of the named phase, or -1 if not found.
func (c *Config) PhaseIndex(name string) int {
	for i, p := range c.Phases {
		if p.Name == name {
			return i
		}
	}
	return -1
}
