package tests

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func withSteps(setup, teardown string) string {
	c := caseJSON("c", "close ${issue}", "t")
	return c[:len(c)-1] + `, "setup": ` + setup + `, "teardown": ` + teardown + `}`
}

func TestParseSteps(t *testing.T) {
	data := fileJSON(withSteps(
		`[{"method": "POST", "url": "https://x/issues", "auth_env": "PAT", "body": {"title": "t"}, "save": {"issue": "number"}}]`,
		`[{"method": "PATCH", "url": "https://x/issues/${issue}"}]`,
	))
	cases, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	c := cases[0]
	if len(c.Setup) != 1 || c.Setup[0].AuthEnv != "PAT" || c.Setup[0].Save["issue"] != "number" || string(c.Setup[0].Body) != `{"title": "t"}` {
		t.Errorf("setup %+v", c.Setup)
	}
	if len(c.Teardown) != 1 || c.Teardown[0].URL != "https://x/issues/${issue}" {
		t.Errorf("teardown %+v", c.Teardown)
	}
	var raw map[string]any
	_ = json.Unmarshal(c.Raw, &raw)
	if _, ok := raw["setup"]; ok {
		t.Errorf("setup left in Raw: %s", c.Raw)
	}
	if _, ok := raw["teardown"]; ok {
		t.Errorf("teardown left in Raw: %s", c.Raw)
	}
	if raw["query"] != "close ${issue}" {
		t.Errorf("Raw lost fields: %s", c.Raw)
	}
}

func TestParseStepsInvalid(t *testing.T) {
	data := fileJSON(withSteps(
		`[{"url": "u", "save": {"job": "a", "x": ""}}]`,
		`[{"method": "GET"}]`,
	))
	_, err := Parse([]byte(data))
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, w := range []string{
		"setup[0]: method is required",
		`setup[0]: save name "job" must be`,
		`setup[0]: save "x" needs a response path`,
		"teardown[0]: url is required",
	} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not contain %q", err, w)
		}
	}

	_, err = Parse([]byte(fileJSON(withSteps(`[{"method": "GET", "url": "u", "sav": {}}]`, `[]`))))
	if err == nil || !strings.Contains(err.Error(), `unknown field "sav"`) {
		t.Errorf("got %v", err)
	}
}

func TestLoadSavedVars(t *testing.T) {
	setup := `[{"method": "POST", "url": "u", "save": {"issue": "number"}}]`
	teardown := `[{"method": "GET", "url": "u", "save": {"found": "items.0.number"}}, {"method": "PATCH", "url": "u/${found}"}]`
	root := writeTree(t, map[string]string{"a.json": fileJSON(withSteps(setup, teardown))})
	s, err := Load([]string{root}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if q := s.Files[0].Cases[0].Query; q != "close ${issue}" {
		t.Errorf("got query %q", q)
	}

	_, err = Load([]string{root}, map[string]string{"issue": "1"})
	want := filepath.Join(root, "a.json") + `: variable "issue" is set by --var and also saved by a step`
	if err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}
