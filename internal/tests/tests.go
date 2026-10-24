// Package tests loads and validates eval test files.
//
// A test file holds {"test_cases": [...]}, where each case uses mcp-tef's test
// case schema unchanged. The raw JSON of each case is kept so it can be sent
// to mcp-tef as written; only the fields tefrun relies on are parsed and
// checked here.
package tests

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Namit2003/mcp-eval/internal/inputerr"
)

// Suite is every test file in a run.
type Suite struct {
	Files []File
}

// File is one loaded test file.
type File struct {
	Path  string
	Cases []Case
}

// Case is one test case. Raw is the case exactly as written in the file.
type Case struct {
	Index                  int             `json:"-"`
	Name                   string          `json:"name"`
	Query                  string          `json:"query"`
	ExpectedToolCalls      []ToolCall      `json:"expected_tool_calls"`
	OrderDependentMatching bool            `json:"order_dependent_matching"`
	AvailableMCPServers    []string        `json:"available_mcp_servers"`
	Raw                    json.RawMessage `json:"-"`
}

// ToolCall is an expected tool call in a test case.
type ToolCall struct {
	MCPServerURL string         `json:"mcp_server_url"`
	ToolName     string         `json:"tool_name"`
	Parameters   map[string]any `json:"parameters,omitempty"`
}

// NumCases returns the total number of cases across all files.
func (s *Suite) NumCases() int {
	n := 0
	for _, f := range s.Files {
		n += len(f.Cases)
	}
	return n
}

// Load discovers files from patterns and loads them all, collecting every
// problem across every file before returning.
func Load(patterns []string) (*Suite, error) {
	paths, err := Discover(patterns)
	if err != nil {
		return nil, err
	}
	var s Suite
	var errs []error
	for _, p := range paths {
		f, err := LoadFile(p)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		s.Files = append(s.Files, *f)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &s, nil
}

// LoadFile reads and validates one test file.
func LoadFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cases, err := Parse(data)
	if err != nil {
		return nil, inputerr.Prefix(path, err)
	}
	return &File{Path: path, Cases: cases}, nil
}

// Parse decodes and validates the contents of a test file.
func Parse(data []byte) ([]Case, error) {
	var top struct {
		TestCases []json.RawMessage `json:"test_cases"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&top); err != nil {
		return nil, inputerr.Describe(data, err)
	}
	if dec.More() {
		return nil, errors.New("unexpected data after the top-level JSON object")
	}
	if len(top.TestCases) == 0 {
		return nil, errors.New(`"test_cases" must contain at least one test case`)
	}

	var cases []Case
	var errs []error
	names := make(map[string]int)
	for i, raw := range top.TestCases {
		var c Case
		// Case-level fields are not restricted: anything else in mcp-tef's
		// schema is passed through untouched via Raw.
		if err := json.Unmarshal(raw, &c); err != nil {
			errs = append(errs, fmt.Errorf("test_cases[%d]: %w", i, inputerr.Describe(raw, err)))
			continue
		}
		c.Index = i
		c.Raw = raw
		for _, problem := range c.problems() {
			errs = append(errs, fmt.Errorf("test_cases[%d]%s: %s", i, c.nameSuffix(), problem))
		}
		if c.Name != "" {
			if first, dup := names[c.Name]; dup {
				errs = append(errs, fmt.Errorf("test_cases[%d]%s: duplicate name, also used by test_cases[%d]", i, c.nameSuffix(), first))
			} else {
				names[c.Name] = i
			}
		}
		cases = append(cases, c)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return cases, nil
}

func (c *Case) nameSuffix() string {
	if c.Name == "" {
		return ""
	}
	return fmt.Sprintf(" (%q)", c.Name)
}

// problems lists validation failures. An empty expected_tool_calls is valid:
// it means the model should call no tool.
func (c *Case) problems() []string {
	var out []string
	if strings.TrimSpace(c.Name) == "" {
		out = append(out, "name is required")
	}
	if strings.TrimSpace(c.Query) == "" {
		out = append(out, "query is required")
	}
	available := make(map[string]bool)
	if len(c.AvailableMCPServers) == 0 {
		out = append(out, "available_mcp_servers must list at least one server URL")
	}
	for j, u := range c.AvailableMCPServers {
		if strings.TrimSpace(u) == "" {
			out = append(out, fmt.Sprintf("available_mcp_servers[%d] is empty", j))
		}
		available[u] = true
	}
	for j, tc := range c.ExpectedToolCalls {
		if strings.TrimSpace(tc.ToolName) == "" {
			out = append(out, fmt.Sprintf("expected_tool_calls[%d]: tool_name is required", j))
		}
		switch {
		case strings.TrimSpace(tc.MCPServerURL) == "":
			out = append(out, fmt.Sprintf("expected_tool_calls[%d]: mcp_server_url is required", j))
		case len(c.AvailableMCPServers) > 0 && !available[tc.MCPServerURL]:
			out = append(out, fmt.Sprintf("expected_tool_calls[%d]: mcp_server_url %q is not in available_mcp_servers", j, tc.MCPServerURL))
		}
	}
	return out
}
