package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type OnFail struct {
	Goto string `yaml:"goto"`
	Max  int    `yaml:"max"`
}

type Phase struct {
	Name         string  `yaml:"name"`
	Type         string  `yaml:"type"`
	Description  string  `yaml:"description"`
	Prompt       string  `yaml:"prompt"`
	Run          string  `yaml:"run"`
	Model        string  `yaml:"model"`
	Timeout      int     `yaml:"timeout"`
	Outputs      []string `yaml:"outputs"`
	Condition    string  `yaml:"condition"`
	ParallelWith string  `yaml:"parallel-with"`
	OnFail       *OnFail `yaml:"on-fail"`
	Cwd          string  `yaml:"cwd"`
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
		return fmt.Errorf("vars must be a mapping")
	}
	for i := 0; i < len(value.Content)-1; i += 2 {
		*ov = append(*ov, VarEntry{
			Key:   value.Content[i].Value,
			Value: value.Content[i+1].Value,
		})
	}
	return nil
}

type Config struct {
	Name          string      `yaml:"name"`
	TicketPattern string      `yaml:"ticket-pattern"`
	Vars          OrderedVars `yaml:"vars"`
	Phases        []Phase     `yaml:"phases"`
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
