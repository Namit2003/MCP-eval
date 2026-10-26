package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/Namit2003/mcp-eval/internal/report"
	"github.com/Namit2003/mcp-eval/internal/runner"
	"github.com/Namit2003/mcp-eval/internal/teff"
)

const mcpTefRepo = "https://github.com/StacklokLabs/mcp-tef.git"

type runFlags struct {
	inputFlags
	parallel     int
	timeout      time.Duration
	jobTimeout   time.Duration
	pollInterval time.Duration
	out          string
	mcpTefDir    string
	mcpTefRef    string
	port         int
}

func (f *runFlags) check() error {
	var errs []error
	if f.parallel < 1 {
		errs = append(errs, fmt.Errorf("--parallel must be >= 1, got %d", f.parallel))
	}
	if f.timeout <= 0 {
		errs = append(errs, fmt.Errorf("--timeout must be > 0, got %s", f.timeout))
	}
	if f.jobTimeout <= 0 {
		errs = append(errs, fmt.Errorf("--job-timeout must be > 0, got %s", f.jobTimeout))
	}
	if f.pollInterval <= 0 {
		errs = append(errs, fmt.Errorf("--poll-interval must be > 0, got %s", f.pollInterval))
	}
	if f.port < 1 || f.port > 65535 {
		errs = append(errs, fmt.Errorf("--port must be between 1 and 65535, got %d", f.port))
	}
	if f.out == "" {
		errs = append(errs, errors.New("--out must not be empty"))
	}
	if f.mcpTefDir == "" {
		errs = append(errs, errors.New("--mcp-tef-dir must not be empty"))
	}
	if f.mcpTefRef == "" {
		errs = append(errs, errors.New("--mcp-tef-ref must not be empty"))
	}
	return errors.Join(errs...)
}

func newRunCmd() *cobra.Command {
	var f runFlags
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run every test case on every model through mcp-tef and write reports",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := f.check(); err != nil {
				return err
			}
			loaded, err := f.load()
			if err != nil {
				return err
			}
			printSummary(cmd.OutOrStdout(), loaded)
			started := time.Now()
			commit, err := teff.Setup(cmd.Context(), mcpTefRepo, f.mcpTefDir, f.mcpTefRef)
			if err != nil {
				return err
			}
			srv, err := teff.Start(cmd.Context(), f.mcpTefDir, f.port, filepath.Join(f.out, "mcp-tef.log"))
			if err != nil {
				return err
			}
			defer srv.Stop()
			fmt.Fprintf(cmd.OutOrStdout(), "mcp-tef healthy at %s\n", srv.URL)
			term := report.NewTerminal(cmd.OutOrStdout())
			results := runner.Run(cmd.Context(), teff.NewClient(srv.URL), loaded.suite, loaded.models.Models, runner.Options{
				Parallel:     f.parallel,
				Timeout:      f.timeout,
				JobTimeout:   f.jobTimeout,
				PollInterval: f.pollInterval,
				OnDone:       term.Progress,
			})
			summaries := report.Summarize(results)
			term.Results(results, summaries)

			flags := make(map[string]string)
			cmd.Flags().VisitAll(func(fl *pflag.Flag) {
				if fl.Name != "help" {
					flags[fl.Name] = fl.Value.String()
				}
			})
			meta := report.Metadata{Timestamp: started.UTC(), McpTefRef: f.mcpTefRef, McpTefCommit: commit, Flags: flags}
			jsonPath := filepath.Join(f.out, "results.json")
			if err := report.WriteJSON(jsonPath, meta, summaries, results); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", jsonPath)
			htmlPath := filepath.Join(f.out, "report.html")
			if err := report.WriteHTML(htmlPath, meta, summaries, results); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", htmlPath)
			// Failed jobs still count as a completed run; only Ctrl+C or
			// SIGTERM makes the run itself fail.
			if err := cmd.Context().Err(); err != nil {
				return fmt.Errorf("run interrupted: %w", err)
			}
			return nil
		},
	}
	f.register(cmd)
	fl := cmd.Flags()
	fl.IntVar(&f.parallel, "parallel", 4, "max concurrent (case * model) jobs across all models")
	fl.DurationVar(&f.timeout, "timeout", 30*time.Minute, "global deadline for the whole run")
	fl.DurationVar(&f.jobTimeout, "job-timeout", 10*time.Minute, "deadline for each job, excluding teardown")
	fl.DurationVar(&f.pollInterval, "poll-interval", 2*time.Second, "how often to poll mcp-tef for a test run's status")
	fl.StringVar(&f.out, "out", "./reports", "directory for results.json, report.html and mcp-tef.log")
	fl.StringVar(&f.mcpTefDir, "mcp-tef-dir", "./.mcp-tef", "where mcp-tef is cloned")
	fl.StringVar(&f.mcpTefRef, "mcp-tef-ref", "main", "mcp-tef git ref to check out")
	fl.IntVar(&f.port, "port", 8000, "port for the mcp-tef server")
	return cmd
}
