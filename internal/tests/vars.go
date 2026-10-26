package tests

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// JobRef is left in test files at load time; the runner replaces it with a
// value unique to each job.
const JobRef = "${job}"

var (
	varName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	varRef  = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// ParseVars turns "name=value" flags into a map. A later flag with the same
// name wins.
func ParseVars(flags []string) (map[string]string, error) {
	vars := make(map[string]string)
	for _, f := range flags {
		name, value, ok := strings.Cut(f, "=")
		if !ok || !varName.MatchString(name) {
			return nil, fmt.Errorf("--var %q must be name=value, with a name of letters, digits and _", f)
		}
		if name == "job" {
			return nil, fmt.Errorf("--var %q: job is built in and cannot be set", f)
		}
		vars[name] = value
	}
	return vars, nil
}

// substitute replaces every ${name} except ${job} in data with its value,
// escaped for use inside a JSON string. Any name without a value is an error.
func substitute(data []byte, vars map[string]string) ([]byte, error) {
	var missing []string
	out := varRef.ReplaceAllFunc(data, func(ref []byte) []byte {
		name := string(ref[2 : len(ref)-1])
		if name == "job" {
			return ref
		}
		value, ok := vars[name]
		if !ok {
			missing = append(missing, name)
			return ref
		}
		quoted, _ := json.Marshal(value)
		return quoted[1 : len(quoted)-1]
	})
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf("undefined variables (set with --var): %s", strings.Join(slices.Compact(missing), ", "))
	}
	return out, nil
}
