package dispatch

import (
	"os"

	"github.com/jorge-barreto/orc/internal/config"
)

// ExpandVars substitutes variables in template using the vars map,
// falling back to environment variables.
func ExpandVars(template string, vars map[string]string) string {
	return os.Expand(template, func(key string) string {
		if v, ok := vars[key]; ok {
			return v
		}
		return os.Getenv(key)
	})
}

// ExpandConfigVars expands ordered var entries in declaration order.
// Each value is expanded using built-ins plus all previously expanded custom vars.
func ExpandConfigVars(vars config.OrderedVars, builtins map[string]string) map[string]string {
	result := make(map[string]string, len(vars))
	lookup := make(map[string]string, len(builtins)+len(vars))
	for k, v := range builtins {
		lookup[k] = v
	}
	for _, entry := range vars {
		expanded := ExpandVars(entry.Value, lookup)
		result[entry.Key] = expanded
		lookup[entry.Key] = expanded
	}
	return result
}
