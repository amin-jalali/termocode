package search

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// MatchRange marks one highlighted span inside a Result.Preview, using
// 0-based byte offsets into the (raw) preview string. ripgrep emits one
// "submatches" entry per match within a line; we keep all of them so the
// renderer can bold every matched span on the row.
type MatchRange struct {
	Start int // byte offset, inclusive
	End   int // byte offset, exclusive
}

// ContextLine is one non-matching line shown around a match when
// Options.Context > 0.
type ContextLine struct {
	Line int    // 1-based line number
	Text string // raw line text (no trailing newline)
}

// Result is one match emitted by ripgrep's --json stream.
type Result struct {
	Path    string       // relative path
	Line    int          // 1-based line number
	Col     int          // 1-based column of the first match on the line
	Preview string       // raw line text (no trailing newline, untrimmed)
	Matches []MatchRange // byte ranges in Preview to highlight
	// Before / After hold the context lines around this match (only when
	// Options.Context > 0). A context line that sits between two close
	// matches can appear in both the earlier match's After and the later
	// match's Before — the same thing ripgrep's own grouping does.
	Before []ContextLine
	After  []ContextLine
}

// DefaultMaxResults is the total result cap used when the caller has no
// `search_max_results` setting. 0 in Options.MaxResults means unlimited.
const DefaultMaxResults = 2000

// maxPerFile mirrors rg's --max-count: at most this many matching lines are
// taken from any one file, so a single huge log can't crowd out the rest.
const maxPerFile = 100

// MaxColumns mirrors --max-columns: rg replaces lines longer than this with
// a placeholder, which keeps very long minified files from blowing up the UI.
const MaxColumns = 200

// ErrNoRipgrep is returned when rg is not installed.
var ErrNoRipgrep = errors.New("ripgrep (rg) not found in PATH")

// Available reports whether ripgrep is installed.
func Available() bool {
	_, err := exec.LookPath("rg")
	return err == nil
}

// Options are the per-search knobs exposed by the live overlay's toggles
// and filter fields. The zero value is a smart-case literal search with no
// filters, no context and no result cap.
type Options struct {
	CaseSensitive bool     // Aa — off means smart-case
	WholeWord     bool     // ab — match whole words only
	Regex         bool     // .* — off means the query is a literal string
	Include       []string // glob filters a file must match (any of)
	Exclude       []string // glob filters that drop a file or directory
	Context       int      // lines of context before/after each match
	MaxResults    int      // total cap on collected results; 0 = unlimited
}

// Summary describes a finished search. Total keeps counting after the
// MaxResults cap is hit, so the UI can say "Showing N of M".
type Summary struct {
	Shown int // results handed to the caller
	Total int // results found (≥ Shown)
	Files int // files with at least one shown result
}

// Truncated reports whether the cap dropped any results.
func (s Summary) Truncated() bool { return s.Total > s.Shown }

// SplitGlobs turns a user-typed filter field ("*.go, !vendor, src/**") into
// a clean glob list. Commas separate entries; blanks are dropped.
func SplitGlobs(s string) []string {
	var out []string
	for _, g := range strings.Split(s, ",") {
		if g = strings.TrimSpace(g); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// rgEvent is one envelope from `rg --json`. We read "begin", "match",
// "context" and "end". See
// https://docs.rs/grep-printer/latest/grep_printer/struct.JSON.html for the
// full schema. The fields we don't read are deliberately omitted.
type rgEvent struct {
	Type string      `json:"type"`
	Data rgEventData `json:"data"`
}

type rgEventData struct {
	Path       rgText       `json:"path"`
	Lines      rgText       `json:"lines"`
	LineNumber int          `json:"line_number"`
	Submatches []rgSubmatch `json:"submatches"`
}

type rgSubmatch struct {
	Match rgText `json:"match"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// rgText is rg's `{ "text": "..." }` string-or-base64 wrapper. We only ever
// see UTF-8 text in our codebase, so we ignore the base64 variant.
type rgText struct {
	Text string `json:"text"`
}

// Run executes a workspace search rooted at dir and returns up to
// DefaultMaxResults matches. Empty / whitespace-only queries return
// (nil, nil) without invoking ripgrep. If rg is not installed the
// built-in walker is used instead.
//
// Single-root call: result Path values stay rg-relative (i.e. relative to
// `dir`) so single-root call sites continue to behave exactly as before.
// For multi-root workspaces use RunDirs, which rewrites paths to absolute.
func Run(dir, query string) ([]Result, error) {
	var all []Result
	_, err := search(context.Background(), []string{dir}, query, legacyOptions(), false,
		func(rs []Result) { all = append(all, rs...) })
	if len(all) == 0 {
		return nil, err
	}
	return all, nil
}

// RunDirs runs a search across every directory in `dirs` and returns up to
// DefaultMaxResults matches across the entire set. Each result's Path is
// rewritten to be absolute so the picker can open the match regardless of
// which root it came from.
func RunDirs(dirs []string, query string) ([]Result, error) {
	rs, _, err := Collect(context.Background(), dirs, query, legacyOptions())
	return rs, err
}

// legacyOptions keeps the pre-toggle semantics of Run / RunDirs: smart-case,
// regex through ripgrep, literal on the fallback walker.
func legacyOptions() Options {
	return Options{Regex: Available(), MaxResults: DefaultMaxResults}
}

// Collect runs Search and gathers every shown result into one slice.
func Collect(ctx context.Context, dirs []string, query string, opts Options) ([]Result, Summary, error) {
	var all []Result
	sum, err := Search(ctx, dirs, query, opts, func(rs []Result) { all = append(all, rs...) })
	return all, sum, err
}

// Search runs the query across every root in `dirs` and calls onFile once
// per file with that file's results (paths absolute, ordered by line). Once
// opts.MaxResults results have been handed over, later matches are only
// counted (Summary.Total), not delivered.
//
// ripgrep is used when installed; otherwise the pure-Go walker runs. One
// bad root doesn't kill the search — its error is returned only when no
// root produced a match. Empty / whitespace-only queries return at once.
func Search(ctx context.Context, dirs []string, query string, opts Options, onFile func([]Result)) (Summary, error) {
	return search(ctx, dirs, query, opts, true, onFile)
}

func search(ctx context.Context, dirs []string, query string, opts Options, abs bool, onFile func([]Result)) (Summary, error) {
	var sum Summary
	if strings.TrimSpace(query) == "" || len(dirs) == 0 {
		return sum, nil
	}
	useRg := Available()
	var lastErr error
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return sum, err
		}
		emit := func(rs []Result) {
			sum.Total += len(rs)
			if opts.MaxResults > 0 {
				room := opts.MaxResults - sum.Shown
				if room <= 0 {
					return
				}
				if len(rs) > room {
					rs = rs[:room]
				}
			}
			if abs {
				// Rewrite each result's relative Path to absolute so the
				// caller can open the file whichever root it came from.
				for i := range rs {
					if rs[i].Path != "" {
						rs[i].Path = absJoin(dir, rs[i].Path)
					}
				}
			}
			sum.Shown += len(rs)
			sum.Files++
			if onFile != nil {
				onFile(rs)
			}
		}
		var err error
		if useRg {
			err = runRg(ctx, dir, query, opts, emit)
		} else {
			err = runFallback(ctx, dir, query, opts, emit)
		}
		if err != nil {
			if ctx.Err() != nil {
				return sum, ctx.Err()
			}
			// One bad root shouldn't kill the whole search — remember it
			// and keep walking. It is surfaced only if nothing matched.
			lastErr = err
		}
	}
	if sum.Total == 0 && lastErr != nil {
		return sum, lastErr
	}
	return sum, nil
}

// Batch is one chunk of a streamed search. The final Batch has Done set
// and carries the Summary / error; it holds no results.
type Batch struct {
	Results []Result
	Done    bool
	Summary Summary
	Err     error
}

// Streaming batch thresholds: flush once this many results are pending, or
// when this much time has passed since the last flush (checked per file).
const (
	streamBatchSize     = 200
	streamBatchInterval = 100 * time.Millisecond
)

// Stream runs Search in the background and delivers results in batches of
// whole files, so a caller can paint the first hits while the rest of the
// tree is still being searched. The channel is closed after the Done batch.
// Cancelling ctx stops the search; pending sends are dropped.
func Stream(ctx context.Context, dirs []string, query string, opts Options) <-chan Batch {
	ch := make(chan Batch, 4)
	go func() {
		defer close(ch)
		send := func(b Batch) bool {
			select {
			case ch <- b:
				return true
			case <-ctx.Done():
				return false
			}
		}
		var pending []Result
		last := time.Now()
		flush := func() {
			if len(pending) == 0 {
				return
			}
			send(Batch{Results: pending})
			pending = nil
			last = time.Now()
		}
		sum, err := Search(ctx, dirs, query, opts, func(rs []Result) {
			pending = append(pending, rs...)
			if len(pending) >= streamBatchSize || time.Since(last) >= streamBatchInterval {
				flush()
			}
		})
		flush()
		send(Batch{Done: true, Summary: sum, Err: err})
	}()
	return ch
}

// rgArgs builds the ripgrep command line for one search.
func rgArgs(query string, opts Options) []string {
	args := []string{
		"--json",
		"--max-count=" + strconv.Itoa(maxPerFile),
		"--max-columns=" + strconv.Itoa(MaxColumns),
	}
	if opts.CaseSensitive {
		args = append(args, "--case-sensitive")
	} else {
		args = append(args, "--smart-case")
	}
	if !opts.Regex {
		args = append(args, "--fixed-strings")
	}
	if opts.WholeWord {
		args = append(args, "--word-regexp")
	}
	if opts.Context > 0 {
		args = append(args, "--context="+strconv.Itoa(opts.Context))
	}
	for _, g := range opts.Include {
		args = append(args, "--glob="+g)
	}
	for _, g := range opts.Exclude {
		args = append(args, "--glob=!"+strings.TrimPrefix(g, "!"))
	}
	return append(args, "--", query)
}

// runRg is the single-directory ripgrep worker. It streams rg's stdout
// through parseStream so results reach onFile while rg is still running.
// Paths stay relative to dir — search() decides whether to absolutise.
func runRg(ctx context.Context, dir, query string, opts Options, onFile func([]Result)) error {
	cmd := exec.CommandContext(ctx, "rg", rgArgs(query, opts)...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	perr := parseStream(stdout, opts.Context, func(_ string, rs []Result) { onFile(rs) })
	// Drain whatever the parser left (e.g. after an over-long line) so rg
	// never blocks on a full pipe and Wait can return.
	_, _ = io.Copy(io.Discard, stdout)
	err = cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil // exit 1 = no matches
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(firstLine(msg))
		}
		return err
	}
	return perr
}

// parseStream reads rg's --json event stream and calls onFile once per file
// with its match results. Context events are attached to the neighbouring
// matches' Before / After lists (within ctxLines of the match).
func parseStream(r io.Reader, ctxLines int, onFile func(path string, rs []Result)) error {
	scanner := bufio.NewScanner(r)
	// rg can emit very long lines on minified files; allow up to 1 MiB per
	// event to be safe.
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	var cur string
	var rs []Result
	var pend []ContextLine // context lines seen since the last match
	flush := func() {
		if n := len(rs); n > 0 {
			last := &rs[n-1]
			for _, c := range pend {
				if c.Line > last.Line && c.Line <= last.Line+ctxLines {
					last.After = append(last.After, c)
				}
			}
			onFile(cur, rs)
		}
		cur, rs, pend = "", nil, nil
	}
	enter := func(path string) {
		if path != cur {
			flush()
			cur = path
		}
	}

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 || raw[0] != '{' {
			continue
		}
		var ev rgEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			// Skip malformed events rather than aborting — rg may interleave
			// summary types we don't care about.
			continue
		}
		path := ev.Data.Path.Text
		switch ev.Type {
		case "begin":
			enter(path)
		case "end":
			flush()
		case "context":
			enter(path)
			pend = append(pend, ContextLine{
				Line: ev.Data.LineNumber,
				Text: strings.TrimRight(ev.Data.Lines.Text, "\r\n"),
			})
		case "match":
			enter(path)
			col := 1
			matches := make([]MatchRange, 0, len(ev.Data.Submatches))
			for i, sm := range ev.Data.Submatches {
				if i == 0 {
					col = sm.Start + 1
				}
				matches = append(matches, MatchRange{Start: sm.Start, End: sm.End})
			}
			res := Result{
				Path:    path,
				Line:    ev.Data.LineNumber,
				Col:     col,
				Preview: strings.TrimRight(ev.Data.Lines.Text, "\r\n"),
				Matches: matches,
			}
			for _, c := range pend {
				if n := len(rs); n > 0 && c.Line <= rs[n-1].Line+ctxLines {
					rs[n-1].After = append(rs[n-1].After, c)
				}
				if c.Line >= res.Line-ctxLines {
					res.Before = append(res.Before, c)
				}
			}
			pend = nil
			rs = append(rs, res)
		}
	}
	flush()
	return scanner.Err()
}

// parseJSON collects every match in a complete rg --json stream. Kept as a
// small helper for tests and one-shot callers.
func parseJSON(data []byte) ([]Result, error) {
	results := make([]Result, 0, 32)
	err := parseStream(bytes.NewReader(data), 0, func(_ string, rs []Result) {
		results = append(results, rs...)
	})
	return results, err
}

// maxFallbackFileSize caps the size of a file the built-in walker will read.
// Anything larger is almost certainly a generated artifact or asset, not source
// a text search wants to surface.
const maxFallbackFileSize = 5 << 20 // 5 MiB

// lineMatcher finds every match of the query on one line. The fallback
// compiles every query to a Go regexp: literal queries are quoted, so the
// same code path handles case folding and whole-word checks for both.
type lineMatcher struct {
	re        *regexp.Regexp
	wholeWord bool
}

func newLineMatcher(query string, opts Options) (*lineMatcher, error) {
	pat := query
	if !opts.Regex {
		pat = regexp.QuoteMeta(query)
	}
	// Smart-case: fold case unless the toggle forces it on or the query
	// itself has an upper-case letter.
	if !opts.CaseSensitive && !hasUpper(query) {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, err
	}
	return &lineMatcher{re: re, wholeWord: opts.WholeWord}, nil
}

func (lm *lineMatcher) find(line string) []MatchRange {
	var out []MatchRange
	for _, loc := range lm.re.FindAllStringIndex(line, -1) {
		if loc[0] == loc[1] {
			continue // empty regex match — nothing to show
		}
		if lm.wholeWord && !atWordBoundary(line, loc[0], loc[1]) {
			continue
		}
		out = append(out, MatchRange{Start: loc[0], End: loc[1]})
	}
	return out
}

// atWordBoundary reports whether line[s:e] is not glued to a word character
// on either side (rg's --word-regexp, approximately).
func atWordBoundary(line string, s, e int) bool {
	if s > 0 {
		r, _ := utf8.DecodeLastRuneInString(line[:s])
		if isWordRune(r) {
			return false
		}
	}
	if e < len(line) {
		r, _ := utf8.DecodeRuneInString(line[e:])
		if isWordRune(r) {
			return false
		}
	}
	return true
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// runFallback is a dependency-free workspace search used when ripgrep is not
// installed, so Find-in-Files works out of the box. It walks `dir` — skipping
// VCS/dependency/hidden directories and binary or oversized files — and
// collects matches in the same Result shape rg would produce. Case, whole
// word, regex (Go RE2 syntax), context lines and include/exclude globs are
// all honoured; globs use filepath.Match plus a simple `**` rule, so they
// are a subset of rg's. There is no .gitignore awareness — installing rg
// restores the full engine.
func runFallback(ctx context.Context, dir, query string, opts Options, onFile func([]Result)) error {
	if query == "" {
		return nil
	}
	lm, err := newLineMatcher(query, opts)
	if err != nil {
		return err
	}
	globs := globFilter{include: opts.Include, exclude: opts.Exclude}

	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil // unreadable entry — skip it, keep walking
		}
		rel, e := filepath.Rel(dir, path)
		if e != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if path != dir && (skipSearchDir(d.Name()) || globs.excluded(rel, d.Name())) {
				return fs.SkipDir
			}
			return nil
		}
		if !globs.allowFile(rel, d.Name()) {
			return nil
		}
		if info, e := d.Info(); e == nil && info.Size() > maxFallbackFileSize {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil || isBinary(data) {
			return nil
		}
		if rs := searchLines(string(data), filepath.FromSlash(rel), lm, opts.Context); len(rs) > 0 {
			onFile(rs)
		}
		return nil
	})
}

// searchLines runs the matcher over one file's text and builds its results,
// including up to ctxLines of surrounding non-matching lines.
func searchLines(text, rel string, lm *lineMatcher, ctxLines int) []Result {
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1] // trailing newline is not an extra line
	}
	hits := map[int][]MatchRange{}
	var order []int
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		lines[i] = line
		if len(order) >= maxPerFile {
			continue
		}
		if ms := lm.find(line); len(ms) > 0 {
			hits[i] = ms
			order = append(order, i)
		}
	}
	if len(order) == 0 {
		return nil
	}
	contextAt := func(i int) ContextLine {
		return ContextLine{Line: i + 1, Text: clip(lines[i])}
	}
	results := make([]Result, 0, len(order))
	for _, i := range order {
		matches := hits[i]
		col := matches[0].Start + 1
		preview := lines[i]
		if len(preview) > MaxColumns {
			preview = preview[:MaxColumns]
			kept := matches[:0]
			for _, mr := range matches {
				if mr.End <= MaxColumns {
					kept = append(kept, mr)
				}
			}
			matches = kept
		}
		res := Result{
			Path:    rel,
			Line:    i + 1,
			Col:     col,
			Preview: preview,
			Matches: matches,
		}
		for j := max(0, i-ctxLines); j < i; j++ {
			if _, isHit := hits[j]; !isHit {
				res.Before = append(res.Before, contextAt(j))
			}
		}
		for j := i + 1; j <= i+ctxLines && j < len(lines); j++ {
			if _, isHit := hits[j]; !isHit {
				res.After = append(res.After, contextAt(j))
			}
		}
		results = append(results, res)
	}
	return results
}

func clip(s string) string {
	if len(s) > MaxColumns {
		return s[:MaxColumns]
	}
	return s
}

// globFilter applies include / exclude globs for the fallback walker.
type globFilter struct {
	include, exclude []string
}

// excluded reports whether any exclude glob matches the entry.
func (g globFilter) excluded(rel, name string) bool {
	for _, p := range g.exclude {
		if globMatch(strings.TrimPrefix(p, "!"), rel, name) {
			return true
		}
	}
	return false
}

// allowFile reports whether a file passes both filters: not excluded and,
// when include globs are set, matching at least one of them.
func (g globFilter) allowFile(rel, name string) bool {
	if g.excluded(rel, name) {
		return false
	}
	if len(g.include) == 0 {
		return true
	}
	for _, p := range g.include {
		if globMatch(p, rel, name) {
			return true
		}
	}
	return false
}

// globMatch is a small gitignore-style matcher on top of filepath.Match:
// a pattern without "/" matches the base name anywhere in the tree, a
// leading "**/" is dropped, and a trailing "/**" (or "/") matches everything
// under that directory. Anything else is matched against the
// slash-separated relative path.
func globMatch(pat, rel, name string) bool {
	pat = strings.TrimPrefix(strings.TrimSpace(pat), "./")
	pat = strings.TrimPrefix(pat, "**/")
	if pat == "" {
		return false
	}
	if p, ok := strings.CutSuffix(pat, "/**"); ok {
		pat = p
	} else if p, ok := strings.CutSuffix(pat, "/"); ok {
		pat = p
	}
	if !strings.Contains(pat, "/") {
		if ok, _ := filepath.Match(pat, name); ok {
			return true
		}
		// "vendor" also covers files below a vendor/ directory.
		for _, part := range strings.Split(rel, "/") {
			if ok, _ := filepath.Match(pat, part); ok {
				return true
			}
		}
		return false
	}
	if ok, _ := filepath.Match(pat, rel); ok {
		return true
	}
	// "src/app" also covers everything below it.
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		if ok, _ := filepath.Match(pat, strings.Join(parts[:i], "/")); ok {
			return true
		}
	}
	return false
}

// skipSearchDir reports whether the built-in walker should prune a directory.
// Mirrors ripgrep's defaults loosely: VCS metadata, common dependency/build
// dirs, and dotfolders are skipped.
func skipSearchDir(name string) bool {
	switch name {
	case ".git", "node_modules", "vendor", "target", "dist", "build",
		".next", ".cache", "__pycache__", ".venv", "venv":
		return true
	}
	return strings.HasPrefix(name, ".")
}

// isBinary reports whether data looks non-textual (contains a NUL byte in the
// first 8 KiB), so the walker skips it instead of dumping garbage into results.
func isBinary(data []byte) bool {
	n := len(data)
	if n > 8192 {
		n = 8192
	}
	return bytes.IndexByte(data[:n], 0) >= 0
}

// absJoin returns dir/rel as an absolute, slash-clean path. Falls back to
// the original input on error so callers always get something usable.
func absJoin(dir, rel string) string {
	if rel == "" {
		return rel
	}
	// rg already emits absolute paths if Cmd.Dir was absolute and the file
	// happens to lie outside; defensively detect that.
	if filepath.IsAbs(rel) {
		return rel
	}
	joined := filepath.Join(dir, rel)
	if abs, err := filepath.Abs(joined); err == nil {
		return abs
	}
	return joined
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
