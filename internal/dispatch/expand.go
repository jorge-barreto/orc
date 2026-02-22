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
	for _, entry := range vars {
		// Build lookup map: builtins + previously expanded vars
		lookup := make(map[string]string, len(builtins)+len(result))
		for k, v := range builtins {
			lookup[k] = v
		}
		for k, v := range result {
			lookup[k] = v
		}
		result[entry.Key] = ExpandVars(entry.Value, lookup)
	}
	return result
}
