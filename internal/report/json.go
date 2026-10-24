package report

import (
	"encoding/json"
	"os"
	"time"

	"github.com/Namit2003/mcp-eval/internal/runner"
)

// Metadata describes a run in results.json.
type Metadata struct {
	Timestamp    time.Time         `json:"timestamp"`
	McpTefRef    string            `json:"mcp_tef_ref"`
	McpTefCommit string            `json:"mcp_tef_commit"`
	Flags        map[string]string `json:"flags"`
}

// jobJSON is a result as written to results.json, with wall-clock time in
// milliseconds like mcp-tef's execution_time_ms.
type jobJSON struct {
	runner.Result
	WallClockMS int64 `json:"wall_clock_ms"`
}

// MarshalJSON writes latencies as milliseconds.
func (l Latency) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]int64{
		"avg_ms":   l.Avg.Milliseconds(),
		"p50_ms":   l.P50.Milliseconds(),
		"p95_ms":   l.P95.Milliseconds(),
		"total_ms": l.Total.Milliseconds(),
	})
}

// WriteJSON writes run metadata, per-model summaries and every job to path.
func WriteJSON(path string, meta Metadata, summaries []ModelSummary, results []runner.Result) error {
	jobs := make([]jobJSON, len(results))
	for i, r := range results {
		jobs[i] = jobJSON{Result: r, WallClockMS: r.WallClock.Milliseconds()}
	}
	data, err := json.MarshalIndent(struct {
		Metadata  Metadata       `json:"metadata"`
		Summaries []ModelSummary `json:"summaries"`
		Jobs      []jobJSON      `json:"jobs"`
	}{meta, summaries, jobs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}
