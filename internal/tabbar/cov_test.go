package tabbar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/theme"
)

func covBufs(n int) []nvim.BufferInfo {
	var bs []nvim.BufferInfo
	for i := 1; i <= n; i++ {
		bs = append(bs, nvim.BufferInfo{ID: i, Path: "/tmp/dir/file_number_" + string(rune('a'+i%26)) + ".go"})
	}
	return bs
}

func TestBuffersReturnsACopy(t *testing.T) {
	m := New()
	m.SetBuffers(twoBufs(), 2)
	got, active := m.Buffers()
	if len(got) != 2 || active != 2 {
		t.Fatalf("%v %d", got, active)
	}
	got[0].ID = 99
	if again, _ := m.Buffers(); again[0].ID == 99 {
		t.Fatal("Buffers leaked the inner slice")
	}
}

func TestBadgeLetters(t *testing.T) {
	for in, want := range map[string]string{
		"": "", "I": "", "!!": "", "??": "U", "M": "M", " a ": "A", "MM": "M",
	} {
		if got := badgeLetter(in); got != want {
			t.Errorf("badgeLetter(%q) = %q, want %q", in, got, want)
		}
	}
	m := New()
	m.SetGitStatus(map[string]string{"/a": "??"})
	if m.gitBadge(nvim.BufferInfo{Path: "/a"}) != "U" || m.gitBadge(nvim.BufferInfo{}) != "" {
		t.Fatal("gitBadge")
	}
}

// With no tabs or no width there is nothing to lay out, scroll or click.
func TestEmptyStripEdges(t *testing.T) {
	m := New()
	if st := m.layoutFrom(0, nil); st.last != -1 {
		t.Fatalf("strip %+v", st)
	}
	m.scrollByTabs(2)
	m.rebuildRegions()
	if len(m.regions) != 0 || m.Targets() != nil {
		t.Fatal("regions on an empty bar")
	}
	m.SetBuffers(covBufs(3), 1)
	m.rebuildRegions() // still no width
	if len(m.regions) != 0 || m.Targets() != nil {
		t.Fatal("regions without a width")
	}
	m.SetWidth(0)
}

// An overflowing bar scrolls by tabs, stays inside its tabs and never
// starts past the last one.
func TestOverflowScrollBounds(t *testing.T) {
	m := New()
	m.SetBuffers(covBufs(12), 1)
	m.SetWidth(40)
	m.scrollByTabs(-5)
	if m.first != 0 {
		t.Fatalf("first = %d", m.first)
	}
	m.scrollByTabs(100)
	if m.first >= 12 || m.first <= 0 {
		t.Fatalf("first = %d", m.first)
	}
	m.first = 50 // a stale position from a longer list
	m.EnsureVisible(3)
	if m.first >= 12 {
		t.Fatalf("first after EnsureVisible = %d", m.first)
	}
	st := m.layoutFrom(-3, m.tabWidths())
	if st.first != 0 {
		t.Fatalf("negative start: %+v", st)
	}
	st = m.layoutFrom(99, m.tabWidths())
	if st.first != 11 {
		t.Fatalf("start past the end: %+v", st)
	}
}

// A first tab wider than the bar is shown shortened, with room for the
// ▶ only when tabs follow; a one-cell bar still shows one cell.
func TestFirstTabWiderThanBar(t *testing.T) {
	m := New()
	m.SetBuffers(covBufs(3), 1)
	m.SetWidth(10)
	m.rebuildRegions()
	if len(m.regions) == 0 {
		t.Fatal("no region for the shortened tab")
	}
	if out := m.View(); ansi.StringWidth(out) != 10 {
		t.Fatalf("view width %d", ansi.StringWidth(out))
	}
	m.SetWidth(1)
	m.rebuildRegions()
	if st, _ := m.layout(); st.fitW < 1 {
		t.Fatalf("fitW %d", st.fitW)
	}
}

// Dead helper kept for callers: renderTab joins the cells.
func TestRenderTabAndExtensionIcon(t *testing.T) {
	s, n := renderTab(nvim.BufferInfo{ID: 1, Path: "/x/a.go"}, true)
	if n == 0 || !strings.Contains(ansi.Strip(s), "a.go") {
		t.Fatalf("%q %d", s, n)
	}
	if New().extensionIcon("a.go") != "" {
		t.Fatal("extension icon slot is blank")
	}
}

// A buffer with no path still draws; a squeezed tab keeps at least one
// cell of the name.
func TestUnnamedTabAndSqueeze(t *testing.T) {
	if cells := renderTabCellsSfx(nvim.BufferInfo{ID: 1}, false, "", "", false, 0); len(cells) == 0 {
		t.Fatal("a buffer with no path drew no tab")
	}
	tiny := renderTabCellsSfx(nvim.BufferInfo{ID: 1, Path: "/x/longname.go"}, false, "", "", false, 3)
	if len(tiny) == 0 {
		t.Fatal("no cells")
	}
}

// The welcome tab has a home icon in Nerd and Unicode modes.
func TestWelcomeTabIcons(t *testing.T) {
	old := theme.CurrentIconMode
	t.Cleanup(func() { theme.CurrentIconMode = old })
	b := nvim.BufferInfo{ID: 1, Path: "/x/welcome"}
	theme.CurrentIconMode = theme.IconModeNerdFont
	nerd := strings.Join(renderTabCellsSfx(b, true, "", "", true, 0), "")
	theme.CurrentIconMode = theme.IconModeUnicode
	uni := strings.Join(renderTabCellsSfx(b, true, "", "", true, 0), "")
	if !strings.Contains(nerd, "") || !strings.Contains(uni, "⌂") {
		t.Fatalf("nerd %q uni %q", nerd, uni)
	}
}

// A mark with no letter before it joins the cell before; with no cell
// before it, it still gets its own cell.
func TestAppendGlyphCellsCombiningMarks(t *testing.T) {
	st := theme.Bg(theme.BgEditor)
	cells := appendGlyphCells([]string{"x"}, "́", st)
	if len(cells) != 1 {
		t.Fatalf("mark made a cell: %d", len(cells))
	}
	cells = appendGlyphCells(nil, "́", st)
	if len(cells) != 1 {
		t.Fatalf("lonely mark: %d cells", len(cells))
	}
}
