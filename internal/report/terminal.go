package report

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"

	"github.com/Namit2003/mcp-eval/internal/runner"
)

// Terminal writes progress and result tables to w. Colors are used only when
// w is a terminal.
type Terminal struct {
	w      io.Writer
	bold   lipgloss.Style
	red    lipgloss.Style
	faint  lipgloss.Style
	header lipgloss.Style
	cell   lipgloss.Style
}

const (
	reasonCol   = 5
	reasonWidth = 62 // 60 characters plus padding
)

// NewTerminal returns a Terminal writing to w.
func NewTerminal(w io.Writer) *Terminal {
	r := lipgloss.NewRenderer(w)
	return &Terminal{
		w:      w,
		bold:   r.NewStyle().Bold(true),
		red:    r.NewStyle().Foreground(lipgloss.Color("1")),
		faint:  r.NewStyle().Faint(true),
		header: r.NewStyle().Bold(true).Padding(0, 1),
		cell:   r.NewStyle().Padding(0, 1),
	}
}

// Progress prints one line for a finished job.
func (t *Terminal) Progress(done, total int, r runner.Result) {
	outcome := r.Status + " " + r.Classification
	if r.Status != "completed" {
		outcome = t.red.Render(r.Status + " " + r.FailureReason)
	}
	fmt.Fprintf(t.w, "[%d/%d] %s × %s  %s  %s\n", done, total, r.CaseName, r.Model, outcome, t.faint.Render(wall(r.WallClock)))
	if r.TeardownError != "" {
		// One line per failed step, rendered separately so lipgloss does not
		// pad them to the same width.
		prefix := "  teardown failed, clean up by hand: "
		for _, line := range strings.Split(r.TeardownError, "\n") {
			fmt.Fprintln(t.w, t.red.Render(prefix+line))
			prefix = "    "
		}
	}
}

// Results prints one table per model followed by its summary.
func (t *Terminal) Results(results []runner.Result, summaries []ModelSummary) {
	for _, s := range summaries {
		var rows [][]string
		var failed []bool
		for _, r := range results {
			if r.Model != s.Model {
				continue
			}
			exec := "-"
			if r.ExecutionTimeMS != nil {
				exec = duration(time.Duration(*r.ExecutionTimeMS) * time.Millisecond)
			}
			rows = append(rows, []string{r.CaseName, r.Status, r.Classification, exec, wall(r.WallClock), r.FailureReason})
			failed = append(failed, r.Status != "completed")
		}
		tbl := table.New().
			Headers("Case", "Status", "Class", "Exec", "Wall", "Failure reason").
			Rows(rows...).
			StyleFunc(func(row, col int) lipgloss.Style {
				s := t.cell
				switch {
				case row == table.HeaderRow:
					s = t.header
				case failed[row]:
					s = t.red.Padding(0, 1)
				}
				if col == reasonCol {
					s = s.Width(reasonWidth) // wrap long reasons onto more lines
				}
				return s
			})

		fmt.Fprintf(t.w, "\n%s\n%s\n", t.bold.Render(s.Model), tbl.Render())
		fmt.Fprintf(t.w, "%s %.1f%%  completed %d  failed %d  TP %d  FP %d  TN %d  FN %d\n",
			t.bold.Render("Pass rate"), s.PassRate*100, s.Completed, s.Failed, s.TP, s.FP, s.TN, s.FN)
		fmt.Fprintf(t.w, "Latency  exec %s  wall %s\n", latencyText(s.ExecutionTime), latencyText(s.WallClock))
	}
}

func latencyText(l *Latency) string {
	if l == nil {
		return "n/a"
	}
	return fmt.Sprintf("avg %s p50 %s p95 %s", duration(l.Avg), duration(l.P50), duration(l.P95))
}

// duration formats d as whole milliseconds below one second, otherwise as
// seconds with two decimals, so values line up.
func duration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// wall formats a job's wall-clock time; zero means the job never ran
// (for example, its test case could not be created).
func wall(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	return duration(d)
}
