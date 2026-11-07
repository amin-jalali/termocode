package ai

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// Tool views (mobile plan, MOB-11): a tool without a registry view turns
// its result into one block for the phone. A view reads only the text the
// model got (out) and the call's args, so the phone never sees more than
// the model did: secret files, passwords and keys are already gone. A
// view returns nil when it has nothing to show; the chat text stays.

// ViewDone gives b back when it passes every block rule, else nil (the
// chat then shows the text only: the safe choice). The id and the
// source stay empty: the agent fills them (the call id, the tool's
// display name), so they are checked with a stand-in id here.
func ViewDone(b *blocks.Block) *blocks.Block {
	if b == nil {
		return nil
	}
	c := *b
	if c.ID == "" {
		c.ID = "view"
	}
	if c.Validate() != nil {
		return nil
	}
	return b
}

// ViewText is s on one line, without control characters, cut to n runes
// with "…".
func ViewText(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.IsControl(r), r == utf8.RuneError:
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		s = strings.TrimSpace(string(r[:max(n-1, 0)])) + "…"
	}
	return s
}

// ViewItemID is the id of the i-th item of a tool view.
func ViewItemID(i int) string { return "i" + strconv.Itoa(i) }

// ViewPath is p as a block ref path, or "" when it is not workspace-
// relative (absolute, a drive, a URL, "..").
func ViewPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || len(p) > blocks.MaxPath || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) ||
		(len(p) > 1 && p[1] == ':') || strings.Contains(p, "://") {
		return ""
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return ""
		}
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return ""
		}
	}
	return p
}

// viewList is a list block of a tool view: items cut to the cap, Total and
// Truncated set when some did not fit.
func viewList(title string, items []blocks.BlockItem, total int) *blocks.Block {
	b := &blocks.Block{Kind: blocks.KindList, Title: ViewText(title, blocks.MaxTitle)}
	if len(items) > blocks.MaxItems {
		items = items[:blocks.MaxItems]
	}
	b.Items = items
	if total > len(items) {
		b.Total, b.Truncated = total, true
	}
	return b
}

// ListDirView is the block of list_dir: one row per entry in the tool's
// own order (os.ReadDir sorts by name), folders with a "dir" badge. Each
// row refers to the entry, so the phone can send it back to the chat.
func ListDirView(args map[string]any, out string) *blocks.Block {
	raw := strings.TrimSpace(ArgString(args, "path"))
	dir := strings.Trim(ViewPath(raw), "/")
	if dir == "." {
		dir = ""
	}
	refs := raw == "" || dir != "" || strings.Trim(raw, "/") == "." // a path outside the workspace: no refs
	lines := strings.Split(out, "\n")
	cut := false
	if i := len(lines) - 1; i > 0 && strings.HasPrefix(lines[i], "… [truncated") {
		// capOut cut the text mid-line: the line before the mark is partial.
		lines, cut = lines[:i-1], true
	}
	var items []blocks.BlockItem
	n := 0
	for _, l := range lines {
		name := strings.TrimRight(l, "\r")
		if name == "" {
			continue
		}
		n++
		if len(items) == blocks.MaxItems {
			continue
		}
		isDir := strings.HasSuffix(name, "/")
		rel := strings.TrimSuffix(name, "/")
		if dir != "" {
			rel = dir + "/" + rel
		}
		it := blocks.BlockItem{ID: ViewItemID(len(items)), Title: ViewText(name, blocks.MaxTitle)}
		if isDir {
			it.Badge = "dir"
		}
		if p := ViewPath(rel); p != "" && refs {
			it.Ref = &blocks.BlockRef{Kind: "file", Path: p}
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		if cut {
			return nil
		}
		return ViewDone(&blocks.Block{Kind: blocks.KindStatus, State: blocks.ToneIdle,
			Title: ViewText("The folder "+orDot(dir)+" is empty", blocks.MaxTitle)})
	}
	title := "Files in " + orDot(dir) + " · " + strconv.Itoa(n)
	if cut {
		title += "+"
	}
	b := viewList(title, items, n)
	b.Truncated = b.Truncated || cut
	return ViewDone(b)
}

// SearchView is the block of search: one row per match ("path:line"
// title, the matching line below), each referring to its file and line.
// The "(showing N of M)" line sets Total.
func SearchView(args map[string]any, out string) *blocks.Block {
	if strings.TrimSpace(out) == "no matches" {
		return ViewDone(&blocks.Block{Kind: blocks.KindStatus, State: blocks.ToneIdle,
			Title: ViewText("No matches for "+strconv.Quote(ArgString(args, "query")), blocks.MaxTitle)})
	}
	var items []blocks.BlockItem
	total := 0
	for _, l := range strings.Split(out, "\n") {
		if l == "" {
			continue
		}
		if s, ok := strings.CutPrefix(l, "(showing "); ok {
			if _, of, ok := strings.Cut(strings.TrimSuffix(s, ")"), " of "); ok {
				if t, err := strconv.Atoi(of); err == nil {
					total = t
				}
			}
			continue
		}
		file, rest, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		num, preview, ok := strings.Cut(rest, ": ")
		line, err := strconv.Atoi(num)
		if !ok || err != nil {
			continue
		}
		if len(items) == blocks.MaxItems {
			continue
		}
		it := blocks.BlockItem{ID: ViewItemID(len(items)), Title: ViewText(file+":"+num, blocks.MaxTitle),
			Sub: ViewText(preview, blocks.MaxSub)}
		if p := ViewPath(file); p != "" {
			it.Ref = &blocks.BlockRef{Kind: "lines", Path: p, Line: line}
		}
		items = append(items, it)
	}
	if len(items) == 0 {
		return nil
	}
	total = max(total, len(items))
	title := "Search " + strconv.Quote(ArgString(args, "query")) + " · " + strconv.Itoa(total)
	return ViewDone(viewList(title, items, total))
}

// TodosView is the block of todo_read: one row per todo, ✓ done, ◐ in
// progress, ○ pending (the page draws the shape with the tone).
func TodosView(ts []Todo) *blocks.Block {
	if len(ts) == 0 {
		return ViewDone(&blocks.Block{Kind: blocks.KindStatus, State: blocks.ToneIdle, Title: "The todo list is empty"})
	}
	items := make([]blocks.BlockItem, 0, min(len(ts), blocks.MaxItems))
	for i, t := range ts {
		if i == blocks.MaxItems {
			break
		}
		it := blocks.BlockItem{ID: ViewItemID(i), Title: ViewText(t.Text, blocks.MaxTitle)}
		switch t.Status {
		case TodoDone:
			it.Tone, it.Badge = blocks.ToneOK, "done"
		case TodoActive:
			it.Tone, it.Badge = blocks.ToneBusy, "doing"
		default:
			it.Tone = blocks.ToneIdle
		}
		if it.Title == "" {
			it.Title = "(empty)"
		}
		items = append(items, it)
	}
	done := len(ts) - TodosOpen(ts)
	title := "Todos · " + strconv.Itoa(done) + " of " + strconv.Itoa(len(ts)) + " done"
	return ViewDone(viewList(title, items, len(ts)))
}

// TodosTextView is TodosView of the todo_read text ("[x] a", "[~] b",
// "[ ] c" lines), for a view that only has the result.
func TodosTextView(_ map[string]any, out string) *blocks.Block {
	var ts []Todo
	for _, l := range strings.Split(out, "\n") {
		if len(l) < 4 || l[0] != '[' || l[2] != ']' {
			continue
		}
		t := Todo{Text: strings.TrimSpace(l[3:])}
		switch l[1] {
		case 'x':
			t.Status = TodoDone
		case '~':
			t.Status = TodoActive
		default:
			t.Status = TodoPending
		}
		ts = append(ts, t)
	}
	return TodosView(ts)
}
