package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func newValidateCmd() *cobra.Command {
	var in inputFlags
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Check the model config and test files without running anything",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			loaded, err := in.load()
			if err != nil {
				return err
			}
			printSummary(cmd.OutOrStdout(), loaded)
			return nil
		},
	}
	in.register(cmd)
	return cmd
}

func printSummary(w io.Writer, in *inputs) {
	fmt.Fprintln(w, "Test files:")
	for _, f := range in.suite.Files {
		fmt.Fprintf(w, "  %s (%d %s)\n", f.Path, len(f.Cases), plural(len(f.Cases), "case", "cases"))
	}
	fmt.Fprintln(w, "Models:")
	for _, m := range in.models.Models {
		fmt.Fprintf(w, "  %s\n", m.Label())
	}
	fmt.Fprintf(w, "\nOK: %d %s, %d %s, %d %s, %d total %s\n",
		len(in.suite.Files), plural(len(in.suite.Files), "file", "files"),
		in.suite.NumCases(), plural(in.suite.NumCases(), "case", "cases"),
		len(in.models.Models), plural(len(in.models.Models), "model", "models"),
		in.numJobs(), plural(in.numJobs(), "job", "jobs"))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
