package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// LogFilter narrows a log search (GITX-9). Empty fields do not filter.
type LogFilter struct {
	Text   string // in the message, any case, plain text (not a pattern)
	Author string // in the author name or e-mail, any case
	Since  string // git date: 2026-01-31, 2.weeks, yesterday
	Until  string
	Path   string // a file or a folder
	Rev    string // a branch, tag or commit; "" = HEAD
	All    bool   // every branch, remote branch and tag
	Change string // commits that add or remove this text (pickaxe -S)
}

// String is the filter in the words of a picker title.
func (f LogFilter) String() string {
	var p []string
	if f.Text != "" {
		p = append(p, strconv.Quote(f.Text))
	}
	add := func(k, v string) {
		if v != "" {
			p = append(p, k+":"+v)
		}
	}
	add("author", f.Author)
	add("since", f.Since)
	add("until", f.Until)
	add("path", f.Path)
	add("branch", f.Rev)
	add("changes", f.Change)
	if f.All {
		p = append(p, "all branches")
	}
	return strings.Join(p, " ")
}

// ParseLogQuery reads the one-line query of Search Log…: plain words are
// message text; author:, since:, until:, path:, branch:, changes: set a
// filter; "all" searches every branch. Double quotes keep spaces in one
// word. Unknown key:value words stay message text.
func ParseLogQuery(q string) LogFilter {
	var f LogFilter
	var text []string
	for _, w := range splitQuoted(q) {
		k, v, ok := strings.Cut(w, ":")
		if !ok || v == "" {
			if w == "all" {
				f.All = true
				continue
			}
			text = append(text, w)
			continue
		}
		switch k {
		case "author":
			f.Author = v
		case "since", "after":
			f.Since = v
		case "until", "before":
			f.Until = v
		case "path":
			f.Path = v
		case "branch":
			f.Rev = v
		case "changes":
			f.Change = v
		default:
			text = append(text, w)
		}
	}
	f.Text = strings.Join(text, " ")
	return f
}

// splitQuoted splits on spaces; "a b" (also inside key:"a b") is one word.
func splitQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	in, has := false, false
	for _, r := range s {
		switch {
		case r == '"':
			in, has = !in, true
		case unicode.IsSpace(r) && !in:
			if has {
				out = append(out, cur.String())
				cur.Reset()
				has = false
			}
		default:
			cur.WriteRune(r)
			has = true
		}
	}
	if has {
		out = append(out, cur.String())
	}
	return out
}

// SearchLog lists up to limit commits that pass f, newest first. A branch
// that is not a commit (or looks like an option) is an error.
func SearchLog(dir string, f LogFilter, limit int) ([]LogEntry, error) {
	if limit <= 0 {
		limit = 200
	}
	args := []string{"log", "--pretty=format:%h%x1f%s%x1f%an%x1f%at%x1f%d%x1e", fmt.Sprintf("-n%d", limit),
		"--regexp-ignore-case", "--fixed-strings"}
	if f.Text != "" {
		args = append(args, "--grep="+f.Text)
	}
	if f.Author != "" {
		args = append(args, "--author="+f.Author)
	}
	if f.Since != "" {
		args = append(args, "--since="+f.Since)
	}
	if f.Until != "" {
		args = append(args, "--until="+f.Until)
	}
	if f.Change != "" {
		args = append(args, "-S"+f.Change)
	}
	switch {
	case f.All:
		args = append(args, "--date-order", "--branches", "--remotes", "--tags", "HEAD")
	case f.Rev != "":
		full, err := ResolveCommit(dir, f.Rev)
		if err != nil {
			return nil, err
		}
		args = append(args, full)
	}
	args = append(args, "--")
	if f.Path != "" {
		args = append(args, f.Path)
	}
	out, err := runCombined(dir, "git", args...)
	if err != nil {
		if !hasCommits(dir) {
			return nil, nil
		}
		return nil, err
	}
	return parseLogEntries(out), nil
}

func parseLogEntries(out string) []LogEntry {
	var entries []LogEntry
	for _, rec := range strings.Split(out, "\x1e") {
		rec = strings.TrimLeft(rec, "\n")
		fields := strings.Split(rec, "\x1f")
		if rec == "" || len(fields) < 5 {
			continue
		}
		entries = append(entries, LogEntry{Hash: fields[0], Subject: fields[1], Author: fields[2],
			When: relativeDate(fields[3]), Refs: strings.TrimSpace(fields[4])})
	}
	return entries
}

// ResolveCommit gives the full hash of a commit, branch or tag name. A
// name that starts with "-" is refused: it would be read as an option.
func ResolveCommit(dir, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("no commit given")
	}
	if strings.HasPrefix(ref, "-") {
		return "", fmt.Errorf("%q is not a commit", ref)
	}
	out, err := runOut(dir, "git", "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil || strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("no commit %s", ref)
	}
	return strings.TrimSpace(out), nil
}
