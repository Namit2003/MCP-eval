package teff

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"strings"
)

// Setup makes dir a checkout of repoURL at ref, then installs mcp-tef's
// dependencies with uv. An existing dir is reused and moved to ref. It
// returns the commit hash that was checked out.
func Setup(ctx context.Context, repoURL, dir, ref string) (string, error) {
	if err := checkout(ctx, repoURL, dir, ref); err != nil {
		return "", fmt.Errorf("mcp-tef setup: %w", err)
	}
	// --frozen installs exactly what uv.lock says and never rewrites it, so
	// the checkout stays clean for the next run's checkout.
	if err := run(ctx, dir, "uv", "sync", "--frozen"); err != nil {
		return "", fmt.Errorf("mcp-tef setup: %w", err)
	}
	commit, err := headCommit(ctx, dir)
	if err != nil {
		return "", fmt.Errorf("mcp-tef setup: %w", err)
	}
	return commit, nil
}

// headCommit returns the commit hash checked out in dir.
func headCommit(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// checkout clones repoURL into dir if needed, then fetches ref and checks it
// out. Fetching ref and checking out FETCH_HEAD works the same for branches,
// tags and commit hashes, and always picks up a branch's latest commit.
func checkout(ctx context.Context, repoURL, dir, ref string) error {
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if err := run(ctx, "", "git", "clone", repoURL, dir); err != nil {
			return err
		}
	}
	if err := run(ctx, dir, "git", "fetch", "origin", ref); err != nil {
		return err
	}
	return run(ctx, dir, "git", "checkout", "--detach", "FETCH_HEAD")
}

// run executes a command in dir, showing its output on stderr.
func run(ctx context.Context, dir, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}
