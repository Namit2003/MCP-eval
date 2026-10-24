package report

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"strings"
	"time"

	"github.com/Namit2003/mcp-eval/internal/runner"
)

//go:embed report.html.tmpl
var htmlTemplate string

var htmlFuncs = template.FuncMap{
	"pct":   func(f float64) string { return fmt.Sprintf("%.1f%%", f*100) },
	"wall":  wall,
	"lat":   latencyText,
	"lower": strings.ToLower,
	"dur":   duration,
	// wbr lets long model names wrap after "/" instead of mid-word.
	"wbr": func(s string) template.HTML {
		return template.HTML(strings.ReplaceAll(template.HTMLEscapeString(s), "/", "/<wbr>"))
	},
	"execMS": func(ms *int) string {
		if ms == nil {
			return "-"
		}
		return duration(time.Duration(*ms) * time.Millisecond)
	},
	"score": func(f *float64) string {
		if f == nil {
			return "-"
		}
		return fmt.Sprintf("%.1f", *f)
	},
	"json": func(v any) string {
		if v == nil {
			return "-"
		}
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(b)
	},
}

var reportTemplate = template.Must(template.New("report").Funcs(htmlFuncs).Parse(htmlTemplate))

// WriteHTML writes a self-contained HTML report to path: no external CSS,
// JS or fonts, so it opens offline.
func WriteHTML(path string, meta Metadata, summaries []ModelSummary, results []runner.Result) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	err = reportTemplate.Execute(f, struct {
		Meta      Metadata
		Summaries []ModelSummary
		Jobs      []runner.Result
	}{meta, summaries, results})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
