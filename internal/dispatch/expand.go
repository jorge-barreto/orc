package dispatch

import "os"

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
