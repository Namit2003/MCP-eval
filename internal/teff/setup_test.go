package teff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, repo, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-m", content)
}

func TestCheckout(t *testing.T) {
	repo := t.TempDir()
	git(t, repo, "init", "-b", "main")
	commit(t, repo, "v1")
	git(t, repo, "tag", "v1")
	commit(t, repo, "v2")

	dir := filepath.Join(t.TempDir(), "mcp-tef")
	ctx := context.Background()

	// Missing dir: cloned at the requested tag.
	if err := checkout(ctx, repo, dir, "v1"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "show", "HEAD:file.txt"); got != "v1" {
		t.Errorf("after clone at v1: got %q", got)
	}

	// Existing dir: moved to the branch, including a commit made after the clone.
	commit(t, repo, "v3")
	if err := checkout(ctx, repo, dir, "main"); err != nil {
		t.Fatal(err)
	}
	if got := git(t, dir, "show", "HEAD:file.txt"); got != "v3" {
		t.Errorf("after checkout of main: got %q", got)
	}

	if err := checkout(ctx, repo, dir, "no-such-ref"); err == nil {
		t.Error("expected an error for an unknown ref")
	}
}
