package rtl

import (
	"strings"
	"testing"
)

func TestDirAndModeNames(t *testing.T) {
	if LTR.String() != "ltr" || RTL.String() != "rtl" {
		t.Errorf("Dir names: %s %s", LTR, RTL)
	}
	for m, want := range map[Mode]string{Termocode: "termocode", Terminal: "terminal", Off: "off"} {
		if m.String() != want {
			t.Errorf("Mode %d = %q, want %q", m, m.String(), want)
		}
		if Resolve(want, nil) != m {
			t.Errorf("Resolve(%q) = %v", want, Resolve(want, nil))
		}
	}
}

func TestBaseDirWithAccentedLatin(t *testing.T) {
	if got := BaseDir("12 éשלום"); got != LTR {
		t.Errorf("first strong letter é: %v, want ltr", got)
	}
	if got := BaseDir("— שלום é"); got != RTL {
		t.Errorf("first strong letter ש: %v, want rtl", got)
	}
}

// Hebrew letters are not shaped, so the expected strings are plain.
func TestBidiRulesOnRealText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		dir  Dir
		want string
	}{
		{"brackets mirror in RTL", "(שלום)", RTL, "(םולש)"},
		{"numbers keep their order", "א 12 ב", RTL, "ב 12 א"},
		{"plus between numbers", "א 1+2", RTL, "1+2 א"},
		{"percent next to a number", "א 5%", RTL, "5% א"},
		{"dollar before a number", "א $5", RTL, "$5 א"},
		{"comma between Arabic digits", "א ١,٢", RTL, "١,٢ א"},
		{"trailing spaces stay at the end", "אב  ", LTR, "בא  "},
		{"a tab is a separator", "אב\tגד", LTR, "בא\tדג"},
		{"direction marks are not drawn", "א‏ב", RTL, "בא"},
	}
	for _, c := range cases {
		if got := Visual(c.in, c.dir); got != c.want {
			t.Errorf("%s: Visual(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestDigitsAfterArabicLettersStayInOrder(t *testing.T) {
	got := Visual("ب 123", RTL)
	if !strings.HasPrefix(got, "123 ") {
		t.Errorf("Visual = %q, want the number first and in order", got)
	}
}

func TestMarkAtTheStartOfALine(t *testing.T) {
	got := Visual("ְאב", RTL)
	if !strings.Contains(got, "ב") || !strings.Contains(got, "א") || !strings.Contains(got, "ְ") {
		t.Errorf("a leading mark lost text: %q", got)
	}
}

func TestBracketsInsideAnAtomMirrorToo(t *testing.T) {
	rs := []rune("x א(ב")
	atoms := []int{0, 0, 1, 1, 1}
	got := Text(Line(rs, LTR, atoms))
	if got != "x ב)א" {
		t.Errorf("atom = %q, want %q", got, "x ב)א")
	}
}
