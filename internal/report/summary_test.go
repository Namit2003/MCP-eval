package report

import (
	"testing"
	"time"

	"github.com/Namit2003/mcp-eval/internal/runner"
)

const ms = time.Millisecond

func completed(model, class string, execMS int, wall time.Duration) runner.Result {
	return runner.Result{Model: model, Status: "completed", Classification: class, ExecutionTimeMS: &execMS, WallClock: wall}
}

func failed(model string, wall time.Duration) runner.Result {
	return runner.Result{Model: model, Status: "failed", FailureReason: "timeout", WallClock: wall}
}

func TestSummarize(t *testing.T) {
	results := []runner.Result{
		completed("a", "TP", 100, 150*ms),
		failed("b", time.Second),
		completed("a", "TP", 200, 250*ms),
		completed("a", "FN", 300, 350*ms),
		failed("b", time.Second),
		failed("a", 5*time.Second), // excluded from latency
	}
	got := Summarize(results)
	want := []ModelSummary{
		{
			Model: "a", Total: 4, Completed: 3, Failed: 1, TP: 2, FN: 1, PassRate: 0.5,
			ExecutionTime: Latency{Avg: 200 * ms, P50: 200 * ms, P95: 300 * ms, Total: 600 * ms},
			WallClock:     Latency{Avg: 250 * ms, P50: 250 * ms, P95: 350 * ms, Total: 750 * ms},
		},
		{Model: "b", Total: 2, Failed: 2},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d summaries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("summary %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

func TestSummarizeAllClassifications(t *testing.T) {
	got := Summarize([]runner.Result{
		completed("m", "TP", 1, ms), completed("m", "FP", 1, ms),
		completed("m", "TN", 1, ms), completed("m", "FN", 1, ms),
	})[0]
	if got.TP != 1 || got.FP != 1 || got.TN != 1 || got.FN != 1 || got.PassRate != 0.25 {
		t.Errorf("got %+v", got)
	}
}

func TestPercentile(t *testing.T) {
	var twenty []time.Duration
	for i := 1; i <= 20; i++ {
		twenty = append(twenty, time.Duration(i))
	}
	cases := []struct {
		sorted []time.Duration
		p      int
		want   time.Duration
	}{
		{twenty, 50, 10},
		{twenty, 95, 19},
		{twenty, 100, 20},
		{[]time.Duration{7}, 50, 7},
		{[]time.Duration{7}, 95, 7},
		{[]time.Duration{1, 2}, 50, 1},
		{[]time.Duration{1, 2}, 95, 2},
	}
	for _, tc := range cases {
		if got := percentile(tc.sorted, tc.p); got != tc.want {
			t.Errorf("p%d of %v = %d, want %d", tc.p, tc.sorted, got, tc.want)
		}
	}
}
