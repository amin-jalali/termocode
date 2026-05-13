package activity

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/theme"
)

func TestNewDefaultsToFiles(t *testing.T) {
	m := New()
	if m.Active() != ViewFiles {
		t.Fatalf("default active view want ViewFiles, got %v", m.Active())
	}
}

func TestSetActiveAndHeight(t *testing.T) {
	m := New()
	m.SetActive(ViewGit)
	if m.Active() != ViewGit {
		t.Fatalf("SetActive failed")
	}
	m.SetHeight(40)
	if m.h != 40 {
		t.Fatalf("SetHeight failed: %d", m.h)
	}
}

func TestUpdateIsNoop(t *testing.T) {
	m := New()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("Update should not produce cmds")
	}
}

func TestHandleMouseClickOnIconRow(t *testing.T) {
	m := New()
	m.SetHeight(40)
	// First item (Files, active by default) — its 2-row icon block sits
	// at y=0,1. Click the icon's top row.
	_, cmd := m.HandleMouse(1, 1, tea.MouseLeft)
	if cmd == nil {
		t.Fatal("click on active icon must emit ToggleSidebar")
	}
	if _, ok := cmd().(ToggleSidebarMsg); !ok {
		t.Fatalf("want ToggleSidebarMsg (clicked active item), got %T", cmd())
	}
}

func TestHandleMouseClickOnInactiveSwitches(t *testing.T) {
	m := New() // ViewFiles active
	m.SetHeight(40)
	// Stride is itemPitch+interItemDivider = 3. Git is the second top item
	// (Search was removed), so its block sits at y=3,4 (after the 1-row
	// hairline at y=2).
	_, cmd := m.HandleMouse(1, 3, tea.MouseLeft)
	if cmd == nil {
		t.Fatal("click on inactive icon must emit SwitchMsg")
	}
	sw, ok := cmd().(SwitchMsg)
	if !ok {
		t.Fatalf("want SwitchMsg, got %T", cmd())
	}
	if sw.View != ViewGit {
		t.Fatalf("switch target want ViewGit, got %v", sw.View)
	}
}

func TestHandleMouseClickOnHairlineIsNoop(t *testing.T) {
	m := New()
	m.SetHeight(40)
	// y=2 is the hairline between Files (y=0,1) and Git (y=3,4).
	// Clicks on the hairline are intentionally non-clickable.
	if _, cmd := m.HandleMouse(1, 2, tea.MouseLeft); cmd != nil {
		t.Fatal("click on inter-item hairline must be a no-op")
	}
}

func TestHandleMouseOutOfBoundsIsNoop(t *testing.T) {
	m := New()
	m.SetHeight(40)
	// x outside Width.
	if _, cmd := m.HandleMouse(99, 1, tea.MouseLeft); cmd != nil {
		t.Fatal("x out of range should be a no-op")
	}
	// y below 0.
	if _, cmd := m.HandleMouse(0, -1, tea.MouseLeft); cmd != nil {
		t.Fatal("y<0 should be a no-op")
	}
	// y above height.
	if _, cmd := m.HandleMouse(0, 100, tea.MouseLeft); cmd != nil {
		t.Fatal("y >= h should be a no-op")
	}
}

func TestHandleMouseIgnoresNonLeftButtons(t *testing.T) {
	m := New()
	m.SetHeight(40)
	if _, cmd := m.HandleMouse(0, 1, tea.MouseRight); cmd != nil {
		t.Fatal("non-left clicks must be ignored")
	}
}

func TestHandleMouseClickInGapBetweenGroups(t *testing.T) {
	m := New()
	m.SetHeight(40)
	// Find a row that classifies as rowEmpty between top and bottom groups.
	bs := m.bottomGroupStart()
	if bs <= topGroupRows() {
		t.Skip("layout has no gap row at this height")
	}
	gapY := topGroupRows() // first row past top group
	if _, cmd := m.HandleMouse(0, gapY, tea.MouseLeft); cmd != nil {
		t.Fatalf("click in empty gap should be a no-op (y=%d, bs=%d)", gapY, bs)
	}
}

func TestViewEmptyAtZeroHeight(t *testing.T) {
	m := New()
	if v := m.View(); v != "" {
		t.Fatalf("zero height view want empty, got %q", v)
	}
}

func TestViewProducesHeightLines(t *testing.T) {
	m := New()
	m.SetHeight(40)
	out := m.View()
	gotLines := strings.Count(out, "\n") + 1
	if gotLines != 40 {
		t.Fatalf("view rows want 40, got %d", gotLines)
	}
}

// TestViewRendersBoldArtwork is a smoke test that the activity bar emits
// the active icon's Bold[0] glyph in row 0 and Bold[1] in row 1. ANSI
// styling is stripped so we can match plain runes.
func TestViewRendersBoldArtwork(t *testing.T) {
	m := New() // ViewFiles active by default
	m.SetHeight(40)
	out := m.View()
	rows := strings.Split(out, "\n")
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 rows, got %d", len(rows))
	}
	row0 := stripANSI(rows[0])
	row1 := stripANSI(rows[1])
	bold := theme.IconFiles.BoldRender()
	if !strings.Contains(row0, bold[0]) {
		t.Fatalf("row 0 missing Bold[0] %q; got %q", bold[0], row0)
	}
	if !strings.Contains(row1, bold[1]) {
		t.Fatalf("row 1 missing Bold[1] %q; got %q", bold[1], row1)
	}
}

// TestViewRendersBoldForEachItem walks every top-group item and confirms
// its Bold[0]/Bold[1] glyphs show up in the rows reserved for that item.
// Stride = itemPitch + interItemDivider so item N starts at idx*stride.
func TestViewRendersBoldForEachItem(t *testing.T) {
	for idx, it := range topItems {
		m := New()
		m.SetActive(it.view)
		m.SetHeight(40)
		out := m.View()
		rows := strings.Split(out, "\n")
		topRow := idx * itemStride
		botRow := topRow + 1
		if botRow >= len(rows) {
			t.Fatalf("not enough rendered rows for item %d", idx)
		}
		bold := it.icon.BoldRender()
		if got := stripANSI(rows[topRow]); !strings.Contains(got, bold[0]) {
			t.Errorf("item %d (%s) row %d: missing Bold[0] %q; got %q",
				idx, it.label, topRow, bold[0], got)
		}
		if got := stripANSI(rows[botRow]); !strings.Contains(got, bold[1]) {
			t.Errorf("item %d (%s) row %d: missing Bold[1] %q; got %q",
				idx, it.label, botRow, bold[1], got)
		}
	}
}

// stripANSI removes CSI escape sequences from s so plain-rune assertions
// don't trip on the colour escapes lipgloss interleaves with the glyphs.
func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) {
				c := s[j]
				if c >= 0x40 && c <= 0x7e && c != '[' {
					j++
					break
				}
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
