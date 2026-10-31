package search

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Files lists the files under dir whose path matches the globs (an entry
// with a leading "!" drops what it matches; no include glob means every
// file). Paths are slash-separated, relative to dir and sorted; at most
// max are returned (max <= 0: all), total counts every match. ripgrep
// (`rg --files`, which honours .gitignore) is used when installed;
// otherwise the Go walker skips the same hidden, VCS and dependency
// folders as the built-in search.
func Files(ctx context.Context, dir string, globs []string, max int) (files []string, total int, err error) {
	var include, exclude []string
	for _, g := range globs {
		if g = strings.TrimSpace(g); g == "" {
			continue
		}
		if e, ok := strings.CutPrefix(g, "!"); ok {
			exclude = append(exclude, e)
		} else {
			include = append(include, g)
		}
	}
	if Available() {
		files, err = filesRg(ctx, dir, include, exclude)
	} else {
		files, err = filesWalk(ctx, dir, globFilter{include: include, exclude: exclude})
	}
	if err != nil {
		return nil, 0, err
	}
	sort.Strings(files)
	total = len(files)
	if max > 0 && len(files) > max {
		files = files[:max]
	}
	return files, total, nil
}

func filesRg(ctx context.Context, dir string, include, exclude []string) ([]string, error) {
	args := []string{"--files"}
	for _, g := range include {
		args = append(args, "--glob="+g)
	}
	for _, g := range exclude {
		args = append(args, "--glob=!"+g)
	}
	cmd := spawn.CommandContext(ctx, "rg", args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 1 { // 1 = no files
			if msg := strings.TrimSpace(stderr.String()); msg != "" {
				return nil, errors.New(firstLine(msg))
			}
			return nil, err
		}
	}
	var files []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if l := strings.TrimPrefix(strings.TrimSpace(sc.Text()), "./"); l != "" {
			files = append(files, filepath.ToSlash(l))
		}
	}
	return files, nil
}

func filesWalk(ctx context.Context, dir string, globs globFilter) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil // unreadable entry: skip it, keep walking
		}
		if path == dir {
			return nil
		}
		rel, e := filepath.Rel(dir, path)
		if e != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if skipSearchDir(d.Name()) || globs.excluded(rel, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || !d.Type().IsRegular() && d.Type()&fs.ModeSymlink == 0 {
			return nil // hidden files, as rg --files
		}
		if globs.allowFile(rel, d.Name()) {
			files = append(files, rel)
		}
		return nil
	})
	return files, err
}

// MatchGlob reports whether the slash-separated relative path rel
// matches the glob (the same gitignore-style rules as the search
// filters: no "/" matches a name anywhere, "dir/**" everything below).
func MatchGlob(glob, rel string) bool {
	rel = filepath.ToSlash(rel)
	return globMatch(glob, rel, filepath.Base(rel))
}
