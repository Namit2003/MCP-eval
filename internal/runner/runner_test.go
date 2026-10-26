package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Namit2003/mcp-eval/internal/config"
	"github.com/Namit2003/mcp-eval/internal/teff"
	"github.com/Namit2003/mcp-eval/internal/tests"
)

// fakeTef is a minimal mcp-tef: runs finish after pollsToFinish polls
// (never if < 0), and it records how many runs are in flight at once.
type fakeTef struct {
	pollsToFinish int
	runStatus     string          // final status: "completed" or "failed"
	failCreate    map[string]bool // case names whose creation returns 422

	mu      sync.Mutex
	nextRun int
	polls   map[string]int
	active  int
	peak    int
	runsFor map[string]int // test case id -> runs started
	apiKeys []string
	created []string // names of created test cases, in order
}

func newFakeTef(t *testing.T, f *fakeTef) *teff.Client {
	f.polls = map[string]int{}
	f.runsFor = map[string]int{}
	if f.runStatus == "" {
		f.runStatus = "completed"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /test-cases", func(w http.ResponseWriter, r *http.Request) {
		var c struct{ Name string }
		json.NewDecoder(r.Body).Decode(&c)
		f.mu.Lock()
		f.created = append(f.created, c.Name)
		f.mu.Unlock()
		if f.failCreate[c.Name] {
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"message": "MCP server not reachable"}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id": "case-%s"}`, c.Name)
	})
	mux.HandleFunc("POST /test-cases/{id}/run", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.nextRun++
		id := fmt.Sprintf("run-%d", f.nextRun)
		f.polls[id] = 0
		f.runsFor[r.PathValue("id")]++
		f.apiKeys = append(f.apiKeys, r.Header.Get("X-Model-API-Key"))
		f.active++
		f.peak = max(f.peak, f.active)
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"id": %q, "status": "pending"}`, id)
	})
	mux.HandleFunc("GET /test-runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		f.mu.Lock()
		defer f.mu.Unlock()
		f.polls[id]++
		if f.pollsToFinish < 0 || f.polls[id] < f.pollsToFinish {
			fmt.Fprintf(w, `{"id": %q, "status": "pending"}`, id)
			return
		}
		if f.polls[id] == f.pollsToFinish {
			f.active--
		}
		fmt.Fprintf(w, `{"id": %q, "status": %q, "classification": "TP", "execution_time_ms": 5, "error_message": "boom"}`, id, f.runStatus)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return teff.NewClient(srv.URL)
}

func suiteOf(names ...string) *tests.Suite {
	var cases []tests.Case
	for _, n := range names {
		cases = append(cases, tests.Case{Name: n, Raw: json.RawMessage(fmt.Sprintf(`{"name": %q}`, n))})
	}
	return &tests.Suite{Files: []tests.File{{Path: "evals/a.json", Cases: cases}}}
}

var ollama = []config.Model{{Provider: "ollama", Model: "m1"}, {Provider: "ollama", Model: "m2"}}

func opts(parallel int, timeout time.Duration) Options {
	return Options{Parallel: parallel, Timeout: timeout, PollInterval: 5 * time.Millisecond}
}

func TestRunCompleted(t *testing.T) {
	t.Setenv("TEFRUN_TEST_KEY", "secret")
	models := []config.Model{{Provider: "ollama", Model: "m1"}, {Provider: "openrouter", Model: "m2", APIKeyEnv: "TEFRUN_TEST_KEY"}}
	f := &fakeTef{pollsToFinish: 2}
	results := Run(context.Background(), newFakeTef(t, f), suiteOf("a", "b"), models, opts(4, 5*time.Second))

	if len(results) != 4 {
		t.Fatalf("got %d results, want 4", len(results))
	}
	wantOrder := []string{"a ollama/m1", "a openrouter/m2", "b ollama/m1", "b openrouter/m2"}
	for i, r := range results {
		if got := r.CaseName + " " + r.Model; got != wantOrder[i] {
			t.Errorf("results[%d] = %s, want %s", i, got, wantOrder[i])
		}
		if r.Status != "completed" || r.FailureReason != "" || r.Classification != "TP" || *r.ExecutionTimeMS != 5 {
			t.Errorf("results[%d]: %+v", i, r)
		}
		if r.SourceFile != "evals/a.json" || r.TestCaseID != "case-"+r.CaseName || r.TestRunID == "" || r.WallClock <= 0 {
			t.Errorf("results[%d] missing metadata: %+v", i, r)
		}
	}
	if f.runsFor["case-a"] != 2 || f.runsFor["case-b"] != 2 {
		t.Errorf("each case should run once per model: %v", f.runsFor)
	}
	keys := strings.Join(f.apiKeys, ",")
	if strings.Count(keys, "secret") != 2 {
		t.Errorf("openrouter jobs should send the key, ollama jobs none: %q", keys)
	}
}

func TestRunParallelLimit(t *testing.T) {
	f := &fakeTef{pollsToFinish: 4}
	results := Run(context.Background(), newFakeTef(t, f), suiteOf("a", "b", "c"), ollama, opts(2, 5*time.Second))

	if len(results) != 6 {
		t.Fatalf("got %d results, want 6", len(results))
	}
	for _, r := range results {
		if r.Status != "completed" {
			t.Errorf("%s/%s: %s %s", r.CaseName, r.Model, r.Status, r.FailureReason)
		}
	}
	if f.peak > 2 {
		t.Errorf("peak in-flight runs = %d, limit is 2", f.peak)
	}
	if f.peak < 2 {
		t.Errorf("peak in-flight runs = %d; jobs should run in parallel up to the limit", f.peak)
	}
}

func TestRunGlobalTimeout(t *testing.T) {
	f := &fakeTef{pollsToFinish: -1} // runs never finish
	start := time.Now()
	results := Run(context.Background(), newFakeTef(t, f), suiteOf("a", "b"), ollama, opts(1, 100*time.Millisecond))

	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("run took %s, should stop at the 100ms deadline", elapsed)
	}
	for _, r := range results {
		if r.Status != "failed" || r.FailureReason != "timeout" {
			t.Errorf("%s/%s: got %s %q, want failed timeout", r.CaseName, r.Model, r.Status, r.FailureReason)
		}
	}
}

func TestRunCancelled(t *testing.T) {
	f := &fakeTef{pollsToFinish: -1}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	results := Run(ctx, newFakeTef(t, f), suiteOf("a"), ollama, opts(2, 5*time.Second))
	for _, r := range results {
		if r.FailureReason != "cancelled" {
			t.Errorf("%s: got %q, want cancelled", r.Model, r.FailureReason)
		}
	}
}

func TestRunCreateFails(t *testing.T) {
	f := &fakeTef{pollsToFinish: 1, failCreate: map[string]bool{"b": true}}
	results := Run(context.Background(), newFakeTef(t, f), suiteOf("a", "b"), ollama, opts(2, 5*time.Second))

	for _, r := range results {
		switch r.CaseName {
		case "a":
			if r.Status != "completed" {
				t.Errorf("a/%s: %s %s", r.Model, r.Status, r.FailureReason)
			}
		case "b":
			if r.Status != "failed" || !strings.Contains(r.FailureReason, "create test case") || !strings.Contains(r.FailureReason, "MCP server not reachable") {
				t.Errorf("b/%s: got %s %q", r.Model, r.Status, r.FailureReason)
			}
		}
	}
	if f.nextRun != 2 {
		t.Errorf("started %d runs, want 2 (only case a)", f.nextRun)
	}
}

func TestRunOnDone(t *testing.T) {
	f := &fakeTef{pollsToFinish: 2, failCreate: map[string]bool{"b": true}}
	o := opts(2, 5*time.Second)
	var calls []int
	o.OnDone = func(done, total int, r Result) {
		if total != 4 {
			t.Errorf("total = %d, want 4", total)
		}
		calls = append(calls, done) // safe: calls never overlap
	}
	Run(context.Background(), newFakeTef(t, f), suiteOf("a", "b"), ollama, o)
	if fmt.Sprint(calls) != "[1 2 3 4]" {
		t.Errorf("OnDone done values = %v, want [1 2 3 4]", calls)
	}
}

func TestRunMcpTefFailure(t *testing.T) {
	f := &fakeTef{pollsToFinish: 1, runStatus: "failed"}
	results := Run(context.Background(), newFakeTef(t, f), suiteOf("a"), ollama[:1], opts(1, 5*time.Second))
	if r := results[0]; r.Status != "failed" || r.FailureReason != "mcp-tef: boom" {
		t.Errorf("got %s %q", r.Status, r.FailureReason)
	}
}

func TestRunJobRef(t *testing.T) {
	f := &fakeTef{pollsToFinish: 1}
	client := newFakeTef(t, f)
	suite := suiteOf("r")
	suite.Files[0].Cases = append(suite.Files[0].Cases, tests.Case{Name: "w", Raw: json.RawMessage(`{"name": "w-${job}"}`)})
	results := Run(context.Background(), client, suite, ollama, opts(2, 5*time.Second))

	w1, w2 := results[2], results[3]
	if w1.JobID == w2.JobID || !strings.HasSuffix(w1.JobID, "-3") || !strings.HasSuffix(w2.JobID, "-4") {
		t.Fatalf("job ids %q %q", w1.JobID, w2.JobID)
	}
	want := []string{"r", "w-" + w1.JobID, "w-" + w2.JobID}
	slices.Sort(f.created) // per-job cases are created inside parallel jobs
	if fmt.Sprint(f.created) != fmt.Sprint(want) {
		t.Errorf("created %v, want %v", f.created, want)
	}
	if results[0].TestCaseID != results[1].TestCaseID || w1.TestCaseID != "case-w-"+w1.JobID || w2.TestCaseID != "case-w-"+w2.JobID {
		t.Errorf("test case ids %q %q %q %q", results[0].TestCaseID, results[1].TestCaseID, w1.TestCaseID, w2.TestCaseID)
	}
}
