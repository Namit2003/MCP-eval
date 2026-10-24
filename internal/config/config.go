// Package config loads and validates model.config.json.
//
// Each model entry maps onto mcp-tef's model_settings. API keys are never read
// into these structs: a model names the environment variable that holds its
// key, and callers resolve it at the moment they need it.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Namit2003/mcp-eval/internal/inputerr"
)

// keylessProviders run locally and need no API key.
var keylessProviders = map[string]bool{
	"ollama": true,
}

// Config is the parsed contents of a model config file.
type Config struct {
	Path   string  `json:"-"`
	Models []Model `json:"models"`
}

// Model is one model to evaluate. Optional fields are pointers so that an
// omitted value falls back to mcp-tef's default instead of being sent as zero.
type Model struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	APIKeyEnv   string   `json:"api_key_env,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	Timeout     *int     `json:"timeout,omitempty"`
	MaxRetries  *int     `json:"max_retries,omitempty"`
}

// Label identifies a model in errors and reports, e.g. "openrouter/anthropic/claude-3.5-sonnet".
func (m Model) Label() string {
	return m.Provider + "/" + m.Model
}

// NeedsAPIKey reports whether the model must have a resolvable API key.
// A keyless provider still needs one if the config names an api_key_env.
func (m Model) NeedsAPIKey() bool {
	return m.APIKeyEnv != "" || !keylessProviders[strings.ToLower(m.Provider)]
}

// ResolveAPIKey looks up the model's API key with lookup (typically
// os.LookupEnv). It returns "" for keyless models that name no variable.
func (m Model) ResolveAPIKey(lookup func(string) (string, bool)) (string, error) {
	if !m.NeedsAPIKey() {
		return "", nil
	}
	if m.APIKeyEnv == "" {
		return "", fmt.Errorf("api_key_env is required for provider %q", m.Provider)
	}
	v, ok := lookup(m.APIKeyEnv)
	if !ok || strings.TrimSpace(v) == "" {
		return "", fmt.Errorf("environment variable %s is not set", m.APIKeyEnv)
	}
	return v, nil
}

// Load reads, decodes and validates the config at path. It does not check
// API keys; call CheckAPIKeys for that.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("model config: %w", err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, inputerr.Prefix(path, err)
	}
	cfg.Path = path
	return cfg, nil
}

// Parse decodes and validates config JSON. Unknown fields are rejected so
// that typos such as "api_key" surface instead of being silently ignored.
func Parse(data []byte) (*Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, inputerr.Describe(data, err)
	}
	if dec.More() {
		return nil, errors.New("unexpected data after the top-level JSON object")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks every model and returns all problems found, joined.
func (c *Config) Validate() error {
	if len(c.Models) == 0 {
		return errors.New(`"models" must contain at least one model`)
	}
	var errs []error
	seen := make(map[string]int)
	for i, m := range c.Models {
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("models[%d]: "+format, append([]any{i}, args...)...))
		}
		if strings.TrimSpace(m.Provider) == "" {
			fail("provider is required")
		}
		if strings.TrimSpace(m.Model) == "" {
			fail("model is required")
		}
		if m.Temperature != nil && *m.Temperature < 0 {
			fail("temperature must be >= 0, got %v", *m.Temperature)
		}
		if m.Timeout != nil && *m.Timeout <= 0 {
			fail("timeout must be > 0 seconds, got %d", *m.Timeout)
		}
		if m.MaxRetries != nil && *m.MaxRetries < 0 {
			fail("max_retries must be >= 0, got %d", *m.MaxRetries)
		}
		if m.Provider != "" && m.Model != "" {
			if first, dup := seen[m.Label()]; dup {
				fail("duplicate of models[%d] (%s)", first, m.Label())
			} else {
				seen[m.Label()] = i
			}
		}
	}
	return errors.Join(errs...)
}

// CheckAPIKeys verifies that every model's API key resolves, so a run fails
// before starting anything rather than partway through.
func (c *Config) CheckAPIKeys(lookup func(string) (string, bool)) error {
	var errs []error
	for i, m := range c.Models {
		if _, err := m.ResolveAPIKey(lookup); err != nil {
			errs = append(errs, fmt.Errorf("models[%d] (%s): %w", i, m.Label(), err))
		}
	}
	return inputerr.Prefix(c.Path, errors.Join(errs...))
}
