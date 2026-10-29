package cellwidth

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
)

var samples = []string{
	"", "abc", "naïve café", "é", "中文日本語", "👩‍💻", "🇮🇷", "ok🇮🇷🇮🇷x",
	"❤️", "سلام دنیا", "a\tb", "─│▕", "1️⃣",
}

// The width of plain text is the width lipgloss and the frame checks use.
func TestStringWidthMatchesFrameWidth(t *testing.T) {
	for _, s := range samples {
		if got, want := StringWidth(s), ansi.StringWidth(s); got != want {
			t.Errorf("StringWidth(%q) = %d, want %d", s, got, want)
		}
	}
}

// A flag is two cells (go-runewidth says one).
func TestFlagIsTwoCells(t *testing.T) {
	if got := StringWidth("🇮🇷"); got != 2 {
		t.Fatalf("flag width %d, want 2", got)
	}
	if runewidth.StringWidth("🇮🇷") == 2 {
		t.Log("go-runewidth now agrees; this package may be dropped")
	}
}

func TestTruncateKeepsGlyphsWhole(t *testing.T) {
	cases := []struct {
		s    string
		w    int
		want string
	}{
		{"abcdef", 4, "abc…"},
		{"abc", 4, "abc"},
		{"🇮🇷🇮🇷🇮🇷", 4, "🇮🇷…"},
		{"中文日本語", 6, "中文…"},
		{"中文日本語", 5, "中文…"},
		{"👩‍💻👩‍💻👩‍💻", 5, "👩‍💻👩‍💻…"},
		{"café au lait", 5, "café…"},
	}
	for _, c := range cases {
		got := Truncate(c.s, c.w, "…")
		if got != c.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", c.s, c.w, got, c.want)
		}
		if StringWidth(got) > c.w {
			t.Errorf("Truncate(%q, %d) = %q is %d cells", c.s, c.w, got, StringWidth(got))
		}
	}
}

func TestTruncateLeftPadsACutGlyph(t *testing.T) {
	if got := TruncateLeft("中文ab", 1, "…"); got != "… 文ab" {
		t.Errorf("got %q", got)
	}
	if got := TruncateLeft("🇮🇷ab", 2, ""); got != "ab" {
		t.Errorf("got %q", got)
	}
	if got := TruncateLeft("ab", 5, "x"); got != "x" {
		t.Errorf("got %q", got)
	}
}

func TestFillRight(t *testing.T) {
	if got := FillRight("🇮🇷", 4); got != "🇮🇷  " {
		t.Errorf("got %q", got)
	}
	if got := FillRight("abcdef", 4); got != "abcdef" {
		t.Errorf("got %q", got)
	}
}

// Everything go-runewidth sizes right stays the same.
func TestSameAsRunewidthForCommonText(t *testing.T) {
	for _, s := range []string{"abc", "naïve café", "中文日本語", "سلام", "─│▕▎", "\x1b[31mred\x1b[0m", "a\tb"} {
		if got, want := StringWidth(s), runewidth.StringWidth(s); got != want {
			t.Errorf("StringWidth(%q) = %d, go-runewidth %d", s, got, want)
		}
	}
}

// In a CJK locale ambiguous glyphs ("…", "─") are two cells, as with
// go-runewidth.
func TestEastAsianAmbiguousStillWide(t *testing.T) {
	old := runewidth.DefaultCondition.EastAsianWidth
	defer func() { runewidth.DefaultCondition.EastAsianWidth = old }()
	runewidth.DefaultCondition.EastAsianWidth = true
	if got := StringWidth("…─"); got != 4 {
		t.Errorf("ambiguous width %d, want 4", got)
	}
	if got := StringWidth("🇮🇷"); got != 2 {
		t.Errorf("flag width %d, want 2", got)
	}
}
