package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const server = "http://localhost:8080/github/mcp"

func caseJSON(name, query, tool string) string {
	calls := "[]"
	if tool != "" {
		calls = `[{"mcp_server_url": "` + server + `", "tool_name": "` + tool + `", "parameters": {"q": "x"}}]`
	}
	return `{"name": "` + name + `", "query": "` + query + `", "expected_tool_calls": ` + calls +
		`, "order_dependent_matching": false, "available_mcp_servers": ["` + server + `"]}`
}

func fileJSON(cases ...string) string {
	return `{"test_cases": [` + strings.Join(cases, ",") + `]}`
}

// writeTree creates files under a temp dir and returns its path.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestParseValid(t *testing.T) {
	cases, err := Parse([]byte(fileJSON(
		caseJSON("Search repos", "Find MCP repos", "search_repositories"),
		caseJSON("No tool", "What is MCP?", ""),
	)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("got %d cases, want 2", len(cases))
	}
	c := cases[0]
	if c.Index != 0 || c.Name != "Search repos" || c.ExpectedToolCalls[0].ToolName != "search_repositories" {
		t.Errorf("unexpected case: %+v", c)
	}
	if len(cases[1].ExpectedToolCalls) != 0 || cases[1].Index != 1 {
		t.Errorf("empty expected_tool_calls should be allowed: %+v", cases[1])
	}
}

func TestParseKeepsRawWithExtraFields(t *testing.T) {
	raw := `{"name": "a", "query": "q", "available_mcp_servers": ["` + server + `"], "some_future_field": {"x": 1}}`
	cases, err := Parse([]byte(fileJSON(raw)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var got, want map[string]any
	_ = json.Unmarshal(cases[0].Raw, &got)
	_ = json.Unmarshal([]byte(raw), &want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Raw = %s, want %s", cases[0].Raw, raw)
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []struct {
		name string
		json string
		want []string
	}{
		{"syntax error has position", "{\n  \"test_cases\": [\n    {,}\n  ]\n}", []string{"line 3, column"}},
		{"unknown top-level key", `{"testcases": []}`, []string{`unknown field "testcases"`}},
		{"no cases", `{"test_cases": []}`, []string{"at least one test case"}},
		{"wrong type in case", fileJSON(`{"name": 5}`), []string{"test_cases[0]: line 1, column", "name must be string"}},
		{
			"missing fields",
			fileJSON(caseJSON("ok", "q", "t"), `{"expected_tool_calls": [{}]}`),
			[]string{
				"test_cases[1]: name is required",
				"test_cases[1]: query is required",
				"test_cases[1]: available_mcp_servers must list at least one server URL",
				"test_cases[1]: expected_tool_calls[0]: tool_name is required",
				"test_cases[1]: expected_tool_calls[0]: mcp_server_url is required",
			},
		},
		{
			"server not available",
			fileJSON(`{"name": "n", "query": "q", "available_mcp_servers": ["http://a"], "expected_tool_calls": [{"mcp_server_url": "http://b", "tool_name": "t"}]}`),
			[]string{`test_cases[0] ("n"): expected_tool_calls[0]: mcp_server_url "http://b" is not in available_mcp_servers`},
		},
		{
			"duplicate names",
			fileJSON(caseJSON("same", "q", "t"), caseJSON("same", "q2", "t")),
			[]string{`test_cases[1] ("same"): duplicate name, also used by test_cases[0]`},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.json))
			if err == nil {
				t.Fatal("expected an error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}

func TestLoadCollectsErrorsFromAllFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"good.json": fileJSON(caseJSON("a", "q", "t")),
		"bad1.json": fileJSON(`{"name": "x"}`),
		"bad2.json": `{`,
	})
	_, err := Load([]string{root})
	if err == nil {
		t.Fatal("expected an error")
	}
	msg := err.Error()
	for _, w := range []string{
		filepath.Join(root, "bad1.json") + `: test_cases[0] ("x"): query is required`,
		filepath.Join(root, "bad2.json") + ": invalid JSON",
	} {
		if !strings.Contains(msg, w) {
			t.Errorf("error %q does not contain %q", msg, w)
		}
	}
	if strings.Contains(msg, "good.json") {
		t.Errorf("good file was reported: %q", msg)
	}
}

func TestLoadSuite(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.json":     fileJSON(caseJSON("a1", "q", "t"), caseJSON("a2", "q", "")),
		"sub/b.json": fileJSON(caseJSON("b1", "q", "t")),
	})
	s, err := Load([]string{root})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(s.Files) != 2 || s.NumCases() != 3 {
		t.Errorf("got %d files / %d cases, want 2 / 3", len(s.Files), s.NumCases())
	}
}

func TestDiscover(t *testing.T) {
	root := writeTree(t, map[string]string{
		"evals/a.json":          "{}",
		"evals/b.txt":           "",
		"evals/x/c.json":        "{}",
		"evals/x/y/d.json":      "{}",
		"evals/.hidden/e.json":  "{}",
		"other/f.json":          "{}",
		"evals/x/notes.json.md": "",
	})
	rel := func(paths ...string) []string {
		var out []string
		for _, p := range paths {
			out = append(out, filepath.Join(root, p))
		}
		return out
	}
	cases := []struct {
		name     string
		patterns []string
		want     []string
	}{
		{"doublestar", []string{root + "/evals/**/*.json"}, rel("evals/a.json", "evals/x/c.json", "evals/x/y/d.json")},
		{"single star is one level", []string{root + "/evals/*/*.json"}, rel("evals/x/c.json")},
		{"directory walks json only", []string{root + "/evals"}, rel("evals/a.json", "evals/x/c.json", "evals/x/y/d.json")},
		{"single file", []string{root + "/other/f.json"}, rel("other/f.json")},
		{"dedupe and sort", []string{root + "/other/f.json", root + "/evals/*.json", root + "/other"}, rel("evals/a.json", "other/f.json")},
		{"doublestar in the middle", []string{root + "/**/y/*.json"}, rel("evals/x/y/d.json")},
		{"explicit hidden dir as base", []string{root + "/evals/.hidden/*.json"}, rel("evals/.hidden/e.json")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Discover(tc.patterns)
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v\nwant %v", got, tc.want)
			}
		})
	}
}

func TestDiscoverRelativePattern(t *testing.T) {
	root := writeTree(t, map[string]string{"evals/a.json": "{}", "evals/z/b.json": "{}"})
	t.Chdir(root)
	got, err := Discover([]string{"./evals/**/*.json"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"evals/a.json", "evals/z/b.json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestDiscoverErrors(t *testing.T) {
	root := writeTree(t, map[string]string{"evals/a.json": "{}"})
	cases := []struct {
		name     string
		patterns []string
		want     string
	}{
		{"no patterns", nil, "no test file patterns"},
		{"glob matches nothing", []string{root + "/evals/*.yaml"}, "matched no files"},
		{"glob base missing", []string{root + "/nope/**/*.json"}, "matched no files"},
		{"missing file", []string{root + "/missing.json"}, "no such file"},
		{"bad glob", []string{root + "/evals/[.json"}, "bad glob"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Discover(tc.patterns)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want error containing %q", err, tc.want)
			}
		})
	}
}
