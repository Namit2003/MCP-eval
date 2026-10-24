package teff

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Client calls the mcp-tef HTTP API described in docs/mcp-tef-api.md.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a client for the mcp-tef server at baseURL. Requests have
// no timeout of their own; the ctx passed to each call bounds it.
func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{}}
}

// ModelSettings is the model_settings body of a run request. Nil optional
// fields are omitted so mcp-tef applies its defaults.
type ModelSettings struct {
	Provider    string   `json:"provider"`
	Model       string   `json:"model"`
	Timeout     *int     `json:"timeout,omitempty"`
	Temperature *float64 `json:"temperature,omitempty"`
	MaxRetries  *int     `json:"max_retries,omitempty"`
}

// TestRun is one execution of a test case on one model.
type TestRun struct {
	ID                      string          `json:"id"`
	TestCaseID              string          `json:"test_case_id"`
	Status                  string          `json:"status"`
	Classification          string          `json:"classification"`
	LLMConfidence           string          `json:"llm_confidence"`
	ConfidenceScore         string          `json:"confidence_score"`
	AvgParameterCorrectness *float64        `json:"avg_parameter_correctness"`
	ExecutionTimeMS         *int            `json:"execution_time_ms"`
	ErrorMessage            string          `json:"error_message"`
	ToolCallMatches         []ToolCallMatch `json:"tool_call_matches"`
	LLMResponseRaw          string          `json:"llm_response_raw"`
}

// ToolCallMatch compares one expected tool call with what the LLM called.
type ToolCallMatch struct {
	MatchType              string            `json:"match_type"`
	ExpectedToolCall       *ExpectedToolCall `json:"expected_tool_call"`
	ActualToolCall         *ActualToolCall   `json:"actual_tool_call"`
	ActualParameters       map[string]any    `json:"actual_parameters"`
	ParameterCorrectness   *float64          `json:"parameter_correctness"`
	ParameterJustification string            `json:"parameter_justification"`
}

// ExpectedToolCall is a tool call the test case expected.
type ExpectedToolCall struct {
	MCPServerURL string         `json:"mcp_server_url"`
	ToolName     string         `json:"tool_name"`
	Parameters   map[string]any `json:"parameters"`
}

// ActualToolCall is the tool the LLM selected.
type ActualToolCall struct {
	Name         string `json:"name"`
	MCPServerURL string `json:"mcp_server_url"`
}

// CreateTestCase creates a test case from its JSON as written in the test
// file and returns its id.
func (c *Client) CreateTestCase(ctx context.Context, testCase json.RawMessage) (string, error) {
	var created struct {
		ID string `json:"id"`
	}
	if err := c.do(ctx, http.MethodPost, "/test-cases", testCase, "", http.StatusCreated, &created); err != nil {
		return "", err
	}
	return created.ID, nil
}

// RunTestCase starts a run of a test case and returns it, usually still
// pending. apiKey is sent as X-Model-API-Key when not empty.
func (c *Client) RunTestCase(ctx context.Context, testCaseID string, settings ModelSettings, apiKey string) (*TestRun, error) {
	body := struct {
		ModelSettings ModelSettings `json:"model_settings"`
	}{settings}
	var run TestRun
	path := "/test-cases/" + url.PathEscape(testCaseID) + "/run"
	if err := c.do(ctx, http.MethodPost, path, body, apiKey, http.StatusCreated, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// GetTestRun fetches a test run.
func (c *Client) GetTestRun(ctx context.Context, runID string) (*TestRun, error) {
	var run TestRun
	if err := c.do(ctx, http.MethodGet, "/test-runs/"+url.PathEscape(runID), nil, "", http.StatusOK, &run); err != nil {
		return nil, err
	}
	return &run, nil
}

// WaitForRun polls a test run every interval until it is completed or
// failed, and returns it. A failed run is returned, not treated as an error.
func (c *Client) WaitForRun(ctx context.Context, runID string, interval time.Duration) (*TestRun, error) {
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		run, err := c.GetTestRun(ctx, runID)
		if err != nil {
			return nil, err
		}
		if run.Status == "completed" || run.Status == "failed" {
			return run, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

// do sends a JSON request and decodes the response into out, failing unless
// the status code is want.
func (c *Client) do(ctx context.Context, method, path string, body any, apiKey string, want int, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("%s %s: %w", method, path, err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if apiKey != "" {
		req.Header.Set("X-Model-API-Key", apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		// mcp-tef errors carry a readable "message"; anything else (such
		// as FastAPI's 422 {"detail": [...]}) is shown as is.
		var e struct {
			Message string `json:"message"`
		}
		msg := string(bytes.TrimSpace(body))
		if json.Unmarshal(body, &e) == nil && e.Message != "" {
			msg = e.Message
		}
		return fmt.Errorf("%s %s: %d: %s", method, path, resp.StatusCode, msg)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decoding response: %w", method, path, err)
	}
	return nil
}
