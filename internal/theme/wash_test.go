package theme

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Spec (UI review #1): hover, selection, focused selection and pressed
// states use one ladder of accent tints, and body text stays readable on
// every step of it, on every surface, in every theme.
func TestWashLadderKeepsTextReadable(t *testing.T) {
	defer ApplyTheme("vscode-dark-plus")
	levels := []WashLevel{WashHover, WashSelected, WashSelectedFocused, WashPressed}
	for i := 1; i < len(levels); i++ {
		if levels[i] <= levels[i-1] {
			t.Fatalf("wash levels must grow: %v", levels)
		}
	}
	for _, nt := range AvailableThemes() {
		ApplyTheme(nt.ID)
		for _, bg := range []Color256{BgEditor, BgSidebar, BgPanel, BgFloat, BgActivityBar} {
			for _, l := range levels {
				w, _ := ParseHex(string(Wash(bg, l)))
				if r := contrastRatio(ToRGB(TextPrimary), w); r < 4.5 {
					t.Errorf("%s: text on wash %.2f over %s: %.2f < 4.5", nt.ID, float64(l), Hex(bg), r)
				}
			}
		}
	}
}

// Spec: widgets do not invent their own accent tints; a background mixed
// toward the accent goes through Wash, so the same state looks the same
// everywhere.
func TestNoAdHocAccentWash(t *testing.T) {
	re := regexp.MustCompile(`(Mix|Lerp)\((theme\.)?(ToRGB\()?(theme\.)?Bg\w+\)?, *(theme\.)?(ToRGB\()?(theme\.)?BorderFocus\)?, *[0-9.]+\)`)
	root := filepath.Join("..", "..", "internal")
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		for _, m := range re.FindAllString(string(b), -1) {
			t.Errorf("%s: ad-hoc accent tint %q; use theme.Wash", p, m)
		}
		return nil
	})
}
