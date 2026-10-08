package search

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// MatchRange marks one highlighted span inside a Result.Preview, using
// 0-based byte offsets into the (raw) preview string. ripgrep emits one
// "submatches" entry per match within a line; we keep all of them so the
// renderer can bold every matched span on the row.
type MatchRange struct {
	Start int // byte offset, inclusive
	End   int // byte offset, exclusive
}

// Result is one match emitted by ripgrep's --json stream.
type Result struct {
	Path    string       // relative path
	Line    int          // 1-based line number
	Col     int          // 1-based column of the first match on the line
	Preview string       // raw line text (no trailing newline, untrimmed)
	Matches []MatchRange // byte ranges in Preview to highlight
}

// MaxResults caps the number of rows we collect; rg may emit more, we stop
// reading after this many matches.
const MaxResults = 100

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

// rgEvent is one envelope from `rg --json`. We only care about "match" events.
// See https://docs.rs/grep-printer/latest/grep_printer/struct.JSON.html for
// the full schema. The fields we don't read are deliberately omitted.
type rgEvent struct {
	Type string         `json:"type"`
	Data rgEventData    `json:"data"`
}

type rgEventData struct {
	Path       rgText        `json:"path"`
	Lines      rgText        `json:"lines"`
	LineNumber int           `json:"line_number"`
	Submatches []rgSubmatch  `json:"submatches"`
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

// Run executes a workspace search rooted at dir and returns up to MaxResults
// matches. Empty / whitespace-only queries return (nil, nil) without invoking
// ripgrep. If rg is not installed, ErrNoRipgrep is returned.
//
// Single-root call: result Path values stay rg-relative (i.e. relative to
// `dir`) so single-root call sites continue to behave exactly as before.
// For multi-root workspaces use RunDirs, which rewrites paths to absolute.
func Run(dir, query string) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if !Available() {
		// No ripgrep on this machine — use the built-in walker so
		// Find-in-Files still works (literal, smart-case, no .gitignore).
		return runFallback(dir, query)
	}
	return runOne(dir, query)
}

// runOne is the single-directory worker shared by Run / RunDirs. It does
// NOT rewrite paths — callers decide whether to keep them relative.
func runOne(dir, query string) ([]Result, error) {
	args := []string{
		"--json",
		"--smart-case",
		"--max-count=100",
		"--max-columns=200",
		"--",
		query,
	}
	cmd := exec.Command("rg", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, errors.New(firstLine(msg))
		}
		return nil, err
	}
	return parseJSON(stdout.Bytes())
}

// maxFallbackFileSize caps the size of a file the built-in walker will read.
// Anything larger is almost certainly a generated artifact or asset, not source
// a text search wants to surface.
const maxFallbackFileSize = 5 << 20 // 5 MiB

// runFallback is a dependency-free workspace search used when ripgrep is not
// installed, so Find-in-Files works out of the box. It walks `dir` — skipping
// VCS/dependency/hidden directories and binary or oversized files — and
// collects literal, smart-case substring matches in the same Result shape rg
// would produce. It is deliberately simpler than ripgrep: literal text only (no
// regex) and no .gitignore awareness. Installing rg restores the full engine.
func runFallback(dir, query string) ([]Result, error) {
	caseSensitive := query != strings.ToLower(query) // smart-case
	needle := query
	if !caseSensitive {
		needle = strings.ToLower(query)
	}
	nlen := len(needle)
	if nlen == 0 {
		return nil, nil
	}

	var results []Result
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry — skip it, keep walking
		}
		if d.IsDir() {
			if path != dir && skipSearchDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if len(results) >= MaxResults {
			return fs.SkipAll
		}
		if info, e := d.Info(); e == nil && info.Size() > maxFallbackFileSize {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil || isBinary(data) {
			return nil
		}
		rel, e := filepath.Rel(dir, path)
		if e != nil {
			rel = path
		}

		lineNo := 0
		for _, line := range strings.Split(string(data), "\n") {
			lineNo++
			line = strings.TrimRight(line, "\r")
			hay := line
			if !caseSensitive {
				hay = strings.ToLower(line)
			}
			first := strings.Index(hay, needle)
			if first < 0 {
				continue
			}
			// Every match on the line, so the renderer can bold each span.
			var matches []MatchRange
			for off := 0; ; {
				j := strings.Index(hay[off:], needle)
				if j < 0 {
					break
				}
				s := off + j
				matches = append(matches, MatchRange{Start: s, End: s + nlen})
				off = s + nlen
			}
			preview := line
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
			results = append(results, Result{
				Path:    rel,
				Line:    lineNo,
				Col:     first + 1,
				Preview: preview,
				Matches: matches,
			})
			if len(results) >= MaxResults {
				return fs.SkipAll
			}
		}
		return nil
	})
	return results, nil
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

// RunDirs runs ripgrep across every directory in `dirs` and returns up to
// MaxResults matches across the entire set. Each result's Path is rewritten
// to be absolute so the picker can open the match regardless of which root
// it came from. Empty / whitespace-only queries short-circuit before
// invoking rg.
func RunDirs(dirs []string, query string) ([]Result, error) {
	if strings.TrimSpace(query) == "" {
		return nil, nil
	}
	if len(dirs) == 0 {
		return nil, nil
	}
	useRg := Available()

	var all []Result
	var lastErr error
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		var results []Result
		var err error
		if useRg {
			results, err = runOne(dir, query)
		} else {
			results, err = runFallback(dir, query)
		}
		if err != nil {
			// One bad root shouldn't kill the whole search — remember it and
			// keep walking. The collected error is only surfaced if no root
			// produced any results.
			lastErr = err
			continue
		}
		// Rewrite each result's relative Path to absolute so the picker can
		// open the file regardless of which root it came from.
		for i := range results {
			if results[i].Path != "" {
				results[i].Path = absJoin(dir, results[i].Path)
			}
		}
		all = append(all, results...)
		if len(all) >= MaxResults {
			all = all[:MaxResults]
			return all, nil
		}
	}
	if len(all) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return all, nil
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

func parseJSON(data []byte) ([]Result, error) {
	results := make([]Result, 0, 32)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// rg can emit very long lines on minified files; allow up to 1 MiB per
	// event to be safe.
	scanner.Buffer(make([]byte, 64*1024), 1<<20)

	for scanner.Scan() {
		if len(results) >= MaxResults {
			break
		}
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
		if ev.Type != "match" {
			continue
		}

		path := ev.Data.Path.Text
		preview := strings.TrimRight(ev.Data.Lines.Text, "\r\n")

		col := 1
		matches := make([]MatchRange, 0, len(ev.Data.Submatches))
		for i, sm := range ev.Data.Submatches {
			if i == 0 {
				col = sm.Start + 1
			}
			matches = append(matches, MatchRange{Start: sm.Start, End: sm.End})
		}

		results = append(results, Result{
			Path:    path,
			Line:    ev.Data.LineNumber,
			Col:     col,
			Preview: preview,
			Matches: matches,
		})
	}
	if err := scanner.Err(); err != nil {
		return results, err
	}
	return results, nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
