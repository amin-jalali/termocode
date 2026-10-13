package theme

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// legacyParseHex is ParseHex as it was (fmt.Sscanf), the reference the
// fast parser must match.
func legacyParseHex(s string) (RGB, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return RGB{}, false
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "%02x%02x%02x", &r, &g, &b); err != nil {
		return RGB{}, false
	}
	return RGB{r, g, b}, true
}

// legacyMix is Mix without the memo.
func legacyMix(a, b Color256, t float64) lipgloss.Color {
	return Lerp(ToRGB(a), ToRGB(b), t).LG()
}

func TestParseHexMatchesLegacy(t *testing.T) {
	cases := []string{
		"#000000", "#ffffff", "#FFFFFF", "#1e1E1e", "abcdef", " #123456 ", "\t#a0b0c0\n",
		"", "#", "#12345", "#1234567", "##123456", "#12345g", "#g12345", "#-12345",
		"#+12345", "#1 2345", "# 12345", "#12 345", "#0x1234", "#12\t345", "color12",
		"#ｆｆｆｆｆｆ", "#é1234", "123456#", "#1_2345",
	}
	for _, s := range cases {
		checkParseHex(t, s)
	}
	// Every byte in every slot.
	for i := 0; i < 256; i++ {
		for j := 0; j < 256; j += 7 {
			checkParseHex(t, "#"+string([]byte{byte(i), byte(j)})+"a0b0")
			checkParseHex(t, "#a0b0"+string([]byte{byte(j), byte(i)}))
		}
	}
}

// checkParseHex compares ParseHex with the old Sscanf parser. They agree
// on every input except one class, on purpose: Sscanf accepted strings
// with non-hex bytes in them ("#12345g" read as #123405, "#1 2345" as
// #012345), against ParseHex's own contract. ParseHex now rejects those.
func checkParseHex(t *testing.T, s string) {
	t.Helper()
	gotC, gotOK := ParseHex(s)
	wantC, wantOK := legacyParseHex(s)
	if gotOK == wantOK && gotC == wantC {
		return
	}
	if !gotOK && wantOK && !strictHex(s) {
		return // the Sscanf quirk above
	}
	t.Errorf("ParseHex(%q) = %v,%v; legacy %v,%v", s, gotC, gotOK, wantC, wantOK)
}

func strictHex(s string) bool {
	s = strings.TrimPrefix(strings.TrimSpace(s), "#")
	if len(s) != 6 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !strings.ContainsRune("0123456789abcdefABCDEF", rune(s[i])) {
			return false
		}
	}
	return true
}

func TestParseHexRejectsNonHex(t *testing.T) {
	for _, s := range []string{"#12345g", "#1 2345", "# 12345", "#12 345", "#+12345"} {
		if c, ok := ParseHex(s); ok {
			t.Errorf("ParseHex(%q) = %v, want not ok", s, c)
		}
	}
}

func FuzzParseHexMatchesLegacy(f *testing.F) {
	for _, s := range []string{"#123456", "abcdef", "#1 2345", "#+12345", " #ABCDEF "} {
		f.Add(s)
	}
	f.Fuzz(checkParseHex)
}

func TestMixMatchesLegacy(t *testing.T) {
	ts := []float64{-1, 0, 0.1, 0.25, 1.0 / 3, 0.5, 0.75, 0.999, 1, 2, math.NaN()}
	check := func(what string) {
		t.Helper()
		for _, a := range []Color256{0, 15, 16, 24, 102, 234, 252, 255, 300, -1} {
			for _, b := range []Color256{0, 32, 236, 255} {
				for _, x := range ts {
					for i := 0; i < 2; i++ { // second call hits the memo
						if got, want := Mix(a, b, x), legacyMix(a, b, x); got != want {
							t.Fatalf("%s: Mix(%d,%d,%v) = %q, legacy %q", what, a, b, x, got, want)
						}
					}
				}
			}
		}
	}
	check("default theme")
	// A theme switch changes what the tokens resolve to; the memo must not
	// hand back the old theme's blends.
	t.Cleanup(func() { ApplyTheme("vscode-dark-plus") })
	for _, nt := range AvailableThemes() {
		ApplyTheme(nt.ID)
		check(nt.ID)
	}
}

func TestMixMemoIsBounded(t *testing.T) {
	for i := 0; i < mixMemoMax*3; i++ {
		Mix(BgEditor, TextPrimary, float64(i)/float64(mixMemoMax*3))
	}
	mixMemo.Lock()
	n := len(mixMemo.m)
	mixMemo.Unlock()
	if n > mixMemoMax {
		t.Fatalf("Mix memo has %d entries, want <= %d", n, mixMemoMax)
	}
}

func TestMixConcurrent(t *testing.T) {
	done := make(chan struct{})
	for g := 0; g < 8; g++ {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 2000; i++ {
				Mix(Color256(g), Color256(i%256), float64(i%7)/7)
			}
		}(g)
	}
	for g := 0; g < 8; g++ {
		<-done
	}
}

func BenchmarkParseHex(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		ParseHex("#1e1e1e")
	}
}

func BenchmarkMix(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Mix(BgEditor, TextPrimary, 0.25)
	}
}

func BenchmarkParseHexLegacy(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		legacyParseHex("#1e1e1e")
	}
}

func BenchmarkMixLegacy(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		legacyMix(BgEditor, TextPrimary, 0.25)
	}
}
