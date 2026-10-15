package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/width"
)

// Spec (UI review #4): one glyph per meaning (more, close, pin, history,
// new, collapse) in every icon mode, each exactly one cell and, in the
// Unicode and ASCII tiers, never East-Asian-Ambiguous.
func TestGlyphTable(t *testing.T) {
	defer func(m IconMode) { CurrentIconMode = m }(CurrentIconMode)
	glyphs := map[string]Icon{"more": GlyphMore, "close": GlyphClose, "pin": GlyphPin,
		"history": GlyphHistory, "new": GlyphNew, "collapse": GlyphCollapse,
		// Source Control GRAPH (scm-graph design §2.3).
		"commit": GlyphCommit, "head": GlyphHead, "merge": GlyphMerge,
		"unpushed": GlyphUnpushed, "expand": GlyphExpand,
		// Terminal tabs: the Fix chip and the CLI agent marks (U9b, U8).
		"ai": GlyphAI, "agent-working": GlyphAgentWorking,
		"agent-waiting": GlyphAgentWaiting, "agent-done": GlyphAgentDone}
	for name, g := range glyphs {
		for _, m := range []IconMode{IconModeNerdFont, IconModeUnicode, IconModeASCII} {
			CurrentIconMode = m
			s := g.String()
			if ansi.StringWidth(s) != 1 {
				t.Errorf("%s in mode %v: %q is %d cells", name, m, s, ansi.StringWidth(s))
			}
			if m != IconModeNerdFont {
				for _, r := range s {
					if width.LookupRune(r).Kind() == width.EastAsianAmbiguous {
						t.Errorf("%s in mode %v: %q is ambiguous-width", name, m, s)
					}
				}
			}
		}
	}
}

// Spec (UI review #4): widgets take these from the table; the old
// one-off "more" (⋮) and close (×) characters do not come back.
func TestNoOneOffChromeGlyphs(t *testing.T) {
	for _, dir := range []string{"../app", "../tabbar", "../explorer"} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, _ := os.ReadFile(f)
			for _, bad := range []string{`"⋮"`, `"×"`, `Render("×")`} {
				if strings.Contains(string(b), bad) {
					t.Errorf("%s: one-off glyph %s; use theme.GlyphMore / GlyphClose", f, bad)
				}
			}
		}
	}
}
