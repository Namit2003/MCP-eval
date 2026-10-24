// Package cmd defines the tefrun command line.
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Namit2003/mcp-eval/internal/config"
	"github.com/Namit2003/mcp-eval/internal/tests"
)

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	// Ctrl+C and SIGTERM cancel the context instead of killing the process,
	// so deferred cleanup such as stopping mcp-tef still runs.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(root.ErrOrStderr(), "error:", indent(err.Error()))
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tefrun",
		Short:         "Run MCP tool-selection evals against a local mcp-tef server",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRunCmd(), newValidateCmd())
	return root
}

// inputFlags are shared by run and validate.
type inputFlags struct {
	tests  []string
	models string
}

func (f *inputFlags) register(cmd *cobra.Command) {
	cmd.Flags().StringArrayVar(&f.tests, "tests", nil, `test files, directories or globs ("**" supported); repeatable`)
	cmd.Flags().StringVar(&f.models, "models", "", "path to model.config.json")
	_ = cmd.MarkFlagRequired("tests")
	_ = cmd.MarkFlagRequired("models")
}

// inputs is everything loaded from the input flags.
type inputs struct {
	suite  *tests.Suite
	models *config.Config
}

func (i *inputs) numJobs() int {
	return i.suite.NumCases() * len(i.models.Models)
}

// load reads and validates the model config and test files, including that
// every required API key is present in the environment. All problems from
// both sources are reported together.
func (f *inputFlags) load() (*inputs, error) {
	var problems []string
	cfg, err := config.Load(f.models)
	if err != nil {
		problems = append(problems, err.Error())
	} else if err := cfg.CheckAPIKeys(os.LookupEnv); err != nil {
		problems = append(problems, err.Error())
	}
	suite, err := tests.Load(f.tests)
	if err != nil {
		problems = append(problems, err.Error())
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid input:\n%s", strings.Join(problems, "\n"))
	}
	return &inputs{suite: suite, models: cfg}, nil
}

// noArgs rejects positional arguments, which usually mean the shell expanded
// an unquoted --tests glob.
func noArgs(cmd *cobra.Command, args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected argument %q (quote --tests globs so the shell does not expand them)", args[0])
	}
	return nil
}

// indent puts continuation lines of a multi-line error under the first.
func indent(s string) string {
	return strings.ReplaceAll(s, "\n", "\n  ")
}
