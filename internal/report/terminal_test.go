package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/Namit2003/mcp-eval/internal/runner"
)

func TestTerminalProgress(t *testing.T) {
	var buf bytes.Buffer
	term := NewTerminal(&buf)
	ok := completed("ollama/m", "TP", 100, 1234*ms)
	ok.CaseName = "Search repos"
	term.Progress(3, 10, ok)
	bad := failed("ollama/m", time.Second)
	bad.CaseName = "Get issue"
	term.Progress(4, 10, bad)

	want := "[3/10] Search repos × ollama/m  completed TP  1.23s\n" +
		"[4/10] Get issue × ollama/m  failed timeout  1.00s\n"
	if buf.String() != want {
		t.Errorf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestTerminalNeverRan(t *testing.T) {
	var buf bytes.Buffer
	r := failed("ollama/m", 0)
	r.CaseName = "c"
	NewTerminal(&buf).Progress(1, 1, r)
	if want := "[1/1] c × ollama/m  failed timeout  -\n"; buf.String() != want {
		t.Errorf("got %q, want %q", buf.String(), want)
	}
}

func TestTerminalResults(t *testing.T) {
	long := failed("b", time.Second)
	long.CaseName = "case-long"
	long.FailureReason = strings.Repeat("Z", 100)
	results := []runner.Result{
		completed("a", "TP", 100, 150*ms),
		completed("a", "FN", 300, 350*ms),
		long,
	}
	results[0].CaseName, results[1].CaseName = "case-1", "case-2"

	var buf bytes.Buffer
	NewTerminal(&buf).Results(results, Summarize(results))
	out := buf.String()

	if strings.Contains(out, "\x1b[") {
		t.Errorf("non-terminal output should have no color codes:\n%s", out)
	}
	for _, want := range []string{
		"case-1", "case-2", "case-long",
		"Pass rate 50.0%  completed 2  failed 0  TP 1  FP 0  TN 0  FN 1",
		"Latency  exec avg 200ms p50 100ms p95 300ms  wall avg 250ms p50 150ms p95 350ms",
		"Pass rate 0.0%  completed 0  failed 1",
		"Latency  exec n/a  wall n/a",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	// The 100-character reason is wrapped, not cut: all of it is shown.
	if strings.Count(out, "Z") != 100 || strings.Contains(out, strings.Repeat("Z", 61)) {
		t.Errorf("failure reason should wrap at 60 characters without losing text:\n%s", out)
	}
	if a, b := strings.Index(out, "case-2"), strings.Index(out, "case-long"); a > b {
		t.Error("model a's table should come before model b's")
	}
}
