// Package projtasks reads a project's own task checklists: the Markdown
// "- [ ]" / "- [x]" lines of a few well-known files (TASKS.md, TODO.md,
// and the same names under docs/). It is a leaf: it reads files and
// nothing else, so the phone's project.tasks op (MOB-52) and its view can
// list them next to the issues, pull requests and merge requests.
//
// Reading is bounded: only regular files in the project (no symlinks, so
// nothing outside the project is read), at most MaxFileBytes each, at
// most MaxItems items in all, each text at most MaxText bytes.
package projtasks

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Files are the checklist files read, root-relative and slash-separated,
// in this order.
var Files = []string{"TASKS.md", "TODO.md", "docs/TASKS.md", "docs/TODO.md"}

// Limits.
const (
	MaxFileBytes = 1 << 20 // a bigger file is read up to here
	MaxItems     = 1000    // across all files
	MaxText      = 300     // bytes of one item's text
	MaxGroup     = 120     // bytes of one heading
)

// Item is one checklist line.
type Item struct {
	File  string // root-relative, slash-separated
	Line  int    // 1-based
	Group string // the nearest heading above it ("" none)
	Text  string // the text after the box, trimmed
	Done  bool   // [x] or [X]
	Depth int    // 0 a top item; 1 and more nested under one
}

// ID is the item's stable id within its project: "<file>:<line>".
func (it Item) ID() string { return it.File + ":" + strconv.Itoa(it.Line) }

// ErrNone: the project has none of the checklist files.
var ErrNone = errors.New("no checklist file (TASKS.md, TODO.md)")

// Read reads every checklist file of root that exists. Files that do not
// exist are skipped; a symlink or a non-regular file is skipped too. It
// returns ErrNone when no file was read.
func Read(root string) ([]Item, error) {
	var out []Item
	read := 0
	for _, rel := range Files {
		if len(out) >= MaxItems {
			break
		}
		data, err := readFile(root, rel)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, errSkip) {
				continue
			}
			return out, err
		}
		read++
		items := Parse(rel, data)
		if room := MaxItems - len(out); len(items) > room {
			items = items[:room]
		}
		out = append(out, items...)
	}
	if read == 0 {
		return nil, ErrNone
	}
	return out, nil
}

var errSkip = errors.New("not a regular file")

func readFile(root, rel string) ([]byte, error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	// The file and each folder on the way must be real (no symlink).
	if dir := filepath.Dir(rel); dir != "." {
		fi, err := os.Lstat(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			return nil, errSkip
		}
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errSkip
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, MaxFileBytes))
}

// Parse reads the checklist items of one file's text. Headings ("#" to
// "######") set the group of the items under them; a fenced code block
// is skipped. A box is "[ ]", "[x]" or "[X]" after "-", "*", "+" or
// "1." / "1)"; an empty item is skipped. At most MaxItems.
func Parse(file string, data []byte) []Item {
	var out []Item
	group := ""
	fence := ""
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), MaxFileBytes)
	line := 0
	for sc.Scan() && len(out) < MaxItems {
		line++
		raw := sc.Text()
		trim := strings.TrimLeft(raw, " \t")
		if f := fenceOf(trim); f != "" {
			switch {
			case fence == "":
				fence = f
			case strings.HasPrefix(f, fence[:1]) && len(f) >= len(fence):
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		if h, ok := heading(trim); ok {
			group = clip(h, MaxGroup)
			continue
		}
		text, done, ok := box(trim)
		if !ok || text == "" {
			continue
		}
		out = append(out, Item{File: file, Line: line, Group: group, Text: clip(text, MaxText), Done: done, Depth: depth(raw[:len(raw)-len(trim)])})
	}
	return out
}

// fenceOf is the fence a line opens or closes ("```", "~~~~"), or "".
func fenceOf(s string) string {
	for _, c := range []byte{'`', '~'} {
		n := 0
		for n < len(s) && s[n] == c {
			n++
		}
		if n >= 3 {
			return s[:n]
		}
	}
	return ""
}

// heading is an ATX heading's text.
func heading(s string) (string, bool) {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || (n < len(s) && s[n] != ' ' && s[n] != '\t') {
		return "", false
	}
	h := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(s[n:]), "#"))
	return h, h != ""
}

// box reads "- [ ] text" (and its kin) from a line without its indent.
func box(s string) (text string, done, ok bool) {
	switch {
	case strings.HasPrefix(s, "- "), strings.HasPrefix(s, "* "), strings.HasPrefix(s, "+ "):
		s = s[2:]
	default:
		n := 0
		for n < len(s) && n < 9 && s[n] >= '0' && s[n] <= '9' {
			n++
		}
		if n == 0 || n+1 >= len(s) || (s[n] != '.' && s[n] != ')') || s[n+1] != ' ' {
			return "", false, false
		}
		s = s[n+2:]
	}
	s = strings.TrimLeft(s, " ")
	if len(s) < 3 || s[0] != '[' || s[2] != ']' || (len(s) > 3 && s[3] != ' ' && s[3] != '\t') {
		return "", false, false
	}
	switch s[1] {
	case ' ':
	case 'x', 'X':
		done = true
	default:
		return "", false, false
	}
	return strings.TrimSpace(s[3:]), done, true
}

// depth is the nesting of an indent: a tab or two spaces is one level.
func depth(indent string) int {
	w := 0
	for _, c := range indent {
		if c == '\t' {
			w += 2
		} else {
			w++
		}
	}
	return w / 2
}

// clip cuts s to at most n bytes on a rune edge, marking the cut with "…".
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
