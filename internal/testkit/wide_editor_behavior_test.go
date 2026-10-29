package testkit

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// wideEditorFiles has a line of mixed wide glyphs, a long CJK line that
// wraps and a Persian line.
func wideEditorFiles() map[string]string {
	f := wideFiles()
	f["wide.txt"] = wideMixed + "\n" + strings.Repeat("中文", 70) + "\n" + wideFa + " and English 42\n"
	return f
}

// cellsAt is the text drawn in the screen cells [x, x+w) of row y.
func cellsAt(d *Driver, x, y, w int) string {
	rows := strings.Split(screen(d), "\n")
	if y < 0 || y >= len(rows) {
		return ""
	}
	return ansi.Cut(rows[y], x, x+w)
}

// Spec (SCN-11): Given a file with CJK, an emoji ZWJ sequence, a flag, a
// combining accent and Persian on one line,
// when it is open in the editor,
// then the frame shows every glyph whole (the accent on its letter, the
// whole flag, the whole ZWJ emoji) and every row keeps the window width;
// and a long CJK line wraps without a too-wide row.
func TestWideEditorShowsWholeGlyphs(t *testing.T) {
	for _, sz := range [][2]int{{120, 34}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			if testing.Short() {
				t.Skip("starts nvim")
			}
			d := New(t, Options{W: sz[0], H: sz[1], Nvim: true, Files: wideEditorFiles()})
			open(d, "wide.txt")
			lua(d, `vim.wo.wrap = true; return ""`)
			d.Settle(200 * time.Millisecond)
			checkGlyphs(d, "editor")
			scr := screen(d)
			for _, want := range []string{"中文 " + wideDev + " " + wideFlag + " caf" + wideMark} {
				if !strings.Contains(scr, want) {
					t.Errorf("editor does not show %q whole:\n%s", want, scr)
				}
			}
		})
	}
}
