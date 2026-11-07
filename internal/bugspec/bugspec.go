// Package bugspec reads and writes bugfix specs (CMP-79): one Markdown
// file per bug that records the current behavior, the expected behavior
// and the behavior that must not change, so a fix is made test first and
// does not break other things.
//
// A spec lives at .termocode/specs/<slug>/bugfix.md in the project.
package bugspec

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// Dir is the specs folder, relative to the project root.
	Dir = ".termocode/specs"
	// FileName is the name of a bugfix spec in its folder.
	FileName = "bugfix.md"
	slugMax  = 48
)

// Spec is a parsed bugfix spec. Each list holds one item per line of its
// section (list marks and comments dropped).
type Spec struct {
	Title     string
	Current   []string
	Expected  []string
	Unchanged []string
	Steps     []string
}

// Slug is title as a folder name: lower case words of a-z and 0-9 joined
// by "-", at most 48 bytes; "bug" when nothing is left.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		default:
			dash = true
		}
	}
	s := b.String()
	if len(s) > slugMax {
		s = strings.TrimRight(s[:slugMax], "-")
	}
	if s == "" {
		return "bug"
	}
	return s
}

// PathFor is the spec file of title under the project root.
func PathFor(root, title string) string {
	return filepath.Join(root, filepath.FromSlash(Dir), Slug(title), FileName)
}

// The section headings of a new spec.
const (
	headCurrent   = "Current behavior"
	headExpected  = "Expected behavior"
	headUnchanged = "Unchanged behavior"
	headSteps     = "Steps to reproduce"
)

// Template is a new spec for title: the three sections and the steps,
// each with a comment that says what goes in it.
func Template(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	return "# Bugfix: " + title + "\n\n" +
		"## " + headCurrent + "\n<!-- What happens now: the input, the wrong output or the error. One line each. -->\n\n" +
		"## " + headExpected + "\n<!-- What should happen instead. Each line becomes a test that fails before the fix. -->\n\n" +
		"## " + headUnchanged + "\n<!-- What must keep working exactly as today. Each line gets a guard test that passes before and after the fix. -->\n\n" +
		"## " + headSteps + "\n<!-- Optional: how to see the bug, step by step. -->\n"
}

// section is which list a "## " heading starts (nil: not one of ours).
func (s *Spec) section(head string) *[]string {
	h := strings.ToLower(head)
	switch {
	case strings.HasPrefix(h, "current"):
		return &s.Current
	case strings.HasPrefix(h, "expected"):
		return &s.Expected
	case strings.HasPrefix(h, "unchanged"):
		return &s.Unchanged
	case strings.HasPrefix(h, "steps"), strings.HasPrefix(h, "reproduce"):
		return &s.Steps
	}
	return nil
}

// Parse reads a spec. The title is the first "# " heading (without a
// leading "Bugfix:"); the sections are found by their "## " heading.
func Parse(md string) Spec {
	var s Spec
	var cur *[]string
	for _, line := range strings.Split(stripComments(md), "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "## "):
			cur = s.section(strings.TrimSpace(t[3:]))
			continue
		case strings.HasPrefix(t, "# "):
			if s.Title == "" {
				title := strings.TrimSpace(t[2:])
				if rest, ok := cutFold(title, "bugfix:"); ok {
					title = strings.TrimSpace(rest)
				}
				s.Title = title
			}
			cur = nil
			continue
		}
		if cur == nil {
			continue
		}
		if item := listItem(t); item != "" {
			*cur = append(*cur, item)
		}
	}
	return s
}

// stripComments drops <!-- … --> (also over several lines).
func stripComments(md string) string {
	var b strings.Builder
	for {
		i := strings.Index(md, "<!--")
		if i < 0 {
			b.WriteString(md)
			return b.String()
		}
		b.WriteString(md[:i])
		j := strings.Index(md[i:], "-->")
		if j < 0 {
			return b.String()
		}
		// keep the line count: a comment over lines leaves its newlines
		b.WriteString(strings.Repeat("\n", strings.Count(md[i:i+j], "\n")))
		md = md[i+j+3:]
	}
}

func cutFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}

// listItem is a line without its list mark ("- ", "* ", "1. ", "[ ] ").
func listItem(t string) string {
	t = strings.TrimSpace(strings.TrimLeft(t, "-*+•"))
	if i := strings.IndexAny(t, ".)"); i > 0 && i <= 3 && strings.Trim(t[:i], "0123456789") == "" {
		t = strings.TrimSpace(t[i+1:])
	}
	for _, box := range []string{"[ ]", "[x]", "[X]"} {
		if rest, ok := strings.CutPrefix(t, box); ok {
			t = strings.TrimSpace(rest)
		}
	}
	return t
}

// Problems are the reasons the spec is not ready for a fix (none: it is).
func (s Spec) Problems() []string {
	var out []string
	if len(s.Current) == 0 {
		out = append(out, headCurrent+" is empty: say what happens now")
	}
	if len(s.Expected) == 0 {
		out = append(out, headExpected+" is empty: say what should happen instead")
	}
	if len(s.Unchanged) == 0 {
		out = append(out, headUnchanged+" is empty: list what must keep working, so the fix does not break it")
	}
	return out
}

// FirstEmptyLine is the 1-based line of the heading of the first empty
// section of the three (0: none is empty or the heading is missing).
func FirstEmptyLine(md string, s Spec) int {
	var want string
	switch {
	case len(s.Current) == 0:
		want = "current"
	case len(s.Expected) == 0:
		want = "expected"
	case len(s.Unchanged) == 0:
		want = "unchanged"
	default:
		return 0
	}
	for i, line := range strings.Split(md, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "## ") && strings.HasPrefix(strings.ToLower(strings.TrimSpace(t[3:])), want) {
			return i + 1
		}
	}
	return 0
}

// FixPrompt is the chat prompt that fixes the bug of spec s (at rel,
// relative to the project root) test first.
func FixPrompt(rel string, s Spec) string {
	var b strings.Builder
	b.WriteString("Fix the bug \"" + s.Title + "\" from the bugfix spec " + rel + " (attached). Work test first:\n" +
		"1. For each expected behavior, write a test and run it: show that it fails now.\n" +
		"2. For each unchanged behavior, write a guard test (or name the test that already covers it) and run it: show that it passes now.\n" +
		"3. Make the smallest fix. Run the new tests and the guard tests: all of them must pass.\n" +
		"Do not change the unchanged behavior. Do not edit the three sections of the spec; add a \"## Fix\" section at its end " +
		"with what you changed and the tests.\n")
	list := func(head string, items []string) {
		b.WriteString("\n" + head + ":\n")
		for _, it := range items {
			b.WriteString("- " + it + "\n")
		}
	}
	list(headCurrent, s.Current)
	list(headExpected, s.Expected)
	list(headUnchanged, s.Unchanged)
	if len(s.Steps) > 0 {
		list(headSteps, s.Steps)
	}
	return b.String()
}

// Entry is one spec on disk.
type Entry struct {
	Path  string // absolute
	Rel   string // relative to the root, with "/"
	Title string
	Spec  Spec
	Mod   time.Time
}

// List is every bugfix spec under root, newest first. No specs folder:
// none and no error.
func List(root string) ([]Entry, error) {
	dir := filepath.Join(root, filepath.FromSlash(Dir))
	ents, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(dir, e.Name(), FileName)
		fi, err := os.Stat(p)
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		body, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := Parse(string(body))
		title := s.Title
		if title == "" {
			title = e.Name()
		}
		out = append(out, Entry{Path: p, Rel: Dir + "/" + e.Name() + "/" + FileName, Title: title, Spec: s, Mod: fi.ModTime()})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Mod.After(out[j].Mod) })
	return out, nil
}
