package app

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Find Results rows. The buffer holds plain text; every row also has an
// entry in a Lua row table
// that the jump keys, F4, folds and the decoration provider read, so
// nothing ever parses the text back.

const (
	findRowMaxCells = 160 // a match row's code is cut here, with "…"
	findLeftMax     = 40  // a first match further right than this…
	findLeftKeep    = 24  // …keeps only this many cells before it
	findTabWidth    = 4
	findRowPrefix   = 10 // "  %5d   " before the code
	findHeaderPad   = 6  // cells before a header's path: fold mark + icon
)

// findRow is one entry of the Lua row table: k is "h" (file header), "m"
// (match), "q" / "s" / "r" (query, summary and rule rows on top), "e"
// (empty-state text) or "" (blank).
type findRow = map[string]any

// trimFindPreview turns a result's line into the text a row shows: tabs
// expanded, indentation dropped, cut around the first match and at
// findRowMaxCells. It returns the byte ranges of the matches in that text
// and which sides were cut ("l", "r", "lr" or "").
func trimFindPreview(p string, ms []search.MatchRange) (string, [][2]int, string) {
	p = strings.TrimRight(p, "\r\n")
	out, pos := findExpandTabs(p)
	lead := len(out) - len(strings.TrimLeft(out, " "))
	text := strings.TrimRight(out[lead:], " ")

	clamp := func(v, lo, hi int) int { return max(lo, min(v, hi)) }
	var hl [][2]int
	for _, m := range ms {
		s := pos(clamp(m.Start, 0, len(p))) - lead
		e := pos(clamp(m.End, 0, len(p))) - lead
		s, e = clamp(s, 0, len(text)), clamp(e, 0, len(text))
		if e > s {
			hl = append(hl, [2]int{s, e})
		}
	}

	cut := ""
	const ell = "…"
	if len(hl) > 0 && findCells(text[:hl[0][0]]) > findLeftMax {
		first := hl[0][0]
		c, w := first, 0
		for c > 0 {
			r, size := utf8.DecodeLastRuneInString(text[:c])
			if w+runewidth.RuneWidth(r) > findLeftKeep {
				break
			}
			w += runewidth.RuneWidth(r)
			c -= size
		}
		if sp := strings.IndexByte(text[c:first], ' '); sp >= 0 && c+sp+1 < first {
			c += sp + 1
		}
		text = ell + text[c:]
		shift := len(ell) - c
		kept := hl[:0]
		for _, h := range hl {
			s, e := h[0]+shift, h[1]+shift
			if e > len(ell) {
				kept = append(kept, [2]int{max(s, len(ell)), e})
			}
		}
		hl = kept
		cut = "l"
	}
	if findCells(text) > findRowMaxCells {
		end, w := 0, 0
		for i, r := range text {
			rw := runewidth.RuneWidth(r)
			if w+rw > findRowMaxCells-1 {
				end = i
				break
			}
			w += rw
		}
		text = text[:end] + ell
		kept := hl[:0]
		for _, h := range hl {
			if h[0] < end {
				kept = append(kept, [2]int{h[0], min(h[1], end)})
			}
		}
		hl = kept
		cut += "r"
	}
	return text, hl, cut
}

// findExpandTabs expands tabs to spaces (and bad UTF-8 to U+FFFD) and
// maps each byte offset of p to its offset in the result. Plain ASCII
// without tabs (most code) is returned as is.
func findExpandTabs(p string) (string, func(int) int) {
	plain := true
	for i := 0; i < len(p) && plain; i++ {
		plain = p[i] != '\t' && p[i] < utf8.RuneSelf
	}
	if plain {
		return p, func(i int) int { return i }
	}
	pos := make([]int, len(p)+1)
	for i := range pos {
		pos[i] = -1
	}
	var b strings.Builder
	col := 0
	for i, r := range p {
		pos[i] = b.Len()
		if r == '\t' {
			n := findTabWidth - col%findTabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		col += runewidth.RuneWidth(r)
	}
	pos[len(p)] = b.Len()
	for j := len(p) - 1; j >= 0; j-- {
		if pos[j] < 0 {
			pos[j] = pos[j+1]
		}
	}
	return b.String(), func(i int) int { return pos[i] }
}

// findCells is the cell width of s; plain ASCII (most code) skips the
// grapheme walk of runewidth.StringWidth.
func findCells(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return runewidth.StringWidth(s)
		}
	}
	return len(s)
}

// buildFindRows renders results as file groups: a header (the display
// path) per file, one row per match, a blank line after each group. A
// streamed batch always holds whole files, so groups never split.
func buildFindRows(results []search.Result, roots []string) ([]string, []findRow) {
	type bucket struct {
		path string
		hits []search.Result
	}
	var order []*bucket
	byPath := map[string]*bucket{}
	for _, r := range results {
		bk, ok := byPath[r.Path]
		if !ok {
			bk = &bucket{path: r.Path}
			byPath[r.Path] = bk
			order = append(order, bk)
		}
		bk.hits = append(bk.hits, r)
	}
	lines := make([]string, 0, len(results)+2*len(order))
	rows := make([]findRow, 0, cap(lines))
	for _, bk := range order {
		sort.SliceStable(bk.hits, func(i, j int) bool { return bk.hits[i].Line < bk.hits[j].Line })
		disp := findDisplayPath(bk.path, roots)
		glyph, color := theme.FileIconFor(bk.path, false, false)
		hdr := findRow{"k": "h", "file": bk.path, "n": len(bk.hits),
			"name_at": findHeaderPad + strings.LastIndexByte(disp, filepath.Separator) + 1, "icon": glyph}
		if hex := theme.Hex(color); strings.HasPrefix(hex, "#") {
			hdr["icon_hl"] = "TermocodeFindIcon_" + hex[1:]
			hdr["icon_fg"] = hex
		}
		lines = append(lines, strings.Repeat(" ", findHeaderPad)+disp)
		rows = append(rows, hdr)
		for _, h := range bk.hits {
			text, hl, cut := trimFindPreview(h.Preview, h.Matches)
			for i := range hl {
				hl[i][0] += findRowPrefix
				hl[i][1] += findRowPrefix
			}
			num := strconv.Itoa(h.Line)
			lines = append(lines, "  "+strings.Repeat(" ", max(5-len(num), 0))+num+"   "+text)
			rows = append(rows, findRow{"k": "m", "file": bk.path, "line": h.Line, "col": max(h.Col, 1),
				"hl": hl, "cut": cut})
		}
		lines = append(lines, "")
		rows = append(rows, findRow{"k": ""})
	}
	return lines, rows
}

// findState is what the summary row (row 2) and the meta chip say.
type findState struct {
	searching      bool
	matches, files int // so far, while searching
	sum            search.Summary
	errText        string
	elapsed        time.Duration
	engine         string // "rg" or "basic"
}

// findSummaryLine is the text of row 2 and its highlight group.
func findSummaryLine(st findState) (string, string) {
	switch {
	case st.errText != "":
		return "Search failed: " + st.errText, "TermocodeFindError"
	case st.searching && st.matches == 0:
		return "Searching…", "TermocodeFindSummary"
	case st.searching:
		return fmt.Sprintf("Searching… %d match%s in %d file%s so far",
			st.matches, matchPlural(st.matches), st.files, plurals(st.files)), "TermocodeFindSummary"
	case st.sum.Truncated():
		return fmt.Sprintf("Showing %d of %d matches across %d file%s · search_max_results",
			st.sum.Shown, st.sum.Total, st.sum.Files, plurals(st.sum.Files)), "TermocodeFindWarn"
	case st.sum.Shown == 0:
		return "No matches", "TermocodeFindSummary"
	}
	return fmt.Sprintf("%d match%s across %d file%s",
		st.sum.Shown, matchPlural(st.sum.Shown), st.sum.Files, plurals(st.sum.Files)), "TermocodeFindSummary"
}
