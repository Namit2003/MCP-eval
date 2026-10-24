package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Namit2003/mcp-eval/internal/runner"
	"github.com/Namit2003/mcp-eval/internal/teff"
)

func TestWriteHTML(t *testing.T) {
	score := 9.5
	ok := completed("ollama/m1", "TP", 100, 150*ms)
	ok.CaseName = "Search repos"
	ok.ToolCallMatches = []teff.ToolCallMatch{{
		MatchType:              "TP",
		ExpectedToolCall:       &teff.ExpectedToolCall{ToolName: "search_repositories", Parameters: map[string]any{"query": "MCP tools"}},
		ActualToolCall:         &teff.ActualToolCall{Name: "search_repositories"},
		ActualParameters:       map[string]any{"query": "MCP"},
		ParameterCorrectness:   &score,
		ParameterJustification: "close enough",
	}}
	ok.LLMResponseRaw = "TOOL CALL. TOOL: search_repositories"
	bad := failed("ollama/m2", time.Second)
	bad.CaseName = "Evil"
	bad.FailureReason = "<script>alert(1)</script>"
	results := []runner.Result{ok, bad}

	path := filepath.Join(t.TempDir(), "report.html")
	meta := Metadata{Timestamp: time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC), McpTefRef: "main", McpTefCommit: "1553c31"}
	if err := WriteHTML(path, meta, Summarize(results), results); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)

	for _, want := range []string{
		"2026-10-24 12:00:00 UTC", "1553c31",
		"ollama/m1", "ollama/m2",
		`<div class="rate">100.0%`, `<div class="rate">0.0%`, // pass rate per card
		"<th>Exec</th><td>100ms</td><td>100ms</td><td>100ms</td>", `<td colspan="3">n/a</td>`,
		"ollama/<wbr>m1", `data-model="ollama/m1"`, `<tr class="details" hidden>`, `class="toggle"`,
		`<tbody class="job failed"`, `data-status="failed"`,
		"search_repositories", "close enough", "9.5", "&#34;query&#34;: &#34;MCP tools&#34;",
		"TOOL CALL. TOOL: search_repositories",
		"&lt;script&gt;alert(1)&lt;/script&gt;", // LLM/mcp-tef text is escaped
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if strings.Contains(out, "<script>alert(1)") {
		t.Error("failure reason was not escaped")
	}
	// Self-contained: no external scripts, stylesheets or fonts.
	for _, external := range []string{"<script src", "<link", "@import", "url(http"} {
		if strings.Contains(out, external) {
			t.Errorf("report references an external resource: %q", external)
		}
	}
}
