package tests

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Discover expands patterns into a sorted, de-duplicated list of test files.
//
// A pattern may be a file, a directory (every *.json beneath it), or a glob.
// Globs use path.Match syntax per segment, plus "**" for zero or more
// directories. Hidden directories (".git", ".mcp-tef", ...) below the starting
// point are skipped while walking. Every pattern must match at least one file.
func Discover(patterns []string) ([]string, error) {
	if len(patterns) == 0 {
		return nil, errors.New("no test file patterns given")
	}
	seen := make(map[string]bool)
	var files []string
	var errs []error
	for _, p := range patterns {
		matches, err := expand(p)
		if err != nil {
			errs = append(errs, fmt.Errorf("tests %q: %w", p, err))
			continue
		}
		if len(matches) == 0 {
			errs = append(errs, fmt.Errorf("tests %q: matched no files", p))
			continue
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				files = append(files, m)
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func expand(pattern string) ([]string, error) {
	pattern = filepath.ToSlash(pattern)
	if !hasMeta(pattern) {
		info, err := os.Stat(pattern)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return []string{filepath.Clean(pattern)}, nil
		}
		return walk(pattern, []string{"**", "*.json"})
	}

	segs := strings.Split(pattern, "/")
	for _, s := range segs {
		if _, err := path.Match(s, ""); err != nil {
			return nil, fmt.Errorf("bad glob: %w", err)
		}
	}
	i := 0
	for i < len(segs) && !hasMeta(segs[i]) {
		i++
	}
	base := strings.Join(segs[:i], "/")
	if base == "" {
		base = "."
		if strings.HasPrefix(pattern, "/") {
			base = "/"
		}
	}
	if _, err := os.Stat(base); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return walk(base, segs[i:])
}

// walk returns the regular files under base whose path relative to base
// matches the pattern segments.
func walk(base string, pat []string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != base && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(base, p)
		if err != nil {
			return err
		}
		if matchSegments(pat, strings.Split(filepath.ToSlash(rel), "/")) {
			out = append(out, filepath.Clean(p))
		}
		return nil
	})
	return out, err
}

func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], name[0]); !ok {
		return false
	}
	return matchSegments(pat[1:], name[1:])
}

func hasMeta(s string) bool {
	return strings.ContainsAny(s, `*?[\`)
}
