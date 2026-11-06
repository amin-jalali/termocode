package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// Files source limits.
const (
	// maxWalk caps the entries one files source looks at.
	maxWalk = 200_000
	// maxContent is the most file text one item holds (the item cap
	// minus room for the other fields).
	maxContent = MaxItemBytes - 2<<10
	// maxDoubleStar caps "**" in a glob (each one multiplies the work).
	maxDoubleStar = 3
)

// FilesInput is a `"from": "files"` source. Each matching regular file
// is one item, keyed by its path:
//
//	{"path":"docs/a.md","size":120,"mod_time":"2026-11-05T10:00:00Z",
//	 "content":"…","truncated":false,"binary":false}
type FilesInput struct {
	// Glob is relative to the workspace, "/" separated; "**" matches any
	// number of folders ("docs/**/*.md"). No "..", not absolute.
	Glob string
	// ChangedSince keeps only files modified after it (zero: all).
	ChangedSince time.Time
	// Content adds the file text (up to about 62 KB, then truncated;
	// binary files get "binary": true and no content).
	Content bool
	// MaxItems caps the files (see Shape.MaxItems).
	MaxItems int
}

type fileItem struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	ModTime   string `json:"mod_time"`
	Content   string `json:"content,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Binary    bool   `json:"binary,omitempty"`
}

var errStopWalk = errors.New("stop")

// Files lists the files that match the glob. It never leaves the
// workspace: folders behind symlinks are not entered, and a symlinked
// file is used only when its target is inside the workspace. .git is
// skipped.
func (s *Sources) Files(ctx context.Context, in FilesInput) (Result, error) {
	pat, err := cleanGlob(in.Glob)
	if err != nil {
		return Result{}, fmt.Errorf("files: %w", err)
	}
	col, err := newCollector(Shape{MaxItems: in.MaxItems})
	if err != nil {
		return Result{}, fmt.Errorf("files: %w", err)
	}
	root, err := s.root()
	if err != nil {
		return Result{}, fmt.Errorf("files: %w", err)
	}
	seen := 0
	var inner error
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return nil // an unreadable folder is skipped
		}
		if seen++; seen > maxWalk {
			return fmt.Errorf("more than %d entries in the workspace; use a narrower glob", maxWalk)
		}
		if seen%1024 == 0 && ctx.Err() != nil {
			return ctx.Err()
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if d.Name() == ".git" && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		if !matchGlob(pat, rel) {
			return nil
		}
		real := p
		if d.Type()&fs.ModeSymlink != 0 {
			t, err := filepath.EvalSymlinks(p)
			if err != nil || !within(root, t) {
				return nil // dangling, or pointing outside: never followed
			}
			real = t
		}
		fi, err := os.Stat(real)
		if err != nil || !fi.Mode().IsRegular() {
			return nil
		}
		if !in.ChangedSince.IsZero() && !fi.ModTime().After(in.ChangedSince) {
			return nil
		}
		it := fileItem{Path: rel, Size: fi.Size(), ModTime: fi.ModTime().UTC().Format(time.RFC3339)}
		if in.Content {
			it.Content, it.Truncated, it.Binary = readText(real)
		}
		b, _ := json.Marshal(it)
		if err := col.add(b, rel); err != nil {
			inner = err
			return errStopWalk
		}
		return nil
	})
	if inner != nil {
		return Result{}, fmt.Errorf("files: %w", inner)
	}
	if err != nil {
		return Result{}, fmt.Errorf("files: %w", err)
	}
	return col.res, nil
}

// readText reads up to maxContent bytes of text.
func readText(p string) (text string, truncated, binary bool) {
	f, err := os.Open(p)
	if err != nil {
		return "", false, false
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, maxContent+1))
	if len(b) > maxContent {
		b, truncated = b[:maxContent], true
		for len(b) > 0 && !utf8.Valid(b) && len(b) > maxContent-utf8.UTFMax {
			b = b[:len(b)-1]
		}
	}
	if bytes.IndexByte(b, 0) >= 0 || !utf8.Valid(b) {
		return "", false, true
	}
	return string(b), truncated, false
}

// cleanGlob checks a glob and splits it into parts.
func cleanGlob(g string) ([]string, error) {
	g = strings.TrimSpace(g)
	if g == "" {
		return nil, errors.New("glob is empty")
	}
	if strings.HasPrefix(g, "/") || filepath.IsAbs(g) || strings.Contains(g, "\\") {
		return nil, fmt.Errorf("%w: glob %q must be relative, with / between folders", ErrOutside, g)
	}
	parts := strings.Split(g, "/")
	if strings.Count(g, "**") > maxDoubleStar {
		return nil, fmt.Errorf("glob %q has more than %d **", g, maxDoubleStar)
	}
	for _, p := range parts {
		if p == ".." {
			return nil, fmt.Errorf("%w: glob %q has ..", ErrOutside, g)
		}
		if p != "**" {
			if _, err := path.Match(p, ""); err != nil {
				return nil, fmt.Errorf("glob %q: %w", g, err)
			}
		}
	}
	out := parts[:0]
	for _, p := range parts {
		if p != "" && p != "." {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("glob %q matches no files", g)
	}
	return out, nil
}

// matchGlob matches a "/" path against glob parts; "**" is any number
// of folders (zero included).
func matchGlob(pat []string, name string) bool {
	return matchParts(pat, strings.Split(name, "/"))
}

func matchParts(pat, parts []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(parts); i++ {
				if matchParts(pat[1:], parts[i:]) {
					return true
				}
			}
			return false
		}
		if len(parts) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], parts[0]); !ok {
			return false
		}
		pat, parts = pat[1:], parts[1:]
	}
	return len(parts) == 0
}
