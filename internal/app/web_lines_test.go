package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEB-17/19): a lines delta on a window of buffer rows: inside,
// reaching in from above, all above (the window moves), trimmed to the
// tail; a change the window cannot know is refused (the hub then resets).
func TestLinesApplyWindow(t *testing.T) {
	ld := func(first, last int, lines ...string) webproto.LinesDelta {
		return webproto.LinesDelta{First: first, Last: last, Lines: lines}
	}
	for _, tc := range []struct {
		name      string
		first     int
		rows      string
		d         webproto.LinesDelta
		max       int
		wantFirst int
		want      string
		ok        bool
	}{
		{"inside", 0, "a b c", ld(1, 2, "B", "B2"), 0, 0, "a B B2 c", true},
		{"append", 0, "a b", ld(2, 2, "c"), 0, 0, "a b c", true},
		{"delete", 0, "a b c", ld(0, 2), 0, 0, "c", true},
		{"past the end", 0, "a b", ld(1, 3, "x"), 0, 0, "a b", false},
		{"above moves", 10, "k l", ld(0, 3), 4, 7, "k l", false}, // window shrinks under max with rows above
		{"above moves, full", 10, "k l m n", ld(0, 3), 4, 7, "k l m n", true},
		{"reaches in", 10, "k l m n", ld(8, 11, "X"), 4, 8, "X l m n", true},
		{"trim", 0, "a b c d", ld(4, 4, "e", "f"), 4, 2, "c d e f", true},
		{"negative", 0, "a", ld(-1, 0), 0, 0, "a", false},
	} {
		first, rows, ok := webLinesApply(tc.first, strings.Fields(tc.rows), tc.d, tc.max)
		if ok != tc.ok || (ok && (first != tc.wantFirst || strings.Join(rows, " ") != tc.want)) {
			t.Errorf("%s: first %d rows %q ok %v", tc.name, first, rows, ok)
		}
	}
}

// Spec (WEB-17): queued lines deltas merge when the second rewrites what
// the first wrote; the merged one carries the newer tick.
func TestLinesOutCoalesce(t *testing.T) {
	a := webLinesOut{LinesDelta: webproto.LinesDelta{Kind: "lines", First: 2, Last: 3, Lines: []string{"x"}, Tick: 5}}
	b := webLinesOut{LinesDelta: webproto.LinesDelta{Kind: "lines", First: 2, Last: 3, Lines: []string{"xy"}, Tick: 6}}
	m, ok := a.coalesce(b)
	if !ok || m.(webLinesOut).Tick != 6 || m.(webLinesOut).Lines[0] != "xy" {
		t.Fatalf("merge: %v %v", m, ok)
	}
	if _, ok := a.coalesce(webLinesOut{LinesDelta: webproto.LinesDelta{First: 3, Last: 4}}); ok {
		t.Fatal("another row merged")
	}
}
