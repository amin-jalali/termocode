package app

import (
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/tabbar"
)

// A hit map for tests: every click target the mouse code knows about in
// the current state, with the text the view draws inside it. A test
// checks that the text really is drawn there and that a click on the
// target reaches it (probeClick). Both use the same layout helpers as
// the view and the mouse code, so any drift between them shows up.

// hitTarget is one click target.
type hitTarget struct {
	// ID names the target; probeClick reports the same name when a click
	// resolves to it ("activity:Explorer", "tab:3", "status:lsp", …).
	ID string
	// Label is text drawn inside the rect ("" when there is nothing to
	// check, e.g. a splitter).
	Label      string
	X, Y, W, H int
}

// probed records id as the target a probed click resolved to. It
// reports true on the probe copy only; the caller must then stop before
// it changes anything.
func (m Model) probed(id string) bool {
	if m.mouseProbe == nil {
		return false
	}
	*m.mouseProbe = id
	return true
}

// probeClick runs a left press at (x, y) through the mouse routing on a
// copy of the model and returns the id of the target it resolved to
// ("" when nothing took it). It changes nothing.
func (m Model) probeClick(x, y int) string {
	var got string
	m.mouseProbe = &got
	if m.imageOpen {
		return "overlay:image"
	}
	m.updateInner(tea.MouseMsg{X: x, Y: y, Type: tea.MouseLeft,
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	return got
}

// cellText is the plain text of render's row in cells [x0, x1), trimmed.
func cellText(render string, row, x0, x1 int) string {
	lines := strings.Split(render, "\n")
	if row < 0 || row >= len(lines) || x1 <= x0 {
		return ""
	}
	return strings.TrimSpace(ansi.Strip(ansi.Cut(lines[row], x0, x1)))
}

// hitTargets lists the click targets of the current state.
func (m Model) hitTargets() []hitTarget {
	if m.w <= 0 || m.h <= 0 || m.imageOpen || m.previewOpen || m.searchOpen {
		return nil
	}
	switch {
	case m.promptOpen, m.settingsModalOpen, m.recentsOpen, m.menuOpen:
		return nil // not mapped yet
	case m.confirmOpen:
		return m.confirmTargets()
	case m.pickerOpen:
		return m.pickerTargets()
	}
	var out []hitTarget
	add := func(t hitTarget) {
		if t.W > 0 && t.H > 0 {
			out = append(out, t)
		}
	}
	pane := m.editorPaneLayout()
	if !m.zenMode {
		out = append(out, m.activityTargets()...)
		if m.showExp {
			x := activity.Width + m.explorerWidth - 1
			add(hitTarget{ID: "sidebar:splitter", Label: cellText(sidebarDivider(1), 0, 0, 1), X: x, Y: 0, W: 1, H: m.h})
			out = append(out, m.sidebarTargets()...)
		}
		out = append(out, m.tabTargets(pane)...)
		if gx := m.overflowGlyphX(); gx >= 0 {
			add(hitTarget{ID: "overflow:glyph", Label: overflowMenuGlyph, X: gx, Y: m.overflowGlyphY(), W: 1, H: 1})
		}
		out = append(out, m.statusTargets()...)
		out = append(out, m.actionsTargets()...)
	}
	if m.editorEmpty() {
		out = append(out, m.welcomeTargets(pane)...)
	}
	if m.termOpen && !m.editorEmpty() {
		out = append(out, m.panelBarTargets(pane)...)
	}
	if m.gitDiffActive && m.nvim != nil && diffNavShown(pane.W) {
		// The change-nav buttons on the diff winbar (" ‹ " and " › ").
		add(hitTarget{ID: "diff:prev", Label: "‹", X: pane.X, Y: pane.Top, W: 3, H: 1})
		add(hitTarget{ID: "diff:next", Label: "›", X: pane.X + 3, Y: pane.Top, W: 3, H: 1})
	}
	return out
}

func (m Model) confirmTargets() []hitTarget {
	var out []hitTarget
	for _, b := range m.confirm.ButtonRects() {
		out = append(out, hitTarget{ID: "confirm:" + b.ID, Label: strings.TrimSpace(ansi.Strip(b.Label)),
			X: b.X, Y: b.Y, W: b.W, H: 1})
	}
	return out
}

func (m Model) pickerTargets() []hitTarget {
	box := m.picker.Box()
	bw, bh := m.picker.BoxSize()
	bx, by := m.picker.Origin(bw, bh)
	var out []hitTarget
	for _, r := range m.picker.RowRects() {
		out = append(out, hitTarget{ID: "picker:row:" + strconv.Itoa(r.Index),
			Label: cellText(box, r.Y-by, r.X-bx, r.X-bx+r.W), X: r.X, Y: r.Y, W: r.W, H: 1})
	}
	return out
}

func (m Model) activityTargets() []hitTarget {
	view := m.activity.View()
	var out []hitTarget
	for _, it := range m.activity.Items() {
		label := ""
		for y := it.Y; y < it.Y+it.H && label == ""; y++ {
			label = cellText(view, y, 0, activity.Width)
		}
		out = append(out, hitTarget{ID: "activity:" + it.Label, Label: label,
			X: 0, Y: it.Y, W: activity.Width, H: it.H})
	}
	return out
}

// sidebarTargets maps the rows of the Explorer and Source Control views.
func (m Model) sidebarTargets() []hitTarget {
	x0 := activity.Width
	w := m.explorerWidth - 1
	var out []hitTarget
	switch m.activity.Active() {
	case activity.ViewFiles:
		for y := 0; y < m.h; y++ {
			if n, ok := m.explorer.VisibleAt(y); ok {
				out = append(out, hitTarget{ID: explorerTargetID(n.Path), Label: n.Name, X: x0, Y: y, W: w, H: 1})
			}
		}
	case activity.ViewGit:
		if !m.gitLoaded || !m.gitIsRepo {
			return nil
		}
		view := m.renderGitSidebar(w, m.h)
		for y := 0; y < m.h; y++ {
			for _, h := range m.gitSidebarHits(y, w) {
				out = append(out, hitTarget{ID: h.id, Label: cellText(view, y, h.x0, h.x1),
					X: x0 + h.x0, Y: y, W: h.x1 - h.x0, H: 1})
			}
		}
	}
	return out
}

func (m Model) tabTargets(pane editorPane) []hitTarget {
	y := m.tabBarRow()
	view := m.tabs.View()
	var out []hitTarget
	for _, tg := range m.tabs.Targets() {
		out = append(out, hitTarget{ID: tabTargetID(tg), Label: cellText(view, 0, tg.X, tg.X+tg.W),
			X: pane.X + tg.X, Y: y, W: tg.W, H: m.tabs.Height()})
	}
	return out
}

// tabTargetID names a tab-bar target.
func tabTargetID(tg tabbar.Target) string {
	switch tg.Kind {
	case tabbar.TargetTab, tabbar.TargetClose:
		return tg.Kind + ":" + strconv.Itoa(tg.ID)
	}
	return "tabs:" + tg.Kind
}

// statusTargets maps the status-bar chips (the same spans the click
// tests use, see statusHitAt).
func (m Model) statusTargets() []hitTarget {
	bx, bw := m.statusBarRect()
	sb := m.status
	sb.SetWidth(bw)
	view := sb.View(m.statusState())
	var out []hitTarget
	for _, s := range m.statusSpans() {
		out = append(out, hitTarget{ID: s.id, Label: cellText(view, 0, s.x0, s.x1),
			X: bx + s.x0, Y: m.h - 1, W: s.x1 - s.x0, H: 1})
	}
	return out
}

func (m Model) actionsTargets() []hitTarget {
	var out []hitTarget
	if x1, x2, y, ok := m.actionsLauncherRect(); ok {
		out = append(out, hitTarget{ID: "actions:launcher", Label: actionsLauncherLabel, X: x1, Y: y, W: x2 - x1, H: 1})
	}
	if !m.actionsPanelVisible() {
		return out
	}
	x1, x2, y, _ := m.actionsHeaderRect()
	header := m.renderActionsHeader(x2 - x1)
	for x := x1; x < x2; x++ {
		if id := m.hitActionsButton(x, y); id != "" {
			// New chat and History belong to the chat panel (aiPanelTargetAt).
			if id == "new" || id == "history" {
				id = "ai:" + id
			} else {
				id = "actions:" + id
			}
			out = append(out, hitTarget{ID: id, Label: cellText(header, 0, x-x1, x-x1+1), X: x, Y: y, W: 1, H: 1})
		}
	}
	// The splitter is the panel's left column below the header row (a
	// press on row 0 there is the header's).
	out = append(out, hitTarget{ID: "actions:splitter", X: x1, Y: 1, W: 1, H: m.h - 1})
	return out
}

func (m Model) welcomeTargets(pane editorPane) []hitTarget {
	view := renderWelcome(pane.W, pane.H, m.welcomeFocus)
	var out []hitTarget
	add := func(id string, row, c0, c1 int) {
		if row < 0 || row >= pane.H {
			return // the page is taller than the pane: clipped, not drawn
		}
		if c1 <= 0 {
			c0, c1 = 0, pane.W // full-row hit
		}
		// The page may be wider than the pane: it is clipped there.
		c1 = min(c1, pane.W)
		if c0 >= c1 {
			return
		}
		out = append(out, hitTarget{ID: id, Label: cellText(view, row, c0, c1),
			X: pane.X + c0, Y: pane.Top + row, W: c1 - c0, H: 1})
	}
	for _, h := range welcomeQuickActionHits(pane.W, pane.H) {
		add("welcome:card:"+strconv.Itoa(h.Index), h.Row, h.ColStart, h.ColEnd)
	}
	for _, h := range welcomeRecentHits(pane.W, pane.H) {
		add("welcome:recent:"+h.Path, h.Row, h.ColStart, h.ColEnd)
	}
	for _, h := range welcomeWorkspaceHits(pane.W, pane.H) {
		add("welcome:workspace:"+h.Path, h.Row, h.ColStart, h.ColEnd)
	}
	return out
}

func (m Model) panelBarTargets(pane editorPane) []hitTarget {
	y := m.terminalTabBarRowAbsolute()
	if y < 0 {
		return nil
	}
	bar := m.renderTerminalTabBar(pane.W)
	var out []hitTarget
	for x := 0; x < pane.W; x++ {
		hit, idx := m.terminalTabBarHitTest(pane.X+x, y)
		id := panelBarTargetID(hit, idx)
		if id == "" || id == "panel:drag" {
			continue
		}
		// Merge runs of cells with the same target into one rect.
		if n := len(out); n > 0 && out[n-1].ID == id && out[n-1].X+out[n-1].W == pane.X+x {
			out[n-1].W++
			out[n-1].Label = cellText(bar, 0, out[n-1].X-pane.X, x+1)
			continue
		}
		out = append(out, hitTarget{ID: id, Label: cellText(bar, 0, x, x+1), X: pane.X + x, Y: y, W: 1, H: 1})
	}
	return out
}

// panelBarTargetID names a bottom-panel tab-bar hit.
func panelBarTargetID(hit terminalTabBarHit, idx int) string {
	n := strconv.Itoa(idx)
	switch hit {
	case terminalHitTabClose:
		return "panel:term-close:" + n
	case terminalHitTabActivate:
		return "panel:term:" + n
	case terminalHitNewTab:
		return "panel:new"
	case terminalHitMaximize:
		return "panel:maximize"
	case terminalHitMinimize:
		return "panel:minimize"
	case terminalHitClosePanel:
		return "panel:close"
	case terminalHitDrag:
		return "panel:drag"
	case panelHitTabClose:
		return "panel:tab-close:" + n
	case panelHitTabActivate:
		return "panel:tab:" + n
	}
	return ""
}

// explorerTargetID names an explorer row.
func explorerTargetID(path string) string { return "explorer:" + filepath.Clean(path) }

// panelContentAt reports whether (x, y) is on the content of a
// non-terminal bottom-panel tab (routePanelContentMouse takes it).
func (m Model) panelContentAt(x, y int) bool {
	px, py, pw, ph, ok := m.panelContentRect()
	return ok && x >= px && x < px+pw && y >= py && y < py+ph
}

// sidebarTargetAt names what a click at sidebar-local x (0 = first
// sidebar column) and screen row y hits.
func (m Model) sidebarTargetAt(x, y int) string {
	switch m.activity.Active() {
	case activity.ViewFiles:
		if n, ok := m.explorer.VisibleAt(y); ok {
			return explorerTargetID(n.Path)
		}
	case activity.ViewGit:
		if h, ok := m.gitSidebarHitAt(x, y); ok {
			return h.id
		}
	}
	return "sidebar"
}
