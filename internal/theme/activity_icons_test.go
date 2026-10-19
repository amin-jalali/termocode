package theme

import (
	"testing"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Activity-bar / status icons in the Unicode and ASCII sets must be one
// plain codepoint (no variation selector) and one cell wide even when
// the terminal treats East-Asian "ambiguous" glyphs as wide.
func TestActivityIconsOneCell(t *testing.T) {
	wide := runewidth.NewCondition()
	wide.EastAsianWidth = true
	icons := map[string]Icon{
		"files": IconFiles, "search": IconSearch, "git": IconGit, "outline": IconOutline,
		"run": IconRun, "tests": IconTests, "services": IconServices, "containers": IconContainers, "debug": IconDebug, "extensions": IconExtensions,
		"terminal": IconTerminal, "account": IconAccount, "settings": IconSettings,
	}
	for name, ic := range icons {
		for tier, g := range map[string]string{"unicode": ic.Unicode, "ascii": ic.ASCII} {
			if utf8.RuneCountInString(g) != 1 {
				t.Errorf("%s %s icon %q must be a single codepoint", name, tier, g)
			}
			if w := runewidth.StringWidth(g); w != 1 {
				t.Errorf("%s %s icon %q is %d cells", name, tier, g, w)
			}
			if tier == "unicode" && wide.StringWidth(g) != 1 {
				t.Errorf("%s unicode icon %q is ambiguous-width (2 cells on CJK terminals)", name, g)
			}
		}
	}
}
