package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/Namit2003/mcp-eval/internal/config"
	"github.com/Namit2003/mcp-eval/internal/teff"
	"github.com/Namit2003/mcp-eval/internal/tests"
)

// Options controls how a run is executed.
type Options struct {
	Parallel     int           // max jobs in flight across all models
	Timeout      time.Duration // global deadline for the whole run
	PollInterval time.Duration // how often to poll a test run's status

	// OnDone, if set, is called after each job finishes with the number of
	// jobs done so far and the total. Calls never overlap.
	OnDone func(done, total int, r Result)
}

// Result is the outcome of one (test case × model) job.
type Result struct {
	JobID      string `json:"job_id"` // run start (unix seconds) and job number; fills ${job}
	CaseName   string `json:"case_name"`
	SourceFile string `json:"source_file"`
	Model      string `json:"model"`

	Status        string `json:"status"` // "completed" or "failed"
	FailureReason string `json:"failure_reason,omitempty"`

	Classification          string               `json:"classification"`
	LLMConfidence           string               `json:"llm_confidence"`
	ConfidenceScore         string               `json:"confidence_score"`
	AvgParameterCorrectness *float64             `json:"avg_parameter_correctness"`
	ToolCallMatches         []teff.ToolCallMatch `json:"tool_call_matches"`
	LLMResponseRaw          string               `json:"llm_response_raw"`

	ExecutionTimeMS *int          `json:"execution_time_ms"` // measured by mcp-tef
	WallClock       time.Duration `json:"-"`                 // measured by tefrun, from run request to final status; written as wall_clock_ms by the report

	TestCaseID string `json:"test_case_id"`
	TestRunID  string `json:"test_run_id"`

	TeardownError string `json:"teardown_error,omitempty"` // the job's result stands; clean up by hand
}

// Run creates every test case once in mcp-tef, then runs each one on every
// model, at most opts.Parallel jobs at a time. A case that uses ${job} or has
// setup or teardown steps is instead created inside each of its jobs. It
// returns one result per job, in file, case, model order. A failed job never
// stops the run.
func Run(ctx context.Context, client *teff.Client, suite *tests.Suite, models []config.Model, opts Options) []Result {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	n := suite.NumCases() * len(models)
	var mu sync.Mutex
	done := 0
	finished := func(r Result) {
		if opts.OnDone == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		done++
		opts.OnDone(done, n, r)
	}

	runStart := time.Now().Unix()
	results := make([]Result, 0, n)
	jobModels := make([]config.Model, 0, n) // jobModels[i] is the model for results[i]
	jobCases := make([]*tests.Case, 0, n)   // jobCases[i] is set when results[i] creates its own test case
	for _, f := range suite.Files {
		for _, c := range f.Cases {
			perJob := bytes.Contains(c.Raw, []byte(tests.JobRef)) || len(c.Setup) > 0 || len(c.Teardown) > 0
			var id string
			var err error
			if !perJob {
				id, err = client.CreateTestCase(ctx, c.Raw)
			}
			for _, m := range models {
				r := Result{JobID: fmt.Sprintf("%d-%d", runStart, len(results)+1), CaseName: c.Name, SourceFile: f.Path, Model: m.Label(), TestCaseID: id}
				if err != nil {
					r.Status = "failed"
					r.FailureReason = failureReason(ctx, fmt.Errorf("create test case: %w", err))
					finished(r)
				}
				results = append(results, r)
				jobModels = append(jobModels, m)
				if perJob {
					jobCases = append(jobCases, &c)
				} else {
					jobCases = append(jobCases, nil)
				}
			}
		}
	}

	// A plain Group, not WithContext: one job failing must not cancel the rest.
	var g errgroup.Group
	g.SetLimit(opts.Parallel)
	for i := range results {
		if results[i].Status == "failed" {
			continue
		}
		g.Go(func() error {
			// Each job writes only results[i], so no locking is needed.
			runJob(ctx, client, &results[i], jobModels[i], jobCases[i], opts.PollInterval)
			finished(results[i])
			return nil
		})
	}
	g.Wait()
	return results
}

// runJob runs one job. When c is set, the job first runs c's setup and
// creates its own test case, and always runs c's teardown at the end.
func runJob(ctx context.Context, client *teff.Client, r *Result, m config.Model, c *tests.Case, poll time.Duration) {
	fail := func(err error) {
		r.Status = "failed"
		r.FailureReason = failureReason(ctx, err)
	}
	if c != nil {
		vars := map[string]string{"job": r.JobID}
		defer func() {
			if err := runTeardown(ctx, c.Teardown, vars); err != nil {
				r.TeardownError = err.Error()
			}
		}()
		for i, s := range c.Setup {
			if err := runStep(ctx, s, vars); err != nil {
				fail(fmt.Errorf("setup[%d]: %w", i, err))
				return
			}
		}
		raw, err := tests.Substitute(c.Raw, vars, nil)
		if err != nil {
			fail(err)
			return
		}
		if r.TestCaseID, err = client.CreateTestCase(ctx, raw); err != nil {
			fail(fmt.Errorf("create test case: %w", err))
			return
		}
	}

	// Wall-clock time covers only the model run, not setup or teardown.
	start := time.Now()
	defer func() { r.WallClock = time.Since(start) }()
	apiKey, err := m.ResolveAPIKey(os.LookupEnv)
	if err != nil {
		fail(err)
		return
	}
	settings := teff.ModelSettings{
		Provider:    m.Provider,
		Model:       m.Model,
		Timeout:     m.Timeout,
		Temperature: m.Temperature,
		MaxRetries:  m.MaxRetries,
	}
	run, err := client.RunTestCase(ctx, r.TestCaseID, settings, apiKey)
	if err != nil {
		fail(err)
		return
	}
	r.TestRunID = run.ID
	run, err = client.WaitForRun(ctx, run.ID, poll)
	if err != nil {
		fail(err)
		return
	}

	r.Status = run.Status
	if run.Status == "failed" {
		r.FailureReason = "mcp-tef: " + run.ErrorMessage
	}
	r.Classification = run.Classification
	r.LLMConfidence = run.LLMConfidence
	r.ConfidenceScore = run.ConfidenceScore
	r.AvgParameterCorrectness = run.AvgParameterCorrectness
	r.ToolCallMatches = run.ToolCallMatches
	r.LLMResponseRaw = run.LLMResponseRaw
	r.ExecutionTimeMS = run.ExecutionTimeMS
}

// failureReason reports "timeout" or "cancelled" when the run's context
// ended, since that is the real cause of whatever err a call returned.
func failureReason(ctx context.Context, err error) string {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return "timeout"
	case errors.Is(ctx.Err(), context.Canceled):
		return "cancelled"
	}
	return err.Error()
}
