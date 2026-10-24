package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

type runFlags struct {
	inputFlags
	parallel     int
	timeout      time.Duration
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
			// Starting mcp-tef and executing jobs arrive in later phases.
			return errors.New("run: executing evals is not implemented yet")
		},
	}
	f.register(cmd)
	fl := cmd.Flags()
	fl.IntVar(&f.parallel, "parallel", 4, "max concurrent (case * model) jobs across all models")
	fl.DurationVar(&f.timeout, "timeout", 30*time.Minute, "global deadline for the whole run")
	fl.DurationVar(&f.pollInterval, "poll-interval", 2*time.Second, "how often to poll mcp-tef for a test run's status")
	fl.StringVar(&f.out, "out", "./reports", "directory for results.json, report.html and mcp-tef.log")
	fl.StringVar(&f.mcpTefDir, "mcp-tef-dir", "./.mcp-tef", "where mcp-tef is cloned")
	fl.StringVar(&f.mcpTefRef, "mcp-tef-ref", "main", "mcp-tef git ref to check out")
	fl.IntVar(&f.port, "port", 8000, "port for the mcp-tef server")
	return cmd
}
