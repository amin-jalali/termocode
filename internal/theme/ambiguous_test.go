package theme

import (
	"testing"

	"golang.org/x/text/width"
)

// Spec (terminal R2): no Unicode-tier icon is East-Asian-Ambiguous. In a
// Chinese / Japanese / Korean locale GNOME Terminal, xterm and Konsole
// draw those 2 cells wide while termocode counts 1, so every icon shifts
// the pane borders and clicks land on the wrong thing.
func TestUnicodeIconsNotAmbiguousWidth(t *testing.T) {
	icons := map[string]Icon{
		"Files": IconFiles, "Search": IconSearch, "Git": IconGit, "Outline": IconOutline,
		"Run": IconRun, "Tests": IconTests, "Services": IconServices, "Debug": IconDebug, "Extensions": IconExtensions,
		"Terminal": IconTerminal, "Account": IconAccount, "Settings": IconSettings,
		"FolderClosed": IconFolderClosed, "FolderOpen": IconFolderOpen, "File": IconFile,
		"ViewTree": IconViewTree, "ViewFlat": IconViewFlat,
		"Branch": IconBranch, "Error": IconError, "Warning": IconWarning, "Dirty": IconDirty,
	}
	check := func(name, s string) {
		for _, r := range s {
			if width.LookupRune(r).Kind() == width.EastAsianAmbiguous {
				t.Errorf("%s: %q (U+%04X) is East-Asian-Ambiguous", name, string(r), r)
			}
		}
	}
	for name, ic := range icons {
		check(name, ic.Unicode)
	}
	for _, g := range unicodeFileGlyphs() {
		check("file glyph", g)
	}
}

// Spec (CP-11): the Containers glyphs of the Unicode tier are not
// East-Asian-Ambiguous either.
func TestContainersGlyphsNotAmbiguousWidth(t *testing.T) {
	icons := map[string]Icon{
		"Containers": IconContainers, "SvcUp": GlyphSvcUp, "SvcBad": GlyphSvcBad, "SvcStarting": GlyphSvcStarting,
		"SvcRestarting": GlyphSvcRestarting, "SvcStopped": GlyphSvcStopped, "SvcExited": GlyphSvcExited,
		"SvcUnknown": GlyphSvcUnknown, "Start": GlyphStart, "Stop": GlyphStop, "Refresh": GlyphRefresh,
		"Prune": GlyphPrune, "ContPaused": GlyphContPaused, "Expand": GlyphExpand, "TreeCollapsed": GlyphTreeCollapsed,
	}
	for name, ic := range icons {
		for _, r := range ic.Unicode {
			if width.LookupRune(r).Kind() == width.EastAsianAmbiguous {
				t.Errorf("%s: %q (U+%04X) is East-Asian-Ambiguous", name, string(r), r)
			}
		}
	}
}
