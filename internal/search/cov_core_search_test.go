package search

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The Find in Files overlay fits the screen at every size: its box, its
// summary line (the long "install rg" hint, an error) and its key hints
// used to push the box past the screen's right and bottom edges. (Below
// 35 columns the app cuts its whole frame, internal/app fitTinyScreen.)
func TestCovCoreOverlayFitsScreen(t *testing.T) {
	for _, rg := range []bool{false, true} {
		for _, errText := range []string{"", strings.Repeat("rg: bad regex ", 8)} {
			for w := 35; w <= 130; w += 3 {
				for _, h := range []int{8, 12, 20, 40} {
					m := New()
					m.rgAvailable = rg
					m.lastErr = errText
					m.SetSize(w, h)
					lines := strings.Split(m.View(), "\n")
					if len(lines) > h {
						t.Errorf("rg=%v err=%v %dx%d: %d rows", rg, errText != "", w, h, len(lines))
					}
					for i, l := range lines {
						if lw := ansi.StringWidth(l); lw > w {
							t.Errorf("rg=%v err=%v %dx%d: row %d is %d wide", rg, errText != "", w, h, i, lw)
							break
						}
					}
				}
			}
		}
	}
}
