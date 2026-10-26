package tests

import (
	"path/filepath"
	"testing"
)

func TestParseVars(t *testing.T) {
	vars, err := ParseVars([]string{"repo=a", "q=x=y", "empty=", "repo=b"})
	if err != nil {
		t.Fatal(err)
	}
	if vars["repo"] != "b" || vars["q"] != "x=y" || vars["empty"] != "" {
		t.Errorf("got %v", vars)
	}
	for _, bad := range []string{"repo", "=a", "1x=a", "a-b=c"} {
		if _, err := ParseVars([]string{bad}); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}

func TestLoadSubstitutesVars(t *testing.T) {
	root := writeTree(t, map[string]string{"a.json": fileJSON(caseJSON("${name}", `say \"${q}\"`, "t"))})
	s, err := Load([]string{root}, map[string]string{"name": "n1", "q": `he said "hi"`})
	if err != nil {
		t.Fatal(err)
	}
	c := s.Files[0].Cases[0]
	if c.Name != "n1" || c.Query != `say "he said "hi""` {
		t.Errorf("got name %q query %q", c.Name, c.Query)
	}
}

func TestLoadUndefinedVars(t *testing.T) {
	root := writeTree(t, map[string]string{"a.json": fileJSON(caseJSON("${b}", "${a} ${b}", "t"))})
	_, err := Load([]string{root}, nil)
	want := filepath.Join(root, "a.json") + ": undefined variables (set with --var): a, b"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v, want %q", err, want)
	}
}
