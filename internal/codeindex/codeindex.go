// Package codeindex is termocode's local codebase index "by meaning"
// (CMP-69). It cuts the workspace's files into chunks of lines, turns
// each chunk into a sparse vector of weighted words (identifiers split
// into words, TF-IDF weights, the file path counted twice) and ranks the
// chunks by cosine similarity to a query. The query is widened with
// related words ("remove" also looks for "delete", "drop" …), so a
// question in plain words finds code that never uses those words.
//
// Everything runs here, in Go, on this computer: no model, no network,
// no file leaves the machine. Secret files (the Skip rule) and binary
// files are never read into the index. The index is saved to a private
// cache file (0600) and an Update reads only the files that changed.
package codeindex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/search"
)

// CacheDirEnv overrides the cache folder (tests, portable installs).
const CacheDirEnv = "TERMOCODE_CODEINDEX_DIR"

// Defaults of Options.
const (
	DefaultMaxFiles     = 8000
	DefaultMaxFileBytes = 256 << 10
	DefaultChunkLines   = 40
	DefaultStep         = 30
	maxLineBytes        = 1000 // longer lines (minified code) are cut
	formatVersion       = 1
)

// Options tune an Index. Zero values take the defaults.
type Options struct {
	// List returns the files to index, slash paths relative to root.
	// Default: the git-tracked files, or a walk of the folder (hidden,
	// VCS and dependency folders skipped) when it is not a git work tree.
	List func(root string) ([]string, error)
	// Skip drops a file before it is read (secret files).
	Skip         func(rel string) bool
	MaxFiles     int
	MaxFileBytes int64
	ChunkLines   int
	Step         int
}

// Hit is one ranked chunk.
type Hit struct {
	Path    string // slash path relative to the root
	Line    int    // 1-based line of the best match in the chunk
	EndLine int    // last line of the chunk
	Score   float64
	Preview string
}

// Stats describe the index after an Update.
type Stats struct {
	Files     int
	Chunks    int
	Reindexed int  // files read in this Update
	Capped    bool // more files than MaxFiles: the rest were left out
	Took      time.Duration
	Built     time.Time
}

type chunk struct {
	Start, End int
	Terms      map[string]float32 // raw term counts (path words included)
	Preview    string
}

type fileEntry struct {
	Size   int64
	Mod    int64
	Chunks []chunk
}

type posting struct {
	chunk int32
	w     float32
}

type chunkRef struct {
	path string
	c    *chunk
}

// Index is the index of one folder. Its methods are safe for use by
// several goroutines.
type Index struct {
	mu    sync.Mutex
	root  string
	opt   Options
	files map[string]*fileEntry
	// unread: binary or empty files seen last Update (size, mod), so an
	// unchanged one is not read again.
	unread map[string][2]int64
	built  time.Time
	capped bool
	// derived by finish
	refs     []chunkRef
	postings map[string][]posting
	idf      map[string]float64
	norms    []float64
}

// New returns an empty index of root.
func New(root string, o Options) *Index {
	if o.MaxFiles <= 0 {
		o.MaxFiles = DefaultMaxFiles
	}
	if o.MaxFileBytes <= 0 {
		o.MaxFileBytes = DefaultMaxFileBytes
	}
	if o.ChunkLines <= 0 {
		o.ChunkLines = DefaultChunkLines
	}
	if o.Step <= 0 || o.Step > o.ChunkLines {
		o.Step = min(DefaultStep, o.ChunkLines)
	}
	if o.List == nil {
		o.List = defaultList
	}
	return &Index{root: filepath.Clean(root), opt: o, files: map[string]*fileEntry{}}
}

// Root is the indexed folder.
func (x *Index) Root() string { return x.root }

// defaultList is the git-tracked files, else a walk of the folder.
func defaultList(root string) ([]string, error) {
	if fs, err := git.TrackedFiles(root); err == nil && len(fs) > 0 {
		return fs, nil
	}
	fs, _, err := search.Files(context.Background(), root, nil, 0)
	return fs, err
}

// Update brings the index up to date with the files on disk: new and
// changed files are read, deleted ones dropped.
func (x *Index) Update(ctx context.Context) (Stats, error) {
	start := time.Now()
	if err := ctx.Err(); err != nil {
		return Stats{}, err
	}
	list, err := x.opt.List(x.root)
	if err != nil {
		return Stats{}, fmt.Errorf("list files: %w", err)
	}
	sort.Strings(list)
	x.mu.Lock()
	defer x.mu.Unlock()
	next := make(map[string]*fileEntry, len(list))
	nextUnread := map[string][2]int64{}
	reindexed, capped := 0, false
	for _, rel := range list {
		if err := ctx.Err(); err != nil {
			return Stats{}, err
		}
		rel = filepath.ToSlash(rel)
		if rel == "" || x.opt.Skip != nil && x.opt.Skip(rel) {
			continue
		}
		if len(next) >= x.opt.MaxFiles {
			capped = true
			break
		}
		abs := filepath.Join(x.root, filepath.FromSlash(rel))
		fi, err := os.Stat(abs)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > x.opt.MaxFileBytes {
			continue
		}
		if old, ok := x.files[rel]; ok && old.Size == fi.Size() && old.Mod == fi.ModTime().UnixNano() {
			next[rel] = old
			continue
		}
		stamp := [2]int64{fi.Size(), fi.ModTime().UnixNano()}
		if old, ok := x.unread[rel]; ok && old == stamp {
			nextUnread[rel] = stamp
			continue
		}
		e, ok := x.readFile(rel, abs, fi)
		reindexed++
		if ok {
			next[rel] = e
		} else {
			nextUnread[rel] = stamp
		}
	}
	x.files, x.unread, x.capped = next, nextUnread, capped
	x.built = time.Now()
	x.finish()
	st := x.statsLocked()
	st.Reindexed, st.Took = reindexed, time.Since(start)
	return st, nil
}

// readFile chunks one file; ok is false for a binary or empty file.
func (x *Index) readFile(rel, abs string, fi os.FileInfo) (*fileEntry, bool) {
	b, err := os.ReadFile(abs)
	if err != nil || len(b) == 0 || bytes.IndexByte(b[:min(len(b), 8000)], 0) >= 0 {
		return nil, false
	}
	var lines []string
	sc := bufio.NewScanner(bytes.NewReader(b))
	sc.Buffer(make([]byte, 64<<10), int(x.opt.MaxFileBytes)+1)
	for sc.Scan() {
		l := sc.Text()
		if len(l) > maxLineBytes {
			l = l[:maxLineBytes]
		}
		lines = append(lines, l)
	}
	pathTerms := Tokens(rel)
	e := &fileEntry{Size: fi.Size(), Mod: fi.ModTime().UnixNano()}
	for s := 0; s < len(lines); s += x.opt.Step {
		end := min(s+x.opt.ChunkLines, len(lines))
		c := chunk{Start: s + 1, End: end, Terms: map[string]float32{}}
		for _, l := range lines[s:end] {
			for _, t := range Tokens(l) {
				c.Terms[t]++
			}
		}
		if len(c.Terms) > 0 {
			for _, t := range pathTerms {
				c.Terms[t] += 2
			}
			c.Preview = preview(lines[s:end])
			e.Chunks = append(e.Chunks, c)
		}
		if end == len(lines) {
			break
		}
	}
	return e, len(e.Chunks) > 0
}

// preview is the chunk's first declaration-like line, else its first
// non-blank line.
func preview(lines []string) string {
	first := ""
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if first == "" {
			first = t
		}
		for _, p := range []string{"func ", "type ", "class ", "def ", "fn ", "pub fn ", "export ", "interface ", "struct "} {
			if strings.HasPrefix(t, p) {
				return cut(t, 120)
			}
		}
	}
	return cut(first, 120)
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// finish rebuilds the postings, IDF and norms. x.mu is held.
func (x *Index) finish() {
	paths := make([]string, 0, len(x.files))
	for p := range x.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	x.refs = x.refs[:0]
	df := map[string]int{}
	for _, p := range paths {
		e := x.files[p]
		for i := range e.Chunks {
			x.refs = append(x.refs, chunkRef{path: p, c: &e.Chunks[i]})
			for t := range e.Chunks[i].Terms {
				df[t]++
			}
		}
	}
	n := float64(len(x.refs))
	x.idf = make(map[string]float64, len(df))
	for t, d := range df {
		x.idf[t] = math.Log(1 + n/float64(d))
	}
	x.postings = make(map[string][]posting, len(df))
	x.norms = make([]float64, len(x.refs))
	for i, r := range x.refs {
		var sum float64
		for t, tf := range r.c.Terms {
			w := (1 + math.Log(float64(tf))) * x.idf[t]
			x.postings[t] = append(x.postings[t], posting{chunk: int32(i), w: float32(w)})
			sum += w * w
		}
		x.norms[i] = math.Sqrt(sum)
	}
}

// Stats describe the index as it is.
func (x *Index) Stats() Stats {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.statsLocked()
}

func (x *Index) statsLocked() Stats {
	return Stats{Files: len(x.files), Chunks: len(x.refs), Capped: x.capped, Built: x.built}
}

// Search ranks the chunks for q and returns the best k (at most two per
// file), best first.
func (x *Index) Search(q string, k int) []Hit {
	if k <= 0 {
		k = 10
	}
	qv := queryVector(q)
	if len(qv) == 0 {
		return nil
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.refs) == 0 {
		return nil
	}
	scores := map[int32]float64{}
	var qnorm float64
	for t, qw := range qv {
		idf, ok := x.idf[t]
		if !ok {
			continue
		}
		w := qw * idf
		qnorm += w * w
		for _, p := range x.postings[t] {
			scores[p.chunk] += w * float64(p.w)
		}
	}
	if qnorm == 0 {
		return nil
	}
	qnorm = math.Sqrt(qnorm)
	type scored struct {
		i int32
		s float64
	}
	all := make([]scored, 0, len(scores))
	for i, s := range scores {
		if n := x.norms[i]; n > 0 {
			all = append(all, scored{i, s / (n * qnorm)})
		}
	}
	sort.Slice(all, func(a, b int) bool {
		if all[a].s != all[b].s {
			return all[a].s > all[b].s
		}
		return all[a].i < all[b].i
	})
	perFile := map[string]int{}
	var hits []Hit
	for _, sc := range all {
		r := x.refs[sc.i]
		if perFile[r.path] >= 2 {
			continue
		}
		perFile[r.path]++
		h := Hit{Path: r.path, Line: r.c.Start, EndLine: r.c.End, Score: sc.s, Preview: r.c.Preview}
		x.refine(&h, qv)
		hits = append(hits, h)
		if len(hits) == k {
			break
		}
	}
	return hits
}

// refine points h at the chunk line with the most query words.
func (x *Index) refine(h *Hit, qv map[string]float64) {
	f, err := os.Open(filepath.Join(x.root, filepath.FromSlash(h.Path)))
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), int(x.opt.MaxFileBytes)+1)
	best, bestLine, text := 0.0, 0, ""
	for n := 1; sc.Scan() && n <= h.EndLine; n++ {
		if n < h.Line {
			continue
		}
		var s float64
		for _, t := range Tokens(sc.Text()) {
			s += qv[t] * x.idf[t]
		}
		if s > best {
			best, bestLine, text = s, n, sc.Text()
		}
	}
	if bestLine > 0 {
		h.Line, h.Preview = bestLine, cut(strings.TrimSpace(text), 120)
	}
}

// ── persistence ─────────────────────────────────────────────────────────

type saved struct {
	Version int
	Root    string
	Built   time.Time
	Capped  bool
	Files   map[string]*fileEntry
}

// CachePath is the cache file of root: one file per folder under
// $TERMOCODE_CODEINDEX_DIR, else <user cache>/termocode/codeindex.
func CachePath(root string) string {
	dir := os.Getenv(CacheDirEnv)
	if dir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			base = os.TempDir()
		}
		dir = filepath.Join(base, "termocode", "codeindex")
	}
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return filepath.Join(dir, hex.EncodeToString(sum[:8])+".gob")
}

// Save writes the index to path (0600, written to a temp file first).
func (x *Index) Save(path string) error {
	x.mu.Lock()
	s := saved{Version: formatVersion, Root: x.root, Built: x.built, Capped: x.capped, Files: x.files}
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(s)
	x.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".codeindex-*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(buf.Bytes())
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), 0o600)
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), path)
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
	}
	return werr
}

// Load reads a cache written by Save for the same folder.
func (x *Index) Load(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s saved
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&s); err != nil {
		return fmt.Errorf("read index cache: %w", err)
	}
	if s.Version != formatVersion {
		return errors.New("index cache: old format")
	}
	if filepath.Clean(s.Root) != x.root {
		return fmt.Errorf("index cache is for %s, not %s", s.Root, x.root)
	}
	if s.Files == nil {
		s.Files = map[string]*fileEntry{}
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	x.files, x.built, x.capped = s.Files, s.Built, s.Capped
	x.finish()
	return nil
}

// ── one index per folder in this process ────────────────────────────────

var (
	sharedMu sync.Mutex
	shared   = map[string]*Index{}
)

// Shared returns this process's index of root, loading its cache file
// the first time. skip is the secret-file rule of the first caller.
func Shared(root string, skip func(rel string) bool) *Index {
	root = filepath.Clean(root)
	sharedMu.Lock()
	defer sharedMu.Unlock()
	if x, ok := shared[root]; ok {
		return x
	}
	x := New(root, Options{Skip: skip})
	_ = x.Load(CachePath(root)) // no cache yet, or an old one: the Update builds it
	shared[root] = x
	return x
}

// Refresh updates the shared index of root and saves it when files were
// read again.
func Refresh(ctx context.Context, root string, skip func(rel string) bool) (*Index, Stats, error) {
	x := Shared(root, skip)
	st, err := x.Update(ctx)
	if err != nil {
		return x, st, err
	}
	if st.Reindexed > 0 {
		_ = x.Save(CachePath(root)) // a cache that cannot be written only costs time next run
	}
	return x, st, nil
}
