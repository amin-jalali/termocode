package tabbar

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"termocode/internal/nvim"
)

func lipglossWidth(s string) int { return lipgloss.Width(s) }

func twoBufs() []nvim.BufferInfo {
	return []nvim.BufferInfo{
		{ID: 1, Path: "/tmp/foo.go"},
		{ID: 2, Path: "/tmp/bar.go", Modified: true},
	}
}

func TestNewIsEmpty(t *testing.T) {
	m := New()
	if m.Width() != 0 {
		t.Fatalf("width want 0, got %d", m.Width())
	}
	if m.Height() != 1 {
		t.Fatalf("height want 1, got %d", m.Height())
	}
}

func TestViewEmptyAtZeroWidth(t *testing.T) {
	m := New()
	m.SetBuffers(twoBufs(), 1)
	if v := m.View(); v != "" {
		t.Fatalf("zero width view should be empty, got %q", v)
	}
}

func TestViewWithoutBuffersFillsWidth(t *testing.T) {
	m := New()
	m.SetWidth(20)
	out := m.View()
	if out == "" {
		t.Fatal("empty-bufs view should still emit a styled fill")
	}
}

func TestViewIncludesBufferNames(t *testing.T) {
	m := New()
	m.SetBuffers(twoBufs(), 2)
	m.SetWidth(80)
	out := m.View()
	if !strings.Contains(out, "foo.go") || !strings.Contains(out, "bar.go") {
		t.Fatalf("buffer names missing in view: %s", out)
	}
}

func TestRebuildRegionsTracksTabs(t *testing.T) {
	m := New()
	m.SetBuffers(twoBufs(), 1)
	m.SetWidth(80)
	m.rebuildRegions()
	if len(m.regions) != 2 {
		t.Fatalf("regions: want 2, got %d (%+v)", len(m.regions), m.regions)
	}
	// First region must start at 0 and end before second region.
	if m.regions[0].start != 0 {
		t.Fatalf("first region must start at 0, got %d", m.regions[0].start)
	}
	if m.regions[1].start <= m.regions[0].end-1 {
		t.Fatalf("regions overlap: %+v", m.regions)
	}
	// closeX is end-2 by construction.
	for _, r := range m.regions {
		if r.closeX != r.end-2 {
			t.Fatalf("closeX want end-2, got region=%+v", r)
		}
	}
}

func TestHandleMouseLeftClickInBodyEmitsSwitch(t *testing.T) {
	m := New()
	m.SetBuffers(twoBufs(), 1)
	m.SetWidth(80)
	m.rebuildRegions()
	// Click in the middle of the second tab body.
	r := m.regions[1]
	bodyX := r.start + 1 // not the close glyph
	if bodyX == r.closeX {
		bodyX = r.start
	}
	_, cmd := m.HandleMouse(bodyX, 100, 0, tea.MouseLeft)
	if cmd == nil {
		t.Fatal("expected a SwitchMsg cmd")
	}
	msg := cmd()
	sw, ok := msg.(SwitchMsg)
	if !ok {
		t.Fatalf("want SwitchMsg, got %T", msg)
	}
	if sw.ID != 2 {
		t.Fatalf("switch ID want 2, got %d", sw.ID)
	}
}

func TestHandleMouseClickOnCloseGlyphEmitsClose(t *testing.T) {
	m := New()
	m.SetBuffers(twoBufs(), 1)
	m.SetWidth(80)
	m.rebuildRegions()
	r := m.regions[0]
	_, cmd := m.HandleMouse(r.closeX, 0, 0, tea.MouseLeft)
	if cmd == nil {
		t.Fatal("close click expected to emit cmd")
	}
	msg, ok := cmd().(CloseMsg)
	if !ok {
		t.Fatalf("want CloseMsg, got %T", cmd())
	}
	if msg.ID != 1 {
		t.Fatalf("close ID want 1, got %d", msg.ID)
	}
}

func TestHandleMouseRightClickEmitsContext(t *testing.T) {
	m := New()
	m.SetBuffers(twoBufs(), 1)
	m.SetWidth(80)
	m.rebuildRegions()
	r := m.regions[0]
	bodyX := r.start + 2
	if bodyX >= r.end || bodyX == r.closeX {
		bodyX = r.start
	}
	_, cmd := m.HandleMouse(bodyX, 50, 4, tea.MouseRight)
	if cmd == nil {
		t.Fatal("right click expected to emit cmd")
	}
	tc, ok := cmd().(TabContextMsg)
	if !ok {
		t.Fatalf("want TabContextMsg, got %T", cmd())
	}
	if tc.ID != 1 || tc.X != 50 || tc.Y != 4 {
		t.Fatalf("ctx payload mismatch: %+v", tc)
	}
}

func TestHandleMouseOutsideRegionIsNoop(t *testing.T) {
	m := New()
	m.SetBuffers(twoBufs(), 1)
	m.SetWidth(80)
	_, cmd := m.HandleMouse(78, 0, 0, tea.MouseLeft)
	if cmd != nil {
		t.Fatalf("click outside any region should not emit cmd")
	}
}

func TestHandleMouseIgnoresOtherButtons(t *testing.T) {
	m := New()
	m.SetBuffers(twoBufs(), 1)
	m.SetWidth(80)
	_, cmd := m.HandleMouse(2, 0, 0, tea.MouseMiddle)
	if cmd != nil {
		t.Fatalf("middle button should be ignored, got cmd")
	}
}

// manyBufs returns enough buffers to overflow any reasonable width.
func manyBufs() []nvim.BufferInfo {
	bufs := make([]nvim.BufferInfo, 0, 12)
	names := []string{
		"alpha.go", "beta.go", "gamma.go", "delta.go",
		"epsilon.go", "zeta.go", "eta.go", "theta.go",
		"iota.go", "kappa.go", "lambda.go", "mu.go",
	}
	for i, n := range names {
		bufs = append(bufs, nvim.BufferInfo{ID: i + 1, Path: "/tmp/" + n})
	}
	return bufs
}

func TestOverflowIndicatorsAppearWhenContentWiderThanW(t *testing.T) {
	m := New()
	m.SetWidth(30)
	m.SetBuffers(manyBufs(), 1)
	out := m.View()
	// First tab is active and visible → no left indicator yet, but
	// far-right tabs are clipped → right indicator must appear.
	if !strings.Contains(out, "▶") {
		t.Fatalf("expected right overflow indicator in view: %q", out)
	}
	if strings.Contains(out, "◀") {
		t.Fatalf("did not expect left overflow indicator yet: %q", out)
	}
}

func TestOverflowIndicatorsBothWhenScrolledMidway(t *testing.T) {
	m := New()
	m.SetWidth(30)
	m.SetBuffers(manyBufs(), 1)
	// Scroll a wheel-down to push past the leftmost tab.
	for i := 0; i < 5; i++ {
		m.HandleMouseInPlace(15, 0, 0, tea.MouseWheelDown)
	}
	out := m.View()
	if !strings.Contains(out, "◀") {
		t.Fatalf("expected left indicator after scrolling: %q", out)
	}
}

func TestEnsureVisibleScrollsToOffscreenActive(t *testing.T) {
	m := New()
	m.SetWidth(30)
	bufs := manyBufs()
	m.SetBuffers(bufs, bufs[len(bufs)-1].ID)
	if m.ScrollX() == 0 {
		t.Fatalf("expected scrollX > 0 after activating last tab in narrow view, got 0")
	}
	out := m.View()
	if !strings.Contains(out, "mu.go") {
		t.Fatalf("active tab name should be visible after EnsureVisible: %q", out)
	}
}

func TestWheelDownIncreasesScrollX(t *testing.T) {
	m := New()
	m.SetWidth(30)
	m.SetBuffers(manyBufs(), 1)
	before := m.ScrollX()
	m, _ = m.HandleMouse(10, 0, 0, tea.MouseWheelDown)
	if m.ScrollX() <= before {
		t.Fatalf("wheel-down should increase scrollX (was %d, now %d)", before, m.ScrollX())
	}
}

func TestWheelUpDecreasesScrollX(t *testing.T) {
	m := New()
	m.SetWidth(30)
	m.SetBuffers(manyBufs(), 1)
	// Scroll right first, then test wheel up.
	m, _ = m.HandleMouse(10, 0, 0, tea.MouseWheelDown)
	m, _ = m.HandleMouse(10, 0, 0, tea.MouseWheelDown)
	mid := m.ScrollX()
	if mid == 0 {
		t.Fatalf("setup: wheel-down didn't advance scrollX")
	}
	m, _ = m.HandleMouse(10, 0, 0, tea.MouseWheelUp)
	if m.ScrollX() >= mid {
		t.Fatalf("wheel-up should decrease scrollX (was %d, now %d)", mid, m.ScrollX())
	}
}

func TestLeftIndicatorClickPagesLeft(t *testing.T) {
	m := New()
	m.SetWidth(30)
	m.SetBuffers(manyBufs(), 1)
	// Scroll right so a left indicator is shown.
	for i := 0; i < 10; i++ {
		m, _ = m.HandleMouse(10, 0, 0, tea.MouseWheelDown)
	}
	before := m.ScrollX()
	if before == 0 {
		t.Fatalf("setup: failed to advance scrollX")
	}
	// Left indicator sits at x=0 once leftOverflow is true.
	m2, _ := m.HandleMouse(0, 0, 0, tea.MouseLeft)
	if m2.ScrollX() >= before {
		t.Fatalf("click on ◀ should reduce scrollX (was %d, now %d)", before, m2.ScrollX())
	}
}

func TestRightIndicatorClickPagesRight(t *testing.T) {
	m := New()
	m.SetWidth(30)
	m.SetBuffers(manyBufs(), 1)
	before := m.ScrollX()
	// Right indicator is at x=m.w-1 = 29.
	m2, _ := m.HandleMouse(29, 0, 0, tea.MouseLeft)
	if m2.ScrollX() <= before {
		t.Fatalf("click on ▶ should increase scrollX (was %d, now %d)", before, m2.ScrollX())
	}
}

func TestViewWidthExactlyEqualsW(t *testing.T) {
	cases := []int{30, 50, 80, 12}
	for _, w := range cases {
		m := New()
		m.SetWidth(w)
		m.SetBuffers(manyBufs(), 1)
		out := m.View()
		got := lipglossWidth(out)
		if got != w {
			t.Fatalf("width=%d: rendered width=%d, want %d", w, got, w)
		}
	}
}

// HandleMouseInPlace is a helper that updates m.scrollX in place via the
// returned model so tests can chain wheel events.
func (m *Model) HandleMouseInPlace(x, sx, sy int, t tea.MouseEventType) {
	*m, _ = m.HandleMouse(x, sx, sy, t)
}
