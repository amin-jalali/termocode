package rtl

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

func hex(s string) string {
	var b strings.Builder
	for _, r := range s {
		fmt.Fprintf(&b, "%04X ", r)
	}
	return strings.TrimSpace(b.String())
}

func rev(s string) string {
	rs := []rune(s)
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	return string(rs)
}

// Shaping in logical order: each letter takes its isolated / initial /
// medial / final presentation form; lam + alef become one ligature.
func TestShapeWords(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"سلام", "ﺳﻼﻡ"},       // seen-ini, lam-alef final, meem iso
		{"ایران", "ﺍﯾﺮﺍﻥ"},    // Persian yeh initial
		{"لا", "ﻻ"},           // lam-alef isolated
		{"پژوهش", "ﭘﮋﻭﻫﺶ"},    // peh, jeh, waw, heh, sheen
		{"گفت‌وگو", "ﮔﻔﺖﻭﮔﻮ"}, // ZWNJ stops the join, then is dropped
		{"چک", "ﭼﮏ"},          // tcheh initial, keheh final
		{"کی", "ﮐﯽ"},          // keheh initial, farsi yeh final
		{"abc", "abc"},
	} {
		if got := Shape(tc.in); got != tc.want {
			t.Errorf("Shape(%q) = %s, want %s", tc.in, hex(got), hex(tc.want))
		}
	}
}

func TestBaseDir(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want Dir
	}{
		{"سلام world", RTL},
		{"hello سلام", LTR},
		{"123 سلام", RTL}, // digits are not strong
		{"", LTR},
		{"(!) …", LTR},
	} {
		if got := BaseDir(tc.in); got != tc.want {
			t.Errorf("BaseDir(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if HasRTL("hello") || !HasRTL("hi سلام") {
		t.Error("HasRTL")
	}
}

func TestVisualMixed(t *testing.T) {
	salam := "ﻡﻼﺳ" // سلام shaped, visual order
	donya := "ﺎﯿﻧﺩ"
	for _, tc := range []struct {
		in   string
		dir  Dir
		want string
	}{
		{"سلام", RTL, salam},
		{"سلام world 123", RTL, "world 123 " + salam}, // W7: 123 after a Latin word is LTR
		{"سلام 123", RTL, "123 " + salam},
		{"سلام world 123", LTR, salam + " world 123"},
		{"(سلام)", RTL, "(" + salam + ")"}, // brackets mirrored
		{"سلام (world) دنیا", RTL, donya + " (world) " + salam},
		{"hello سلام دنیا!", LTR, "hello " + donya + " " + salam + "!"},
		{"ایران", RTL, rev("ﺍﯾﺮﺍﻥ")},
		{"plain English", LTR, "plain English"},
		{"سلام ", RTL, " " + salam}, // L1: the trailing space stays at the line end
	} {
		if got := Visual(tc.in, tc.dir); got != tc.want {
			t.Errorf("Visual(%q, %v) =\n  %s\nwant\n  %s", tc.in, tc.dir, hex(got), hex(tc.want))
		}
	}
}

// Glyphs map back to the logical runes they came from, so a copy can
// give the original text.
func TestLineMapsToLogical(t *testing.T) {
	rs := []rune("سلام")
	gs := Line(rs, RTL, nil)
	want := []Glyph{{"ﻡ", 3, 4}, {"ﻼ", 1, 3}, {"ﺳ", 0, 1}}
	if fmt.Sprint(gs) != fmt.Sprint(want) {
		t.Fatalf("Line = %v, want %v", gs, want)
	}
	if got := Pick(rs, gs); got != "سلام" {
		t.Errorf("Pick(all) = %q", got)
	}
	if got := Pick(rs, gs[:1]); got != "م" {
		t.Errorf("Pick(first visual) = %q, want the last logical letter", got)
	}
	// The ZWNJ is not drawn but is part of the glyph before it.
	rs = []rune("گفت‌وگو")
	if got := Pick(rs, Line(rs, RTL, nil)); got != string(rs) {
		t.Errorf("Pick keeps the ZWNJ: %q", got)
	}
}

// An atom (inline code) is one unit: it moves as a whole and keeps its
// inner order.
func TestAtoms(t *testing.T) {
	rs := []rune("سلام a+b")
	atoms := make([]int, len(rs))
	for i := 5; i < len(rs); i++ {
		atoms[i] = 1
	}
	got := Text(Line(rs, RTL, atoms))
	if want := "a+b ﻡﻼﺳ"; got != want {
		t.Errorf("atom line = %s, want %s", hex(got), hex(want))
	}
	// Neutral-only code keeps its order too.
	rs = []rune("سلام -> دنیا")
	atoms = make([]int, len(rs))
	atoms[5], atoms[6] = 1, 1
	if got := Text(Line(rs, RTL, atoms)); !strings.Contains(got, "->") {
		t.Errorf("atom reversed: %q", got)
	}
}

func TestMarksFollowBase(t *testing.T) {
	// beh + fatha + teh: the fatha stays after its letter in the output.
	got := Visual("بَت", RTL)
	rs := []rune(got)
	if len(rs) != 3 || rs[2] != 0x064E {
		t.Errorf("marks: %s", hex(got))
	}
}

// Every presentation form is one cell in both width libraries, so frame
// width invariants hold.
func TestPresentationFormsOneCell(t *testing.T) {
	for _, f := range forms {
		for _, r := range f {
			if r == 0 {
				continue
			}
			s := string(r)
			if ansi.StringWidth(s) != 1 || runewidth.StringWidth(s) != 1 {
				t.Errorf("%04X: ansi %d runewidth %d", r, ansi.StringWidth(s), runewidth.StringWidth(s))
			}
		}
	}
	for _, r := range []rune{0xFEF5, 0xFEF6, 0xFEF7, 0xFEF8, 0xFEF9, 0xFEFA, 0xFEFB, 0xFEFC} {
		if ansi.StringWidth(string(r)) != 1 || runewidth.RuneWidth(r) != 1 {
			t.Errorf("%04X not 1 cell", r)
		}
	}
}

func TestParseMode(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	for _, tc := range []struct {
		set  string
		env  map[string]string
		want Mode
	}{
		{"", nil, Termocode},
		{"auto", map[string]string{"VTE_VERSION": "7600"}, Termocode},
		{"auto", map[string]string{"KONSOLE_VERSION": "230805"}, Terminal},
		{"auto", map[string]string{"MLTERM": "3.9"}, Terminal},
		{"termocode", map[string]string{"KONSOLE_VERSION": "1"}, Termocode},
		{"terminal", nil, Terminal},
		{"off", nil, Off},
		{"bogus", nil, Termocode},
	} {
		if got := Resolve(tc.set, env(tc.env)); got != tc.want {
			t.Errorf("Resolve(%q, %v) = %v, want %v", tc.set, tc.env, got, tc.want)
		}
	}
}
