package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func env(vars map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

func TestParseValid(t *testing.T) {
	cfg, err := Parse([]byte(`{"models": [
		{"provider": "openrouter", "model": "anthropic/claude-3.5-sonnet", "api_key_env": "OR_KEY", "temperature": 0.4, "timeout": 30, "max_retries": 3},
		{"provider": "ollama", "model": "llama3.2:3b"}
	]}`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(cfg.Models) != 2 {
		t.Fatalf("got %d models, want 2", len(cfg.Models))
	}
	m := cfg.Models[0]
	if m.Label() != "openrouter/anthropic/claude-3.5-sonnet" || *m.Temperature != 0.4 || *m.Timeout != 30 || *m.MaxRetries != 3 {
		t.Errorf("unexpected model: %+v", m)
	}
	o := cfg.Models[1]
	if o.Temperature != nil || o.Timeout != nil || o.MaxRetries != nil {
		t.Errorf("omitted optional fields should stay nil: %+v", o)
	}
}

func TestParseZeroTemperatureIsKept(t *testing.T) {
	cfg, err := Parse([]byte(`{"models": [{"provider": "ollama", "model": "m", "temperature": 0}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models[0].Temperature == nil || *cfg.Models[0].Temperature != 0 {
		t.Errorf("temperature 0 should be set explicitly, got %v", cfg.Models[0].Temperature)
	}
}

func TestParseInvalid(t *testing.T) {
	cases := []struct {
		name string
		json string
		want []string
	}{
		{"syntax error has position", "{\n  \"models\": [\n    {\"provider\": }\n  ]\n}", []string{"line 3, column"}},
		{"truncated", `{"models": [`, []string{"unexpected end of input"}},
		{"wrong type", `{"models": [{"provider": "a", "model": "b", "timeout": "30"}]}`, []string{"line 1, column", "timeout must be int, got string"}},
		{"unknown field", `{"models": [{"provider": "a", "model": "b", "api_key": "x"}]}`, []string{`unknown field "api_key"`}},
		{"no models", `{"models": []}`, []string{"at least one model"}},
		{"missing models key", `{}`, []string{"at least one model"}},
		{"trailing data", `{"models": [{"provider": "a", "model": "b"}]} {}`, []string{"unexpected data"}},
		{
			"field errors carry index",
			`{"models": [
				{"provider": "ollama", "model": "ok"},
				{"provider": "", "model": "", "temperature": -1, "timeout": 0, "max_retries": -2}
			]}`,
			[]string{
				"models[1]: provider is required",
				"models[1]: model is required",
				"models[1]: temperature must be >= 0",
				"models[1]: timeout must be > 0",
				"models[1]: max_retries must be >= 0",
			},
		},
		{
			"duplicate model",
			`{"models": [{"provider": "ollama", "model": "m"}, {"provider": "ollama", "model": "m"}]}`,
			[]string{"models[1]: duplicate of models[0] (ollama/m)"},
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

func TestLoadPrefixesPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model.config.json")
	if err := os.WriteFile(path, []byte(`{"models": [{"provider": "", "model": ""}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		if !strings.HasPrefix(line, path+": models[0]: ") {
			t.Errorf("line %q is not prefixed with the path and index", line)
		}
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err == nil || !strings.Contains(err.Error(), "nope.json") {
		t.Fatalf("expected a missing-file error naming the file, got %v", err)
	}
}

func TestCheckAPIKeys(t *testing.T) {
	cfg := &Config{Path: "m.json", Models: []Model{
		{Provider: "openrouter", Model: "a", APIKeyEnv: "OR_KEY"},
		{Provider: "ollama", Model: "b"},
	}}
	if err := cfg.CheckAPIKeys(env(map[string]string{"OR_KEY": "secret"})); err != nil {
		t.Errorf("all keys set, got %v", err)
	}

	err := cfg.CheckAPIKeys(env(nil))
	if err == nil || !strings.Contains(err.Error(), "m.json: models[0] (openrouter/a): environment variable OR_KEY is not set") {
		t.Errorf("unset key: got %v", err)
	}
	if strings.Contains(err.Error(), "models[1]") {
		t.Errorf("ollama needs no key, but was reported: %v", err)
	}

	if err := cfg.CheckAPIKeys(env(map[string]string{"OR_KEY": "  "})); err == nil {
		t.Error("a blank key should count as unset")
	}
}

func TestResolveAPIKey(t *testing.T) {
	lookup := env(map[string]string{"K": "secret"})
	cases := []struct {
		name    string
		model   Model
		want    string
		wantErr string
	}{
		{"keyed provider", Model{Provider: "openrouter", APIKeyEnv: "K"}, "secret", ""},
		{"keyed provider without env name", Model{Provider: "openrouter"}, "", `api_key_env is required for provider "openrouter"`},
		{"keyless provider", Model{Provider: "ollama"}, "", ""},
		{"keyless provider is case-insensitive", Model{Provider: "Ollama"}, "", ""},
		{"keyless provider with env name must resolve", Model{Provider: "ollama", APIKeyEnv: "MISSING"}, "", "MISSING is not set"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.model.ResolveAPIKey(lookup)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("got err %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got (%q, %v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestKeyNeverInConfig(t *testing.T) {
	// Keys are resolved on demand, so nothing that serializes a Config can
	// contain one.
	t.Setenv("TEFRUN_TEST_KEY", "super-secret-value")
	cfg, err := Parse([]byte(`{"models": [{"provider": "openrouter", "model": "a", "api_key_env": "TEFRUN_TEST_KEY"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.CheckAPIKeys(os.LookupEnv); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(cfg)
	if strings.Contains(string(b), "super-secret-value") {
		t.Errorf("config JSON leaked the key: %s", b)
	}
}
