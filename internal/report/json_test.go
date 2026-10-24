package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Namit2003/mcp-eval/internal/runner"
)

func TestWriteJSON(t *testing.T) {
	results := []runner.Result{
		completed("a", "TP", 100, 1500*ms),
		failed("a", 2*time.Second),
	}
	meta := Metadata{
		Timestamp:    time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
		McpTefRef:    "main",
		McpTefCommit: "1553c31",
		Flags:        map[string]string{"parallel": "4"},
	}
	path := filepath.Join(t.TempDir(), "results.json")
	if err := WriteJSON(path, meta, Summarize(results), results); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Metadata  map[string]any   `json:"metadata"`
		Summaries []map[string]any `json:"summaries"`
		Jobs      []map[string]any `json:"jobs"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}

	if got.Metadata["mcp_tef_commit"] != "1553c31" || got.Metadata["timestamp"] != "2026-10-24T12:00:00Z" {
		t.Errorf("metadata: %v", got.Metadata)
	}
	s := got.Summaries[0]
	if s["model"] != "a" || s["pass_rate"] != 0.5 || s["failed"] != 1.0 {
		t.Errorf("summary: %v", s)
	}
	if exec := s["execution_time"].(map[string]any); exec["p95_ms"] != 100.0 || exec["total_ms"] != 100.0 {
		t.Errorf("execution_time: %v", exec)
	}
	ok, bad := got.Jobs[0], got.Jobs[1]
	if ok["wall_clock_ms"] != 1500.0 || ok["execution_time_ms"] != 100.0 || ok["classification"] != "TP" {
		t.Errorf("completed job: %v", ok)
	}
	if _, present := ok["failure_reason"]; present {
		t.Errorf("completed job should omit failure_reason: %v", ok)
	}
	if _, present := ok["WallClock"]; present {
		t.Errorf("raw WallClock duration should not be written: %v", ok)
	}
	if bad["status"] != "failed" || bad["failure_reason"] != "timeout" || bad["wall_clock_ms"] != 2000.0 {
		t.Errorf("failed job: %v", bad)
	}
}
