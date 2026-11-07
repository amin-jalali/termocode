package agentlog

import (
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Summary is one row of the session history.
type Summary struct {
	CLI         CLI
	Path        string
	ID          string
	Cwd         string
	Branch      string
	Start       time.Time
	ModTime     time.Time
	FirstPrompt string // one line, redacted
	Prompts     int
	Tokens      int // in + out
	// Err is why the file cannot be read ("" when it can).
	Err string
}

type indexEntry struct {
	mod  time.Time
	size int64
	sum  Summary
	keep bool // the session belongs to the project it was built for
}

// Index is the cached session history: a file is read again only when
// its modification time or size changed. Safe for concurrent use.
type Index struct {
	mu    sync.Mutex
	root  string // the project the cache is for
	cache map[string]indexEntry
}

// NewIndex is an empty index.
func NewIndex() *Index { return &Index{cache: map[string]indexEntry{}} }

// Build lists the sessions of the project at root from both CLIs,
// newest first. Only folders are listed for a file that did not change.
// An empty root lists the sessions of every project (CMP-30).
func (x *Index) Build(h Homes, root string) []Summary {
	all := root == ""
	slug := ""
	if !all {
		root = filepath.Clean(root)
		slug = Slug(root)
	}
	var refs []Ref
	projects := filepath.Join(h.Claude, "projects")
	if ents, err := readDir(projects); err == nil {
		for _, e := range ents {
			if n := e.Name(); e.IsDir() && (all || n == slug || strings.HasPrefix(n, slug+"-")) {
				refs = append(refs, claudeDirFiles(filepath.Join(projects, n))...)
			}
		}
	}
	refs = append(refs, h.codexFiles()...)
	sortNewest(refs)
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.root != root { // another project: whose file is whose changes
		x.root, x.cache = root, map[string]indexEntry{}
	}
	seen := make(map[string]bool, len(refs))
	var out []Summary
	for _, r := range refs {
		seen[r.Path] = true
		e, ok := x.cache[r.Path]
		if !ok || !e.mod.Equal(r.ModTime) || e.size != r.Size {
			e = indexEntry{mod: r.ModTime, size: r.Size}
			if r.CLI == Codex && !all { // another project's file: its first line says so
				if c := codexCwd(r.Path); c != "" && !InProject(root, c) {
					x.cache[r.Path] = e
					continue
				}
			}
			e.sum = summarize(r)
			e.keep = all || e.sum.Cwd == "" && r.CLI == Claude && filepath.Base(filepath.Dir(r.Path)) == slug ||
				e.sum.Cwd != "" && InProject(root, e.sum.Cwd)
			x.cache[r.Path] = e
		}
		if e.keep {
			out = append(out, e.sum)
		}
	}
	for p := range x.cache {
		if !seen[p] {
			delete(x.cache, p)
		}
	}
	return out
}

// summarize reads one file into its history row.
func summarize(r Ref) Summary {
	s, err := ReadFile(r.CLI, r.Path)
	sum := Summary{CLI: r.CLI, Path: r.Path, ID: s.ID, Cwd: s.Cwd, Branch: s.Branch, Start: s.Start, ModTime: r.ModTime,
		FirstPrompt: oneLine(s.FirstPrompt(), 100), Prompts: s.Prompts(), Tokens: s.Usage.In + s.Usage.Out}
	if err != nil {
		sum.Err = err.Error()
		if !strings.HasPrefix(sum.Err, "cannot read this session") {
			sum.Err = "cannot read this session: " + sum.Err
		}
	}
	if sum.Start.IsZero() {
		sum.Start = r.ModTime
	}
	return sum
}
