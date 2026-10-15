package app

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/search"
)

// spans returns the text each highlight range covers.
func spans(text string, hl [][2]int) []string {
	var out []string
	for _, h := range hl {
		out = append(out, text[h[0]:h[1]])
	}
	return out
}

func TestTrimFindPreview(t *testing.T) {
	long := strings.Repeat("x", 60) + " needle " + strings.Repeat("y", 300)
	cases := []struct {
		name     string
		preview  string
		ms       []search.MatchRange
		wantText string // prefix of the kept text ("" = skip)
		wantHL   []string
		cut      string
	}{
		{"indent stripped", "\t\tfoo(needle)", []search.MatchRange{{Start: 6, End: 12}}, "foo(needle)", []string{"needle"}, ""},
		{"two matches", "a needle b needle", []search.MatchRange{{Start: 2, End: 8}, {Start: 11, End: 17}}, "a needle b needle", []string{"needle", "needle"}, ""},
		{"tab inside", "a\tneedle", []search.MatchRange{{Start: 2, End: 8}}, "a   needle", []string{"needle"}, ""},
		{"multibyte", "é漢😀 needle", []search.MatchRange{{Start: 10, End: 16}}, "é漢😀 needle", []string{"needle"}, ""},
		{"left and right cut", long, []search.MatchRange{{Start: 61, End: 67}}, "…", []string{"needle"}, "lr"},
		{"trailing CR", "needle\r", []search.MatchRange{{Start: 0, End: 6}}, "needle", []string{"needle"}, ""},
	}
	for _, c := range cases {
		text, hl, cut := trimFindPreview(c.preview, c.ms)
		if !strings.HasPrefix(text, c.wantText) {
			t.Errorf("%s: text %q, want prefix %q", c.name, text, c.wantText)
		}
		if got := spans(text, hl); strings.Join(got, ",") != strings.Join(c.wantHL, ",") {
			t.Errorf("%s: spans %q, want %q (text %q)", c.name, got, c.wantHL, text)
		}
		if cut != c.cut {
			t.Errorf("%s: cut %q, want %q", c.name, cut, c.cut)
		}
		if !utf8.ValidString(text) {
			t.Errorf("%s: invalid UTF-8 %q", c.name, text)
		}
		if w := runewidth.StringWidth(text); w > findRowMaxCells {
			t.Errorf("%s: %d cells > %d", c.name, w, findRowMaxCells)
		}
		if strings.Contains(text, "\t") {
			t.Errorf("%s: tab kept: %q", c.name, text)
		}
	}
}

// A match beyond the right cut is dropped, never half-kept past the text.
func TestTrimFindPreviewDropsRangePastCut(t *testing.T) {
	p := "needle " + strings.Repeat("z", 400) + " needle"
	text, hl, cut := trimFindPreview(p, []search.MatchRange{{Start: 0, End: 6}, {Start: 408, End: 414}})
	if cut != "r" {
		t.Errorf("cut = %q", cut)
	}
	if got := spans(text, hl); len(got) != 1 || got[0] != "needle" {
		t.Errorf("spans = %q", got)
	}
}

func TestBuildFindRows(t *testing.T) {
	lines, rows := buildFindRows([]search.Result{
		{Path: "/ws/a.go", Line: 9, Col: 1, Preview: "nine", Matches: []search.MatchRange{{Start: 0, End: 4}}},
		{Path: "/ws/b/c.go", Line: 1, Col: 3, Preview: "  one", Matches: []search.MatchRange{{Start: 2, End: 5}}},
		{Path: "/ws/a.go", Line: 2, Col: 1, Preview: "two", Matches: []search.MatchRange{{Start: 0, End: 3}}},
	}, []string{"/ws"})
	want := []string{
		"      a.go",
		"      2   two",
		"      9   nine",
		"",
		"      b/c.go",
		"      1   one",
		"",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	if len(rows) != len(lines) {
		t.Fatalf("%d rows for %d lines", len(rows), len(lines))
	}
	h := rows[4]
	if h["k"] != "h" || h["file"] != "/ws/b/c.go" || h["n"] != 1 || h["name_at"] != 8 {
		t.Errorf("header row = %v", h)
	}
	m := rows[5]
	if m["k"] != "m" || m["line"] != 1 || m["col"] != 3 || m["file"] != "/ws/b/c.go" {
		t.Errorf("match row = %v", m)
	}
	hl := m["hl"].([][2]int)
	if got := spans(lines[5], hl); len(got) != 1 || got[0] != "one" {
		t.Errorf("match spans = %q", got)
	}
	if rows[3]["k"] != "" {
		t.Errorf("blank row = %v", rows[3])
	}
}

func TestFindSummaryLine(t *testing.T) {
	cases := []struct {
		st   findState
		want string
		hl   string
	}{
		{findState{searching: true, matches: 120, files: 18}, "Searching… 120 matches in 18 files so far", "TermocodeFindSummary"},
		{findState{sum: search.Summary{Shown: 3, Total: 3, Files: 2}}, "3 matches across 2 files", "TermocodeFindSummary"},
		{findState{sum: search.Summary{Shown: 1, Total: 1, Files: 1}}, "1 match across 1 file", "TermocodeFindSummary"},
		{findState{sum: search.Summary{Shown: 2000, Total: 5123, Files: 40}}, "Showing 2000 of 5123 matches across 40 files · search_max_results", "TermocodeFindWarn"},
		{findState{errText: "boom"}, "Search failed: boom", "TermocodeFindError"},
		{findState{}, "No matches", "TermocodeFindSummary"},
	}
	for _, c := range cases {
		got, hl := findSummaryLine(c.st)
		if got != c.want || hl != c.hl {
			t.Errorf("findSummaryLine(%+v) = %q %s, want %q %s", c.st, got, hl, c.want, c.hl)
		}
	}
}

func BenchmarkBuildFindRows(b *testing.B) {
	var rs []search.Result
	for f := 0; f < 1500; f++ {
		for l := 0; l < 7; l++ {
			rs = append(rs, search.Result{Path: "/ws/pkg/f" + itoa(f) + ".go", Line: l + 1, Col: 5,
				Preview: "\tfunc needle(a, b int) int { return a + b } // a comment that is long",
				Matches: []search.MatchRange{{Start: 6, End: 12}}})
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buildFindRows(rs, []string{"/ws"})
	}
}
