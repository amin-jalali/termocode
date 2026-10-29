package testkit

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Spec (SCN-11): Given files whose names hold CJK, emoji (ZWJ, flag),
// a combining accent and Persian,
// when they are open in tabs (more than fit at 80x24),
// then the tab bar and the breadcrumbs keep every row at the window
// width and cut no glyph in half, also when switching tabs.
func TestWideTabTitlesAndBreadcrumbs(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 34}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			if testing.Short() {
				t.Skip("starts nvim")
			}
			d := New(t, Options{W: sz[0], H: sz[1], Nvim: true, Files: wideFiles()})
			for _, f := range []string{
				"中文目录/日本語のとても長いファイル名です.md",
				"emoji-" + wideDev + "-" + wideFlag + "-notes.txt",
				"caf" + wideMark + "-naïve.txt",
				"فارسی-and-English-名前.txt",
			} {
				open(d, f)
				checkGlyphs(d, "tabs after opening "+f)
			}
			for i := 0; i < 4; i++ {
				d.Key("ctrl+pgup")
				checkGlyphs(d, fmt.Sprintf("tab switch %d", i))
			}
			// Hover the tab bar: tooltips with the full names.
			for x := 0; x < d.W; x += 4 {
				d.Move(x, 0)
				checkGlyphs(d, fmt.Sprintf("hover tab bar at %d", x))
			}
		})
	}
}

// Spec (SCN-11): Given a terminal tab,
// when a command prints CJK, emoji (ZWJ, flags), combining accents,
// Persian and a long wide line that wraps, and sets the tab title to a
// wide text,
// then the panel and its tab title keep every row at the window width
// and cut no glyph in half.
func TestWideTerminalOutputAndTitle(t *testing.T) {
	needBash(t)
	for _, sz := range [][2]int{{80, 24}, {120, 34}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			if testing.Short() {
				t.Skip("starts nvim")
			}
			d := New(t, Options{W: sz[0], H: sz[1], Nvim: true, Files: wideFiles()})
			openBashTerminal(d)
			long := strings.Repeat("中文"+wideDev+wideFlag, 30)
			termSend(d, 0, fmt.Sprintf("printf '%%s\\n%%s\\n\\033]0;%%s\\007' '%s' '%s' '%s'; echo done-$((40+2))\n",
				wideMixed, long, "標題 "+wideDev+" "+wideFlag+" title"))
			if !d.WaitForText("done-42", 10*time.Second) {
				t.Fatalf("the shell did not run:\n%s", screen(d))
			}
			d.Settle(300 * time.Millisecond)
			checkGlyphs(d, "terminal")
			if !strings.Contains(screen(d), "中文 "+wideDev+" "+wideFlag) {
				t.Errorf("terminal does not show the wide line whole:\n%s", screen(d))
			}
		})
	}
}
