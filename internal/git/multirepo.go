package git

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Several repositories in one workspace and submodules (GITX-31).

// findReposSkip are folders FindRepos never walks into: package and
// build output, which can be huge and hold no repo of the user's.
var findReposSkip = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "dist": true,
	"build": true, "out": true, ".venv": true, "venv": true, "__pycache__": true,
}

// findReposMax caps how many repositories FindRepos returns.
const findReposMax = 50

// FindRepos returns root (when it is a repository's top folder) and the
// repositories found below it, at most maxDepth folders down, sorted by
// path with root first. A folder counts when it has a .git folder or
// file (a submodule or worktree has a file). Hidden folders and package
// folders (node_modules, vendor …) are not walked.
func FindRepos(root string, maxDepth int) []string {
	root = filepath.Clean(root)
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == root {
				return err
			}
			return fs.SkipDir
		}
		if !d.IsDir() {
			return nil
		}
		if p != root {
			name := d.Name()
			if strings.HasPrefix(name, ".") || findReposSkip[name] {
				return fs.SkipDir
			}
		}
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
			out = append(out, p)
			if len(out) >= findReposMax {
				return fs.SkipAll
			}
		}
		if rel, _ := filepath.Rel(root, p); rel != "." && strings.Count(rel, string(filepath.Separator))+1 >= maxDepth {
			return fs.SkipDir
		}
		return nil
	})
	return out
}

// SubmoduleState is the first letter of a `git submodule status` line.
type SubmoduleState byte

const (
	SubmoduleOK       SubmoduleState = ' ' // checked out at the recorded commit
	SubmoduleNotInit  SubmoduleState = '-' // not initialized
	SubmoduleChanged  SubmoduleState = '+' // checked out at another commit
	SubmoduleConflict SubmoduleState = 'U' // merge conflicts
)

// Submodule is one submodule of a repository.
type Submodule struct {
	Path     string // relative to the repository top
	SHA      string // the commit checked out (or recorded, when not initialized)
	State    SubmoduleState
	Describe string // git describe of SHA ("v1.0", "heads/main"); may be empty
}

// parseSubmoduleStatus reads `git submodule status` output.
func parseSubmoduleStatus(out string) []Submodule {
	var subs []Submodule
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		st := SubmoduleState(line[0])
		rest := line[1:]
		sha, rest, ok := strings.Cut(rest, " ")
		if !ok {
			continue
		}
		s := Submodule{SHA: sha, State: st, Path: rest}
		if i := strings.LastIndex(rest, " ("); i >= 0 && strings.HasSuffix(rest, ")") {
			s.Path, s.Describe = rest[:i], rest[i+2:len(rest)-1]
		}
		subs = append(subs, s)
	}
	return subs
}

// Submodules lists the submodules of the repository at dir. No
// .gitmodules: none, and git does not run.
func Submodules(dir string) ([]Submodule, error) {
	if _, err := os.Stat(filepath.Join(dir, ".gitmodules")); err != nil {
		return nil, nil
	}
	out, err := runOut(dir, "git", "--no-optional-locks", "submodule", "status")
	if err != nil {
		return nil, err
	}
	return parseSubmoduleStatus(out), nil
}

type subCacheEntry struct {
	key  string
	mod  time.Time
	subs []Submodule
}

var subCache sync.Map // dir → *subCacheEntry

// SubmodulesCached is Submodules, reused while key (the caller's view
// of the repo state: HEAD and the changed files) and the .gitmodules
// file are the same. Errors give nil.
func SubmodulesCached(dir, key string) []Submodule {
	st, err := os.Stat(filepath.Join(dir, ".gitmodules"))
	if err != nil {
		return nil
	}
	if v, ok := subCache.Load(dir); ok {
		c := v.(*subCacheEntry)
		if c.key == key && c.mod.Equal(st.ModTime()) {
			return c.subs
		}
	}
	subs, err := Submodules(dir)
	if err != nil {
		return nil
	}
	if subs == nil {
		subs = []Submodule{}
	}
	subCache.Store(dir, &subCacheEntry{key: key, mod: st.ModTime(), subs: subs})
	return subs
}

// errBadSubmodulePath: a path that could be read as an option.
var errBadSubmodulePath = errors.New("not a submodule path")

// SubmoduleUpdate runs `git submodule update --init --recursive` for
// path, or for all submodules when path is "". It checks out the commit
// the repository records; git refuses (and changes nothing) when a
// submodule has local changes that would be lost.
func SubmoduleUpdate(dir, path string) error {
	args := []string{"submodule", "update", "--init", "--recursive"}
	if path != "" {
		if strings.HasPrefix(path, "-") {
			return errBadSubmodulePath
		}
		args = append(args, "--", path)
	}
	return runGit(dir, args...)
}
