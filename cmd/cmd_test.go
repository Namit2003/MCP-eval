package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func execute(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

func TestValidateExamples(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "test-key")
	out, err := execute(t, "validate", "--tests", "../examples/evals/**/*.json", "--models", "../examples/model.config.json")
	if err != nil {
		t.Fatalf("validate: %v\n%s", err, out)
	}
	if !strings.Contains(out, "OK: 2 files, 5 cases, 2 models, 10 total jobs") {
		t.Errorf("unexpected summary:\n%s", out)
	}
	if strings.Contains(out, "test-key") {
		t.Errorf("output leaked the API key:\n%s", out)
	}
}

func TestValidateMissingAPIKey(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	_, err := execute(t, "validate", "--tests", "../examples/evals", "--models", "../examples/model.config.json")
	if err == nil || !strings.Contains(err.Error(), "OPENROUTER_API_KEY is not set") {
		t.Fatalf("expected missing key error, got %v", err)
	}
}

func TestValidateReportsConfigAndTestErrorsTogether(t *testing.T) {
	_, err := execute(t, "validate", "--tests", "does-not-exist/*.json", "--models", "does-not-exist.json")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, w := range []string{"does-not-exist.json", `tests "does-not-exist/*.json": matched no files`} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not contain %q", err, w)
		}
	}
}

func TestRequiredFlags(t *testing.T) {
	_, err := execute(t, "validate")
	if err == nil || !strings.Contains(err.Error(), `required flag(s) "models", "tests" not set`) {
		t.Fatalf("got %v", err)
	}
}

func TestPositionalArgsRejected(t *testing.T) {
	_, err := execute(t, "validate", "--tests", "a.json", "b.json", "--models", "m.json")
	if err == nil || !strings.Contains(err.Error(), "quote --tests globs") {
		t.Fatalf("got %v", err)
	}
}

func TestRunFlagChecks(t *testing.T) {
	_, err := execute(t, "run", "--tests", "x", "--models", "y",
		"--parallel", "0", "--timeout", "0s", "--job-timeout", "0s", "--poll-interval", "-1s", "--port", "70000", "--mcp-tef-ref", "")
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, w := range []string{"--parallel must be >= 1", "--timeout must be > 0", "--job-timeout must be > 0", "--poll-interval must be > 0", "--port must be between", "--mcp-tef-ref must not be empty"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("error %q does not contain %q", err, w)
		}
	}
}
