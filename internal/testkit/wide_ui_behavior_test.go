package testkit

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/activity"
)

// wideSizes are the window sizes the SCN-11 screens are checked at: the
// smallest common one, a narrow one where most names are cut, and wide.
var wideSizes = [][2]int{{80, 24}, {60, 20}, {120, 34}, {200, 50}}

// Spec (SCN-11): Given a workspace whose file and folder names hold CJK,
// an emoji ZWJ sequence, a flag, a combining accent and Persian mixed
// with English,
// when the explorer lists them at several window sizes (also with the
// sidebar so narrow that every name is cut with "…"),
// then every row is exactly the window width and no glyph is cut in half.
func TestWideNamesInExplorer(t *testing.T) {
	for _, sz := range wideSizes {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			d := New(t, Options{W: sz[0], H: sz[1], Files: wideFiles()})
			d.Action("view:files")
			clickExplorer(d, "中文目录")
			if !d.WaitForText("日本語", 2*time.Second) {
				t.Fatalf("folder did not open:\n%s", screen(d))
			}
			checkGlyphs(d, "explorer")
			for _, name := range []string{"emoji-", "فا", "caf"} {
				if !strings.Contains(screen(d), name) {
					t.Errorf("explorer does not show %q:\n%s", name, screen(d))
				}
			}
			// Narrow the sidebar step by step: names get cut with "…".
			for x := sidebarW; x > activity.Width+8; x -= 3 {
				d.Drag(x, d.H/2, x-3, d.H/2)
				checkGlyphs(d, fmt.Sprintf("sidebar edge at %d", x-3))
			}
			// Hover every row: tooltips with the full name.
			for y := 1; y < d.H-1 && !d.Failed(); y++ {
				d.Move(activity.Width+3, y)
				checkGlyphs(d, fmt.Sprintf("hover row %d", y))
			}
		})
	}
}

// Spec (SCN-11): a toast whose title and detail hold wide glyphs, long
// enough to be cut, keeps every row at the window width and cuts no
// glyph in half.
func TestWideToastText(t *testing.T) {
	long := strings.Repeat(wideMixed+" ", 6)
	for _, sz := range wideSizes {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			d := New(t, Options{W: sz[0], H: sz[1], Files: wideFiles()})
			d.Action("toast:" + long + "|" + long)
			if !d.WaitForText("中文", time.Second) {
				t.Fatalf("toast not shown:\n%s", screen(d))
			}
			checkGlyphs(d, "toast")
			d.Action("toast:" + wideDev + wideFlag + "|" + wideFa)
			checkGlyphs(d, "two toasts")
		})
	}
}

// Spec (SCN-11): Given files with wide names,
// when the user opens Find Files and types a CJK filter, then an emoji
// one,
// then the matching file is listed, nothing else, and every row of the
// list (cut with "…" when narrow) keeps the window width.
func TestWidePaletteFilter(t *testing.T) {
	for _, sz := range wideSizes {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			d := New(t, Options{W: sz[0], H: sz[1], Files: wideFiles()})
			d.Palette("find-files-overlay")
			d.Type("日本語")
			if !d.WaitForText("ファイル", 2*time.Second) {
				t.Fatalf("CJK filter does not find the file:\n%s", screen(d))
			}
			checkGlyphs(d, "find files 日本語")
			if strings.Contains(screen(d), "a.txt") {
				t.Errorf("CJK filter still lists a.txt:\n%s", screen(d))
			}
			for range []rune("日本語") {
				d.Key("backspace")
			}
			d.Type(wideDev)
			if !d.WaitForText("notes.txt", 2*time.Second) {
				t.Fatalf("emoji filter does not find the file:\n%s", screen(d))
			}
			checkGlyphs(d, "find files emoji")
			d.Key("esc")
			// The command palette with a wide filter that matches nothing.
			d.Key("f1")
			d.Type(wideMixed)
			checkGlyphs(d, "command palette")
			d.Key("esc")
		})
	}
}

// Spec (SCN-11): Given commits whose subjects hold CJK, emoji (ZWJ,
// flags), combining accents and Persian,
// when Source Control shows the GRAPH and the user hovers each commit,
// then every row keeps the window width and no glyph is cut.
func TestWideCommitMessagesInGraph(t *testing.T) {
	subjects := []string{
		"feat: 中文提交信息 with a long tail that is wider than the sidebar",
		"fix: " + wideDev + " pair work " + wideFlag + " flag " + wideFlag + wideFlag,
		"docs: caf" + wideMark + " naïve " + strings.Repeat("e\u0301", 30),
		"chore: " + wideFa + " و English با 42 عدد",
	}
	for _, sz := range wideSizes {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			d := New(t, Options{W: sz[0], H: sz[1], Git: true,
				Setup: func(t testing.TB, dir string) {
					r := &repo{t: t, dir: dir}
					for _, s := range subjects {
						r.git("commit", "-q", "--allow-empty", "-m", s)
					}
				}})
			d.Action("view:git")
			if !d.WaitForText("pair work", 3*time.Second) {
				t.Fatalf("graph does not list the commits:\n%s", screen(d))
			}
			checkGlyphs(d, "graph")
			for y := 0; y < d.H && !d.Failed(); y++ {
				d.Move(activity.Width+4, y)
				d.Settle(30 * time.Millisecond) // the commit detail loads async
				checkGlyphs(d, fmt.Sprintf("hover row %d", y))
			}
			// Open the newest commit's view.
			scmClickText(d, "pair work")
			d.Settle(300 * time.Millisecond)
			checkGlyphs(d, "commit view")
		})
	}
}
