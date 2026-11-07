package pairsync

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// ErrTooMany is returned when a side has more files than the cap.
var ErrTooMany = errors.New("too many files to sync; add folders to .gitignore")

// DefaultNames are folders and files that are never synced: git's own
// state, dependency folders that each side builds itself, termocode's
// folder and OS clutter.
var DefaultNames = []string{".git", "node_modules", ".termocode", "__pycache__", ".venv", ".DS_Store"}

// Ignore says which paths a sync skips: DefaultNames plus the simple
// lines of the root .gitignore. A "!" line is not supported and is
// skipped, so a sync can skip more than git does, never less.
type Ignore struct {
	names map[string]bool
	globs []string // matched against each path segment
	paths []string // anchored at the root (the line had a "/")
}

// NewIgnore builds the rules from the text of a .gitignore ("" = only
// DefaultNames).
func NewIgnore(gitignore string) *Ignore {
	ig := &Ignore{names: map[string]bool{}}
	for _, n := range DefaultNames {
		ig.names[n] = true
	}
	for _, line := range strings.Split(gitignore, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		line = strings.TrimSuffix(line, "/")
		anchored := strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		if _, err := path.Match(line, ""); err != nil {
			continue // a bad pattern
		}
		if anchored || strings.Contains(line, "/") {
			ig.paths = append(ig.paths, line)
		} else {
			ig.globs = append(ig.globs, line)
		}
	}
	return ig
}

// Match reports whether rel (a slash path) is skipped.
func (ig *Ignore) Match(rel string) bool {
	if ig == nil {
		return false
	}
	segs := strings.Split(rel, "/")
	for i, s := range segs {
		if ig.names[s] {
			return true
		}
		for _, g := range ig.globs {
			if ok, _ := path.Match(g, s); ok {
				return true
			}
		}
		prefix := strings.Join(segs[:i+1], "/")
		for _, p := range ig.paths {
			if ok, _ := path.Match(p, prefix); ok {
				return true
			}
		}
	}
	return false
}

// Walk lists the regular files under the root of fsys (symlinks are
// skipped). It reads FileInfo only, never file bodies.
func Walk(ctx context.Context, fsys workspace.FS, ig *Ignore, max int) (Snapshot, error) {
	snap := Snapshot{}
	var walk func(dir string) error
	walk = func(dir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		p := dir
		if p == "" {
			p = "."
		}
		ents, err := fsys.ReadDir(ctx, p)
		if err != nil {
			return err
		}
		for _, d := range ents {
			rel := d.Name()
			if dir != "" {
				rel = dir + "/" + d.Name()
			}
			if ig.Match(rel) {
				continue
			}
			switch {
			case d.Type()&fs.ModeSymlink != 0:
				continue
			case d.IsDir():
				if err := walk(rel); err != nil {
					return err
				}
				continue
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				continue
			}
			if len(snap) >= max {
				return ErrTooMany
			}
			snap[rel] = Entry{Size: info.Size(), MTime: info.ModTime().UnixNano(), Mode: uint32(info.Mode().Perm())}
		}
		return nil
	}
	if err := walk(""); err != nil {
		return nil, err
	}
	return snap, nil
}

// statScript lists the files under $1 as "size mtime mode ./path", one
// per line, in ONE engine call. Only POSIX find and stat -c (GNU and
// busybox) are needed. The default names are pruned so a big
// node_modules is never walked.
func statScript() string {
	var prune []string
	for _, n := range DefaultNames {
		prune = append(prune, "-name '"+n+"'")
	}
	return `cd "$1" && find . \( ` + strings.Join(prune, " -o ") + ` \) -prune -o -type f -exec stat -c '%s %Y %a %n' {} +`
}

// ExecList lists the files under root through x (the container's
// exec). A file name with a newline is skipped.
func ExecList(ctx context.Context, x services.Exec, root string, ig *Ignore, max int) (Snapshot, error) {
	stdout, stderr, code, err := x.Capture(ctx, root, "sh", "-c", statScript(), "sh", root)
	if err != nil {
		return nil, err
	}
	if code != 0 && strings.TrimSpace(stdout) == "" {
		msg := strings.TrimSpace(stderr)
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = fmt.Sprintf("exit %d", code)
		}
		return nil, fmt.Errorf("cannot list %s: %s", root, msg)
	}
	return ParseStatList(stdout, ig, max)
}

// ParseStatList reads the output of statScript. Lines it cannot read
// are skipped.
func ParseStatList(out string, ig *Ignore, max int) (Snapshot, error) {
	snap := Snapshot{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.SplitN(line, " ", 4)
		if len(f) != 4 || !strings.HasPrefix(f[3], "./") {
			continue
		}
		size, err1 := strconv.ParseInt(f[0], 10, 64)
		sec, err2 := strconv.ParseInt(f[1], 10, 64)
		mode, err3 := strconv.ParseUint(f[2], 8, 32)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		rel := path.Clean(strings.TrimPrefix(f[3], "./"))
		if rel == "." || strings.HasPrefix(rel, "../") || ig.Match(rel) {
			continue
		}
		if len(snap) >= max {
			return nil, ErrTooMany
		}
		snap[rel] = Entry{Size: size, MTime: sec * int64(time.Second), Mode: uint32(mode) & 0o777}
	}
	return snap, nil
}
