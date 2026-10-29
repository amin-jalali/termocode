package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/theme"
)

// The mouse code reuses the welcome hit areas recorded by the last render
// (welcomeLayoutHits). These tests change every input of the layout in one
// running session, with no pause (the cache lives for a second), and check
// that hover and click still find the card the frame draws there.

// checkWelcomeHits checks the cached hit areas against a fresh layout and
// the frame on screen, then hovers and probes every card.
func checkWelcomeHits(t *testing.T, m Model, stage string) Model {
	t.Helper()
	// Ask for the hits before View: the mouse event comes first, on the
	// render cached before the change.
	pane := m.editorPaneLayout()
	got := welcomeLayoutHits(pane.W, pane.H)
	frame := m.View()
	_, rh, wh, qh := renderWelcomeFull(pane.W, pane.H, m.welcomeFocus)
	if !reflect.DeepEqual(got.actions, qh) || !reflect.DeepEqual(got.recents, rh) || !reflect.DeepEqual(got.workspaces, wh) {
		t.Fatalf("%s: stale welcome hits\ncached %+v\nfresh  %+v", stage, got, welcomeHits{recents: rh, workspaces: wh, actions: qh})
	}
	actions := welcomeQuickActions()
	titled := map[int]bool{} // cards whose title row is in the hit map
	for _, h := range qh {
		x, y := pane.X+h.ColStart+2, pane.Top+h.Row
		// A tile is several lines tall: its title is on one of them.
		for k := 0; k < 4; k++ {
			if label := cellText(frame, y+k, pane.X+h.ColStart, pane.X+h.ColEnd); strings.Contains(label, actions[h.Index].title) {
				titled[h.Index] = true
			}
		}
		m = hoverAt(m, x, y)
		if m.welcomeFocus != h.Index {
			t.Fatalf("%s: hover on card %d (%q) lit card %d", stage, h.Index, actions[h.Index].title, m.welcomeFocus)
		}
		if id := m.probeClick(x, y); id != "welcome:card:"+itoa(h.Index) {
			t.Fatalf("%s: click on card %d (%q) reaches %q", stage, h.Index, actions[h.Index].title, id)
		}
	}
	if len(titled) != len(actions) {
		t.Fatalf("%s: only cards %v show their title inside their hit area\n%s", stage, titled, frame)
	}
	m.View() // leave a warm cache (current focus) for the next change
	return m
}

func TestWelcomeHitsFollowLayoutChanges(t *testing.T) {
	dir := withConfigDir(t)
	oldIcons := theme.CurrentIconMode
	t.Cleanup(func() {
		theme.CurrentIconMode = oldIcons
		theme.ApplyTheme("vscode-dark-plus")
		invalidateWelcome()
	})
	invalidateWelcome()

	m0, _ := newTestModel(t).Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	m := checkWelcomeHits(t, m0.(Model), "start")

	nm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 38})
	m = checkWelcomeHits(t, nm.(Model), "resize")

	nm, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 44})
	m = checkWelcomeHits(t, nm.(Model), "taller")

	nm, _ = m.Update(tea.WindowSizeMsg{Width: 90, Height: 60}) // stacked layout
	m = checkWelcomeHits(t, nm.(Model), "narrow")

	nm, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 45})
	m = checkWelcomeHits(t, nm.(Model), "wide again")
	m.actionsOpen, m.actionsPinned = true, true
	m.actionsWidth, m.actionsWidthSet = 40, true
	m.applyLayout()
	m = checkWelcomeHits(t, m, "AI panel open")

	// Drag with no frame in between: the hover right after the drag
	// still sees the render from before it.
	m = startDrag(m)
	edge := m.w - m.actionsColumnWidth()
	for i := 1; i <= 15; i++ {
		nm, _ = m.Update(tea.MouseMsg{X: edge - i, Y: 10, Type: tea.MouseMotion, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
		m = nm.(Model)
	}
	nm, _ = m.Update(tea.MouseMsg{X: edge - 15, Y: 10, Type: tea.MouseRelease, Action: tea.MouseActionRelease})
	m = checkWelcomeHits(t, nm.(Model), "AI panel dragged")

	// More recent files make the page taller, so the cards move up.
	for _, name := range []string{"a.go", "b.go", "c.go", "d.go", "e.go"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		pushRecent(p)
	}
	m = checkWelcomeHits(t, m, "recents changed")
	pushWorkspace(dir)
	m = checkWelcomeHits(t, m, "workspaces changed")

	m.applyTheme("one-dark")
	m = checkWelcomeHits(t, m, "theme switched")

	theme.CurrentIconMode = otherIconMode(theme.CurrentIconMode)
	checkWelcomeHits(t, m, "icon mode switched")
}

// otherIconMode returns an icon mode different from mode.
func otherIconMode(mode theme.IconMode) theme.IconMode {
	if mode == theme.IconModeUnicode {
		return theme.IconModeNerdFont
	}
	return theme.IconModeUnicode
}

// The card focus only recolors the page: every focus has the same hit
// areas, which is why welcomeLayoutHits may reuse any focus's render.
func TestWelcomeFocusKeepsLayout(t *testing.T) {
	withConfigDir(t)
	for _, size := range [][2]int{{160, 45}, {90, 60}, {60, 30}} {
		_, r0, w0, q0 := renderWelcomeFull(size[0], size[1], 0)
		for f := 1; f < len(welcomeQuickActions()); f++ {
			_, r, w, q := renderWelcomeFull(size[0], size[1], f)
			if !reflect.DeepEqual(q, q0) || !reflect.DeepEqual(r, r0) || !reflect.DeepEqual(w, w0) {
				t.Fatalf("%v: focus %d moves the hit areas", size, f)
			}
		}
	}
}
