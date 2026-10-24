package report

import (
	"slices"
	"time"

	"github.com/Namit2003/mcp-eval/internal/runner"
)

// ModelSummary aggregates one model's results.
type ModelSummary struct {
	Model         string   `json:"model"`
	Total         int      `json:"total"`
	Completed     int      `json:"completed"`
	Failed        int      `json:"failed"`
	TP            int      `json:"tp"`
	FP            int      `json:"fp"`
	TN            int      `json:"tn"`
	FN            int      `json:"fn"`
	PassRate      float64  `json:"pass_rate"`      // (TP + TN) / Total; failed jobs count in Total
	ExecutionTime *Latency `json:"execution_time"` // from mcp-tef's execution_time_ms
	WallClock     *Latency `json:"wall_clock"`     // measured by tefrun
}

// Latency statistics over a model's completed jobs. A nil *Latency means
// there were no measurements.
type Latency struct {
	Avg, P50, P95, Total time.Duration
}

// Summarize computes one summary per model, in the order models first
// appear in results.
func Summarize(results []runner.Result) []ModelSummary {
	var order []string
	byModel := make(map[string][]runner.Result)
	for _, r := range results {
		if _, seen := byModel[r.Model]; !seen {
			order = append(order, r.Model)
		}
		byModel[r.Model] = append(byModel[r.Model], r)
	}

	summaries := make([]ModelSummary, 0, len(order))
	for _, model := range order {
		s := ModelSummary{Model: model}
		var exec, wall []time.Duration
		for _, r := range byModel[model] {
			s.Total++
			if r.Status != "completed" {
				s.Failed++
				continue
			}
			s.Completed++
			switch r.Classification {
			case "TP":
				s.TP++
			case "FP":
				s.FP++
			case "TN":
				s.TN++
			case "FN":
				s.FN++
			}
			wall = append(wall, r.WallClock)
			if r.ExecutionTimeMS != nil {
				exec = append(exec, time.Duration(*r.ExecutionTimeMS)*time.Millisecond)
			}
		}
		s.PassRate = float64(s.TP+s.TN) / float64(s.Total)
		s.ExecutionTime = latency(exec)
		s.WallClock = latency(wall)
		summaries = append(summaries, s)
	}
	return summaries
}

func latency(ds []time.Duration) *Latency {
	if len(ds) == 0 {
		return nil
	}
	sorted := slices.Sorted(slices.Values(ds))
	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	return &Latency{
		Avg:   total / time.Duration(len(sorted)),
		P50:   percentile(sorted, 50),
		P95:   percentile(sorted, 95),
		Total: total,
	}
}

// percentile uses the nearest-rank method: the value at 1-based position
// ceil(p/100 × n) in sorted, which is always a measured value.
func percentile(sorted []time.Duration, p int) time.Duration {
	rank := (p*len(sorted) + 99) / 100
	return sorted[max(rank, 1)-1]
}
