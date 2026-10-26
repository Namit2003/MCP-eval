package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Namit2003/mcp-eval/internal/tests"
)

// teardownTimeout bounds teardown on its own, so cleanup still runs after
// the run's deadline passes or Ctrl+C cancels it.
const teardownTimeout = 30 * time.Second

// retryDelay is the pause between attempts of a step with retry set.
var retryDelay = 2 * time.Second

// runStep fills ${name} references in s from vars, sends the request, and
// adds any values the step saves to vars. A failed step is tried again up to
// s.Retry more times.
func runStep(ctx context.Context, s tests.Step, vars map[string]string) error {
	data, _ := json.Marshal(s)
	data, err := tests.Substitute(data, vars, nil)
	if err != nil {
		return err
	}
	// Decode into a new Step: s shares Body and Save with the case, which
	// other jobs use too.
	s = tests.Step{}
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		err := send(ctx, s, vars)
		if err == nil || attempt >= s.Retry {
			return err
		}
		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return err
		}
	}
}

// send makes one attempt of a filled-in step.
func send(ctx context.Context, s tests.Step, vars map[string]string) error {
	var body io.Reader
	if len(s.Body) > 0 {
		body = bytes.NewReader(s.Body)
	}
	req, err := http.NewRequestWithContext(ctx, s.Method, s.URL, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if s.AuthEnv != "" {
		token := os.Getenv(s.AuthEnv)
		if token == "" {
			return fmt.Errorf("environment variable %s is not set", s.AuthEnv)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%s %s: %s: %.200s", s.Method, s.URL, resp.Status, respBody)
	}

	if len(s.Save) == 0 {
		return nil
	}
	var doc any
	dec := json.NewDecoder(bytes.NewReader(respBody))
	dec.UseNumber() // keep ids such as 123 as "123", not "123.0"
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("%s %s: response is not JSON: %w", s.Method, s.URL, err)
	}
	for name, path := range s.Save {
		value, err := lookup(doc, path)
		if err != nil {
			return fmt.Errorf("save %q: %w", name, err)
		}
		vars[name] = value
	}
	return nil
}

// lookup follows a dot path such as "data.id" or "items.0.number" through
// JSON objects and arrays and returns the value as text.
func lookup(doc any, path string) (string, error) {
	for _, key := range strings.Split(path, ".") {
		found := false
		switch v := doc.(type) {
		case map[string]any:
			doc, found = v[key]
		case []any:
			if i, err := strconv.Atoi(key); err == nil && i >= 0 && i < len(v) {
				doc, found = v[i], true
			}
		}
		if !found {
			return "", fmt.Errorf("path %q not found in response", path)
		}
	}
	switch v := doc.(type) {
	case string:
		return v, nil
	case json.Number:
		return v.String(), nil
	}
	text, _ := json.Marshal(doc)
	return string(text), nil
}

// runTeardown runs every teardown step, even after one fails, so as much as
// possible is cleaned up. It ignores the run's cancellation. A step that uses
// a variable that was never saved is skipped: whatever it would clean up was
// never created, and the failed setup or teardown step is already reported.
func runTeardown(ctx context.Context, steps []tests.Step, vars map[string]string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), teardownTimeout)
	defer cancel()
	var errs []error
	for _, s := range steps {
		data, _ := json.Marshal(s)
		if _, err := tests.Substitute(data, vars, nil); err != nil {
			continue
		}
		errs = append(errs, runStep(ctx, s, vars))
	}
	return errors.Join(errs...)
}
