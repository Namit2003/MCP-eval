package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Namit2003/mcp-eval/internal/tests"
)

// stepServer records every request and answers with status and body.
type stepServer struct {
	status int
	body   string

	mu   sync.Mutex
	reqs []string // "METHOD path auth content-type body"
}

func newStepServer(t *testing.T, s *stepServer) string {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.reqs = append(s.reqs, strings.Join([]string{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), string(b)}, " "))
		s.mu.Unlock()
		w.WriteHeader(s.status)
		io.WriteString(w, s.body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestRunStep(t *testing.T) {
	t.Setenv("TEFRUN_TEST_PAT", "secret")
	s := &stepServer{status: 201, body: `{"number": 12, "data": {"id": "abc", "ok": true}, "items": [{"number": 9}]}`}
	url := newStepServer(t, s)
	step := tests.Step{
		Method: "POST", URL: url + "/issues/${job}", AuthEnv: "TEFRUN_TEST_PAT",
		Body: json.RawMessage(`{"title": "t ${job}"}`),
		Save: map[string]string{"issue": "number", "id": "data.id", "ok": "data.ok", "first": "items.0.number"},
	}
	vars := map[string]string{"job": "7-1"}
	if err := runStep(context.Background(), step, vars); err != nil {
		t.Fatal(err)
	}
	if want := `POST /issues/7-1 Bearer secret application/json {"title":"t 7-1"}`; s.reqs[0] != want {
		t.Errorf("request %q, want %q", s.reqs[0], want)
	}
	if vars["issue"] != "12" || vars["id"] != "abc" || vars["ok"] != "true" || vars["first"] != "9" {
		t.Errorf("vars %v", vars)
	}
}

func TestRunStepErrors(t *testing.T) {
	url := newStepServer(t, &stepServer{status: 404, body: `{"message": "Not Found"}`})
	err := runStep(context.Background(), tests.Step{Method: "GET", URL: url + "/x"}, nil)
	if err == nil || !strings.Contains(err.Error(), `404 Not Found: {"message": "Not Found"}`) {
		t.Errorf("non-2xx: got %v", err)
	}

	t.Setenv("TEFRUN_TEST_PAT", "")
	err = runStep(context.Background(), tests.Step{Method: "GET", URL: url, AuthEnv: "TEFRUN_TEST_PAT"}, nil)
	if err == nil || !strings.Contains(err.Error(), "TEFRUN_TEST_PAT is not set") {
		t.Errorf("missing token: got %v", err)
	}

	ok := newStepServer(t, &stepServer{status: 200, body: `{"a": 1, "items": []}`})
	for _, path := range []string{"a.b", "items.0.number", "items.x"} {
		err = runStep(context.Background(), tests.Step{Method: "GET", URL: ok, Save: map[string]string{"x": path}}, map[string]string{})
		if err == nil || !strings.Contains(err.Error(), `save "x": path "`+path+`" not found`) {
			t.Errorf("missing path %q: got %v", path, err)
		}
	}
}

func TestRunTeardownIgnoresCancel(t *testing.T) {
	s := &stepServer{status: 200}
	url := newStepServer(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	steps := []tests.Step{{Method: "DELETE", URL: url + "/a"}, {Method: "DELETE", URL: url + "/b"}}
	if err := runTeardown(ctx, steps, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.reqs) != 2 {
		t.Errorf("teardown sent %d requests, want 2", len(s.reqs))
	}
}

func stepCase(name, url string) tests.Case {
	return tests.Case{
		Name:     name,
		Raw:      json.RawMessage(`{"name": "` + name + `-${issue}"}`),
		Setup:    []tests.Step{{Method: "POST", URL: url + "/create/${job}", Save: map[string]string{"issue": "number"}}},
		Teardown: []tests.Step{{Method: "DELETE", URL: url + "/issues/${issue}"}},
	}
}

func TestRunSetupTeardown(t *testing.T) {
	f := &fakeTef{pollsToFinish: 1}
	client := newFakeTef(t, f)
	s := &stepServer{status: 200, body: `{"number": 5}`}
	suite := &tests.Suite{Files: []tests.File{{Path: "w.json", Cases: []tests.Case{stepCase("w", newStepServer(t, s))}}}}
	results := Run(context.Background(), client, suite, ollama[:1], opts(1, 5*time.Second))

	r := results[0]
	if r.Status != "completed" || r.TestCaseID != "case-w-5" || r.TeardownError != "" {
		t.Errorf("result %+v", r)
	}
	if len(s.reqs) != 2 || !strings.HasPrefix(s.reqs[0], "POST /create/"+r.JobID) || !strings.HasPrefix(s.reqs[1], "DELETE /issues/5") {
		t.Errorf("step requests %q", s.reqs)
	}
}

func TestRunSetupSharedByModels(t *testing.T) {
	f := &fakeTef{pollsToFinish: 1}
	client := newFakeTef(t, f)
	s := &stepServer{status: 200, body: `{"number": 5}`}
	c := stepCase("w", newStepServer(t, s))
	// Indented like a real file, so the filled-in body fits in its memory.
	c.Setup[0].Body = json.RawMessage(`{
        "title": "tefrun ${job}"
      }`)
	models := append(ollama, ollama...)
	suite := &tests.Suite{Files: []tests.File{{Path: "w.json", Cases: []tests.Case{c}}}}
	results := Run(context.Background(), client, suite, models, opts(2, 5*time.Second))

	for _, r := range results {
		if r.Status != "completed" {
			t.Errorf("job %s: %s %s", r.JobID, r.Status, r.FailureReason)
		}
	}
	if got := string(c.Setup[0].Body); !strings.Contains(got, `"tefrun ${job}"`) {
		t.Errorf("case body changed to %q", got)
	}
}

func TestRunSetupFails(t *testing.T) {
	f := &fakeTef{pollsToFinish: 1}
	client := newFakeTef(t, f)
	s := &stepServer{status: 500, body: "down"}
	suite := &tests.Suite{Files: []tests.File{{Path: "w.json", Cases: []tests.Case{stepCase("w", newStepServer(t, s))}}}}
	r := Run(context.Background(), client, suite, ollama[:1], opts(1, 5*time.Second))[0]

	if r.Status != "failed" || !strings.HasPrefix(r.FailureReason, "setup[0]: POST ") || len(f.created) != 0 {
		t.Errorf("result %+v, created %v", r, f.created)
	}
	// Teardown still runs; ${issue} was never saved, so it fails and is reported.
	if !strings.Contains(r.TeardownError, "undefined variables (set with --var): issue") {
		t.Errorf("teardown error %q", r.TeardownError)
	}
}
