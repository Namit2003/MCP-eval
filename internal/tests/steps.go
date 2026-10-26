package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Step is one HTTP request run before (setup) or after (teardown) a job.
type Step struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	AuthEnv string            `json:"auth_env,omitempty"` // env var holding a bearer token
	Body    json.RawMessage   `json:"body,omitempty"`
	Save    map[string]string `json:"save,omitempty"`  // variable name -> dot path in the JSON response, such as items.0.number
	Retry   int               `json:"retry,omitempty"` // extra attempts if the step fails
}

// UnmarshalJSON rejects unknown fields so a typo such as "sav" is not
// silently ignored.
func (s *Step) UnmarshalJSON(data []byte) error {
	type plain Step
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode((*plain)(s))
}

func stepProblems(kind string, steps []Step) []string {
	var out []string
	for i, s := range steps {
		where := fmt.Sprintf("%s[%d]", kind, i)
		if strings.TrimSpace(s.Method) == "" {
			out = append(out, where+": method is required")
		}
		if strings.TrimSpace(s.URL) == "" {
			out = append(out, where+": url is required")
		}
		if s.Retry < 0 {
			out = append(out, where+": retry must be >= 0")
		}
		for name, path := range s.Save {
			if !varName.MatchString(name) || name == "job" {
				out = append(out, fmt.Sprintf("%s: save name %q must be letters, digits and _, and not job", where, name))
			}
			if strings.TrimSpace(path) == "" {
				out = append(out, fmt.Sprintf("%s: save %q needs a response path", where, name))
			}
		}
	}
	return out
}

// saveNames returns every variable saved by a setup or teardown step in a
// test file. Invalid JSON returns nothing; Parse reports it later.
func saveNames(data []byte) map[string]bool {
	type step struct {
		Save map[string]string `json:"save"`
	}
	var top struct {
		TestCases []struct {
			Setup    []step `json:"setup"`
			Teardown []step `json:"teardown"`
		} `json:"test_cases"`
	}
	_ = json.Unmarshal(data, &top)
	names := make(map[string]bool)
	for _, c := range top.TestCases {
		for _, s := range append(c.Setup, c.Teardown...) {
			for name := range s.Save {
				names[name] = true
			}
		}
	}
	return names
}

// withoutSteps returns raw without the setup and teardown fields, which are
// tefrun's and not part of mcp-tef's schema.
func withoutSteps(raw json.RawMessage) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "setup")
	delete(fields, "teardown")
	return json.Marshal(fields)
}

// CheckAuthEnvs reports every auth_env that is unset or blank, once per file.
func (s *Suite) CheckAuthEnvs(lookup func(string) (string, bool)) error {
	var errs []error
	for _, f := range s.Files {
		missing := make(map[string]bool)
		for _, c := range f.Cases {
			for _, st := range append(c.Setup, c.Teardown...) {
				if st.AuthEnv == "" {
					continue
				}
				if v, ok := lookup(st.AuthEnv); !ok || strings.TrimSpace(v) == "" {
					missing[st.AuthEnv] = true
				}
			}
		}
		for _, name := range slices.Sorted(maps.Keys(missing)) {
			errs = append(errs, fmt.Errorf("%s: environment variable %s is not set (used by auth_env)", f.Path, name))
		}
	}
	return errors.Join(errs...)
}
