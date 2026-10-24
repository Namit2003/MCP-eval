package teff

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCreateTestCase(t *testing.T) {
	raw := `{"name": "a", "query": "q", "available_mcp_servers": ["http://s"], "extra": 1}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/test-cases" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		var got, want map[string]any
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		json.Unmarshal([]byte(raw), &want)
		if !jsonEqual(got, want) {
			t.Errorf("body %s, want %s", body, raw)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": "case-1", "name": "a"}`))
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL).CreateTestCase(context.Background(), json.RawMessage(raw))
	if err != nil || id != "case-1" {
		t.Fatalf("got (%q, %v)", id, err)
	}
}

func TestRunTestCase(t *testing.T) {
	temp := 0.0
	cases := []struct {
		name       string
		settings   ModelSettings
		apiKey     string
		wantBody   string
		wantHeader string
	}{
		{
			name:       "with key and settings",
			settings:   ModelSettings{Provider: "openrouter", Model: "m", Temperature: &temp},
			apiKey:     "secret",
			wantBody:   `{"model_settings": {"provider": "openrouter", "model": "m", "temperature": 0}}`,
			wantHeader: "secret",
		},
		{
			name:     "keyless, defaults omitted",
			settings: ModelSettings{Provider: "ollama", Model: "m"},
			wantBody: `{"model_settings": {"provider": "ollama", "model": "m"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/test-cases/case-1/run" {
					t.Errorf("path %s", r.URL.Path)
				}
				if got := r.Header.Get("X-Model-API-Key"); got != tc.wantHeader {
					t.Errorf("X-Model-API-Key = %q, want %q", got, tc.wantHeader)
				}
				var got, want map[string]any
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &got)
				json.Unmarshal([]byte(tc.wantBody), &want)
				if !jsonEqual(got, want) {
					t.Errorf("body %s, want %s", body, tc.wantBody)
				}
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(`{"id": "run-1", "test_case_id": "case-1", "status": "pending"}`))
			}))
			defer srv.Close()

			run, err := NewClient(srv.URL).RunTestCase(context.Background(), "case-1", tc.settings, tc.apiKey)
			if err != nil || run.ID != "run-1" || run.Status != "pending" {
				t.Fatalf("got (%+v, %v)", run, err)
			}
		})
	}
}

func TestHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"error": "ValidationError", "message": "MCP server not reachable", "details": null}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).CreateTestCase(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected an error")
	}
	if want := "POST /test-cases: 422: MCP server not reachable"; err.Error() != want {
		t.Errorf("got %q, want %q", err, want)
	}
}

func TestHTTPErrorWithoutMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"detail": [{"msg": "Field required"}]}`))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL).CreateTestCase(context.Background(), json.RawMessage(`{}`))
	if want := `POST /test-cases: 422: {"detail": [{"msg": "Field required"}]}`; err == nil || err.Error() != want {
		t.Errorf("got %v, want %q", err, want)
	}
}

// runServer serves GET /test-runs/run-1, returning the bodies in order and
// repeating the last one.
func runServer(t *testing.T, bodies ...string) (*httptest.Server, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/test-runs/run-1" {
			t.Errorf("path %s", r.URL.Path)
		}
		n := int(calls.Add(1))
		w.Write([]byte(bodies[min(n, len(bodies))-1]))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func TestWaitForRunCompleted(t *testing.T) {
	srv, calls := runServer(t,
		`{"id": "run-1", "status": "pending"}`,
		`{"id": "run-1", "status": "running"}`,
		`{"id": "run-1", "status": "completed", "classification": "TP", "execution_time_ms": 1234,
		  "tool_call_matches": [{"match_type": "TP", "expected_tool_call": {"mcp_server_url": "http://s", "tool_name": "search_repositories", "parameters": null},
		    "actual_tool_call": {"name": "search_repositories", "mcp_server_url": "http://s"}, "parameter_correctness": 9.5, "parameter_justification": "close"}]}`,
	)
	run, err := NewClient(srv.URL).WaitForRun(context.Background(), "run-1", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("polled %d times, want 3", calls.Load())
	}
	if run.Status != "completed" || run.Classification != "TP" || *run.ExecutionTimeMS != 1234 {
		t.Errorf("unexpected run: %+v", run)
	}
	m := run.ToolCallMatches[0]
	if m.ActualToolCall.Name != "search_repositories" || *m.ParameterCorrectness != 9.5 || m.ParameterJustification != "close" {
		t.Errorf("unexpected match: %+v", m)
	}
}

func TestWaitForRunFailed(t *testing.T) {
	srv, _ := runServer(t, `{"id": "run-1", "status": "failed", "classification": null, "error_message": "Tool ingestion failed: boom"}`)
	run, err := NewClient(srv.URL).WaitForRun(context.Background(), "run-1", time.Millisecond)
	if err != nil {
		t.Fatalf("a failed run is not an error: %v", err)
	}
	if run.Status != "failed" || run.ErrorMessage != "Tool ingestion failed: boom" || run.Classification != "" {
		t.Errorf("unexpected run: %+v", run)
	}
}

func TestWaitForRunTimeout(t *testing.T) {
	srv, _ := runServer(t, `{"id": "run-1", "status": "pending"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := NewClient(srv.URL).WaitForRun(ctx, "run-1", 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want DeadlineExceeded", err)
	}
}

func TestRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until the client gives up
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := NewClient(srv.URL).GetTestRun(ctx, "run-1")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want DeadlineExceeded", err)
	}
}

func jsonEqual(a, b map[string]any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
