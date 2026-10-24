package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
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
}

// Result is the outcome of one (test case × model) job.
type Result struct {
	CaseName   string
	SourceFile string
	Model      string

	Status        string // "completed" or "failed"
	FailureReason string

	Classification          string
	LLMConfidence           string
	ConfidenceScore         string
	AvgParameterCorrectness *float64
	ToolCallMatches         []teff.ToolCallMatch
	LLMResponseRaw          string

	ExecutionTimeMS *int          // measured by mcp-tef
	WallClock       time.Duration // measured by tefrun, from run request to final status

	TestCaseID string
	TestRunID  string
}

// Run creates every test case once in mcp-tef, then runs each one on every
// model, at most opts.Parallel jobs at a time. It returns one result per
// job, in file, case, model order. A failed job never stops the run.
func Run(ctx context.Context, client *teff.Client, suite *tests.Suite, models []config.Model, opts Options) []Result {
	ctx, cancel := context.WithTimeout(ctx, opts.Timeout)
	defer cancel()

	n := suite.NumCases() * len(models)
	results := make([]Result, 0, n)
	jobModels := make([]config.Model, 0, n) // jobModels[i] is the model for results[i]
	for _, f := range suite.Files {
		for _, c := range f.Cases {
			id, err := client.CreateTestCase(ctx, c.Raw)
			for _, m := range models {
				r := Result{CaseName: c.Name, SourceFile: f.Path, Model: m.Label(), TestCaseID: id}
				if err != nil {
					r.Status = "failed"
					r.FailureReason = failureReason(ctx, fmt.Errorf("create test case: %w", err))
				}
				results = append(results, r)
				jobModels = append(jobModels, m)
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
			runJob(ctx, client, &results[i], jobModels[i], opts.PollInterval)
			return nil
		})
	}
	g.Wait()
	return results
}

func runJob(ctx context.Context, client *teff.Client, r *Result, m config.Model, poll time.Duration) {
	start := time.Now()
	defer func() { r.WallClock = time.Since(start) }()

	fail := func(err error) {
		r.Status = "failed"
		r.FailureReason = failureReason(ctx, err)
	}
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
