package tasks

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// ── file:line links (ported from mobocode terminal_link_parser.dart) ────

// Link is one `file:line(:col)` match inside a line of text.
type Link struct {
	Path string // as printed (may be relative)
	Line int    // 1-based
	Col  int    // 1-based, 0 when absent
	// Start / End are byte offsets of the match in the text (half-open).
	Start, End int
}

// codeExtensions are the file extensions worth linking. Conservative on
// purpose so timestamps, IPs and versions never match.
var codeExtensions = map[string]bool{
	".dart": true, ".go": true, ".rs": true, ".py": true, ".ts": true,
	".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".c": true, ".h": true, ".cpp": true, ".cc": true, ".hpp": true,
	".lua": true, ".json": true, ".yaml": true, ".yml": true, ".sh": true,
	".bash": true, ".toml": true, ".md": true, ".kt": true, ".java": true,
	".rb": true, ".swift": true, ".cs": true, ".ex": true, ".exs": true,
	".php": true, ".html": true, ".css": true, ".scss": true, ".vue": true,
	".svelte": true, ".zig": true, ".mod": true, ".sql": true, ".proto": true,
}

var linkRe = regexp.MustCompile(`((?:[^\s:]+/)?[^\s:/]+\.[a-zA-Z]{1,10}):(\d+)(?::(\d+))?`)

// linkTrimLeft are wrapper characters stripped from the front of a path
// ("(src/a.go:3)" links "src/a.go:3").
const linkTrimLeft = "\"'`([{<"

// ParseLinks returns every `file:line(:col)` link in text, in order. Unlike
// the Dart original a bare file name ("main_test.go:12", as `go test`
// prints) is accepted too — the extension allow-list keeps it strict.
func ParseLinks(text string) []Link {
	var out []Link
	for _, m := range linkRe.FindAllStringSubmatchIndex(text, -1) {
		start := m[2]
		path := text[m[2]:m[3]]
		for len(path) > 0 && strings.ContainsRune(linkTrimLeft, rune(path[0])) {
			path = path[1:]
			start++
		}
		if path == "" || !codeExtensions[strings.ToLower(filepath.Ext(path))] {
			continue
		}
		line, err := strconv.Atoi(text[m[4]:m[5]])
		if err != nil || line <= 0 {
			continue
		}
		col := 0
		if m[6] >= 0 {
			col, _ = strconv.Atoi(text[m[6]:m[7]])
		}
		out = append(out, Link{Path: path, Line: line, Col: col, Start: start, End: m[1]})
	}
	return out
}

// LinkAt returns the link covering byte offset off in text.
func LinkAt(text string, off int) (Link, bool) {
	for _, l := range ParseLinks(text) {
		if off >= l.Start && off < l.End {
			return l, true
		}
	}
	return Link{}, false
}

// ResolvePath makes path absolute against root (a leading "./" is
// dropped). Absolute paths are returned cleaned.
func ResolvePath(path, root string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(root, strings.TrimPrefix(path, "./"))
}

// ── Problem matchers ────────────────────────────────────────────────────

// Severity values match vim.diagnostic.severity (1 = error … 4 = hint).
const (
	SevError   = 1
	SevWarning = 2
	SevInfo    = 3
	SevHint    = 4
)

// Problem is one diagnostic found in task output.
type Problem struct {
	Path     string // as printed (resolve with ResolvePath)
	Line     int    // 1-based
	Col      int    // 1-based (1 when absent)
	Severity int
	Message  string
}

// Known matcher names. Every name turns on the same built-in recognisers
// (they do not clash: each looks for a distinct shape), so the names
// mostly document intent. "$none" or an empty list disables matching.
var knownMatchers = map[string]bool{
	"$generic": true, "$go": true, "$gcc": true, "$tsc": true,
	"$rustc": true, "$pytest": true, "$eslint-compact": true,
}

// KnownMatcher reports whether name is a built-in matcher.
func KnownMatcher(name string) bool { return knownMatchers[name] }

// Enabled reports whether a matcher list turns matching on.
func Enabled(names []string) bool {
	for _, n := range names {
		if n != "" && n != "$none" {
			return true
		}
	}
	return false
}

var (
	ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)
	// path:line(:col)?: message — gcc, go, pytest, eslint-compact, …
	genericRe = regexp.MustCompile(`^\s*((?:[A-Za-z]:)?[^\s:()"']+\.[A-Za-z0-9]{1,10}):(\d+)(?::(\d+))?:?\s*(.*)$`)
	// path(line,col): error TS1234: message — tsc
	tscRe = regexp.MustCompile(`^\s*([^\s()]+\.[A-Za-z0-9]{1,10})\((\d+),(\d+)\):\s*(error|warning|info)\s+(.*)$`)
	// rustc: "error[E0308]: msg" then "  --> src/main.rs:4:5"
	rustHeadRe  = regexp.MustCompile(`^(error|warning)(?:\[\w+\])?:\s*(.+)$`)
	rustArrowRe = regexp.MustCompile(`^\s*-->\s*([^\s:]+):(\d+):(\d+)`)
	// "error: msg", "warning[W1]: msg", "note: msg" at the start of a message
	sevPrefixRe = regexp.MustCompile(`(?i)^(fatal error|error|warning|warn|note|info|hint)(?:\[[^\]]*\])?\s*:\s*(.*)$`)
	hasLetterRe = regexp.MustCompile(`[A-Za-z]`)
)

// StripANSI removes terminal escape sequences.
func StripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// Match scans task output lines and returns the problems found, in output
// order, without duplicates. Returns nil when matchers is disabled.
func Match(matchers []string, lines []string) []Problem {
	if !Enabled(matchers) {
		return nil
	}
	var out []Problem
	seen := map[Problem]bool{}
	add := func(p Problem) {
		if p.Col <= 0 {
			p.Col = 1
		}
		if p.Message == "" {
			p.Message = "problem"
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	var rustSev int
	var rustMsg string
	for _, raw := range lines {
		line := strings.TrimRight(StripANSI(raw), " \r")
		if rustMsg != "" {
			if m := rustArrowRe.FindStringSubmatch(line); m != nil {
				add(Problem{Path: m[1], Line: atoi(m[2]), Col: atoi(m[3]), Severity: rustSev, Message: rustMsg})
				rustMsg = ""
				continue
			}
		}
		if m := rustHeadRe.FindStringSubmatch(line); m != nil {
			rustSev, rustMsg = sevFromWord(m[1]), m[2]
			continue
		}
		if m := tscRe.FindStringSubmatch(line); m != nil {
			add(Problem{Path: m[1], Line: atoi(m[2]), Col: atoi(m[3]), Severity: sevFromWord(m[4]), Message: m[5]})
			continue
		}
		if m := genericRe.FindStringSubmatch(line); m != nil {
			ext := filepath.Ext(m[1])
			if !hasLetterRe.MatchString(ext) {
				continue
			}
			ln := atoi(m[2])
			if ln <= 0 {
				continue
			}
			sev, msg := SevError, strings.TrimSpace(m[4])
			if sm := sevPrefixRe.FindStringSubmatch(msg); sm != nil {
				sev, msg = sevFromWord(sm[1]), strings.TrimSpace(sm[2])
			}
			add(Problem{Path: m[1], Line: ln, Col: atoi(m[3]), Severity: sev, Message: msg})
		}
	}
	return out
}

func sevFromWord(w string) int {
	switch strings.ToLower(w) {
	case "warning", "warn":
		return SevWarning
	case "note", "info":
		return SevInfo
	case "hint":
		return SevHint
	}
	return SevError
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
