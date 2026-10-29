package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/services"
)

// covSvcClick clicks the sidebar row that match finds.
func covSvcClick(t *testing.T, m *Model, typ tea.MouseEventType, match func(svcRow) bool) (svcRow, tea.Cmd) {
	t.Helper()
	m.renderServicesSidebarRaw(m.explorerWidth-1, m.h, true, svcTestNow, -1, -1)
	rows := m.services.rows()
	for y := 0; y < m.h; y++ {
		if i := m.servicesRowAt(y); i >= 0 && match(rows[i]) {
			next, cmd := m.handleServicesSidebarMouse(3, y, typ)
			*m = next
			return rows[i], cmd
		}
	}
	t.Fatalf("no visible row:\n%s", svcRowsText(m.services))
	return svcRow{}, nil
}

// Clicks: right-click opens the menu of the row kind; a double click
// opens logs or uses a context; the wheel scrolls; a click on a header
// folds it.
func TestCovSvcSidebarMouse(t *testing.T) {
	m, _ := covSvcHost(t)
	m.explorerWidth = 50
	for _, sec := range []string{"images", "networks", "volumes", "help", "registries"} {
		m.services.collapsed[hostSection(sec)] = true
	}
	for _, c := range []struct {
		name  string
		match func(svcRow) bool
		want  string
	}{
		{"service", func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" }, "Recreate"},
		{"container", func(r svcRow) bool { return r.host == hostContainer && r.hostID == "c1" }, "Logs"},
		{"context", func(r svcRow) bool { return r.host == hostContext && r.hostID == "prod" }, "Use in this session"},
		{"section", func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("containers") }, "Prune stopped containers"},
	} {
		m.menuOpen = false
		covSvcClick(t, m, tea.MouseRight, c.match)
		if !m.menuOpen || !strings.Contains(covSvcMenuText(m), c.want) {
			t.Errorf("%s menu = %s", c.name, covSvcMenuText(m))
		}
	}
	m.menuOpen = false
	// The REGISTRIES header menu: connect, browse, search.
	m.services.collapsed[hostSection("registries")] = false
	reg := selectRow(t, m.services, func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("registries") })
	m.openSectionMenu(reg, 3, 3)
	for id, kind := range map[string]promptKindEnum{"browse": promptKindContainerBrowse, "search": promptKindContainerSearch} {
		m.promptOpen = false
		m.handleServicesMenuSelect(id)
		if !m.promptOpen || m.promptKind != kind {
			t.Errorf("%s: prompt %v kind %d", id, m.promptOpen, m.promptKind)
		}
	}
	m.promptOpen = false
	m.handleServicesMenuSelect("connect")
	m.promptOpen = false
	m.openSectionMenu(svcRow{kind: svcRowSection, section: svcSecCompose}, 3, 3)
	m.services.collapsed[hostSection("registries")] = true
	m.menuOpen = false
	// Double click on a service: logs (no editor: a toast).
	covSvcClick(t, m, tea.MouseLeft, func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" })
	covSvcClick(t, m, tea.MouseLeft, func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" })
	if !strings.Contains(regToast(m), "Cannot show logs") {
		t.Errorf("double click toast = %q", regToast(m))
	}
	// Double click on a context uses it.
	covSvcClick(t, m, tea.MouseLeft, func(r svcRow) bool { return r.host == hostContext && r.hostID == "prod" })
	_, cmd := covSvcClick(t, m, tea.MouseLeft, func(r svcRow) bool { return r.host == hostContext && r.hostID == "prod" })
	if cmd == nil || m.services.sessionContext != "prod" {
		t.Errorf("double click context: %q", m.services.sessionContext)
	}
	m.services.sessionContext = ""
	// A click on a folded header opens it.
	covSvcClick(t, m, tea.MouseLeft, func(r svcRow) bool { return r.kind == svcRowSection && r.section == hostSection("images") })
	if m.services.collapsed[hostSection("images")] {
		t.Error("click did not open IMAGES")
	}
	// The wheel moves the cursor; a click on nothing does nothing.
	before := m.services.cursor
	next, _ := m.handleServicesSidebarMouse(3, 10, tea.MouseWheelDown)
	*m = next
	if m.services.cursor == before {
		t.Error("wheel down did not move")
	}
	next, _ = m.handleServicesSidebarMouse(3, 10, tea.MouseWheelUp)
	*m = next
	if next, cmd := m.handleServicesSidebarMouse(3, 10, tea.MouseMotion); cmd != nil {
		t.Error("motion did something")
	} else {
		*m = next
	}
	if next, cmd := m.handleServicesSidebarMouse(3, m.h-1, tea.MouseLeft); cmd != nil {
		t.Error("a click below the list did something")
	} else {
		*m = next
	}
	// The ⋯ button opens the sections menu.
	next, _ = m.handleServicesSidebarMouse(m.explorerWidth-2, 0, tea.MouseLeft)
	*m = next
	if !m.menuOpen || m.menuKind != menuKindContainers {
		t.Error("⋯ did not open the sections menu")
	}
}

// The action row: Start all runs, and with Docker off it says so.
func TestCovSvcActionRowClicks(t *testing.T) {
	m, _ := covSvcProject(t)
	m.explorerWidth = 50
	m.renderServicesSidebarRaw(49, m.h, true, svcTestNow, -1, -1)
	layout := m.services.actionLayout(49)
	if len(layout) == 0 {
		t.Fatal("no actions")
	}
	a := layout[0]
	next, _ := m.handleServicesSidebarMouse(a.start, svcActionRow, tea.MouseLeft)
	*m = next
	if !strings.Contains(regToast(m), "Cannot start services") {
		t.Errorf("start all toast = %q", regToast(m))
	}
	next, cmd := m.handleServicesSidebarMouse(48, svcActionRow, tea.MouseLeft)
	*m = next
	if cmd != nil {
		t.Error("a click between actions did something")
	}
	m.services.problem.Kind = services.ProblemDaemonDown
	for _, b := range m.services.actionLayout(49) {
		if b.disabled {
			next, _ = m.handleServicesSidebarMouse(b.start, svcActionRow, tea.MouseLeft)
			*m = next
			if !strings.Contains(regToast(m), "Docker is off") {
				t.Errorf("disabled toast = %q", regToast(m))
			}
			break
		}
	}
	// The empty card: a click below the summary opens services.json.
	m.services.det.ComposeFiles, m.services.det.Compose = nil, false
	m.services.compose, m.services.local = nil, nil
	m.services.sections = map[string]bool{"containers": false, "images": false, "registries": false, "networks": false, "volumes": false, "contexts": false, "help": false}
	if !m.services.showEmptyCard() {
		t.Skip("no empty card in this state")
	}
	next, _ = m.handleServicesSidebarMouse(3, svcSummaryRow+2, tea.MouseLeft)
	*m = next
	next, cmd = m.handleServicesSidebarMouse(3, svcSummaryRow+2, tea.MouseRight)
	*m = next
	if cmd != nil {
		t.Error("right click on the empty card did something")
	}
}

// Keys on the PROJECT part that depend on the state.
func TestCovSvcProjectKeysByState(t *testing.T) {
	m, f := covSvcProject(t)
	st := m.services
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()
	// a / A with Docker off.
	st.problem.Kind = services.ProblemDaemonDown
	for _, k := range []string{"a", "A"} {
		press(m, k)
		if !strings.Contains(regToast(m), "Docker is off") {
			t.Errorf("%s toast = %q", k, regToast(m))
		}
	}
	st.problem.Kind = services.ProblemNone
	runServicesCmds(t, m, press(m, "A"))
	selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowService && r.name == "redis" })
	runServicesCmds(t, m, press(m, "x"))
	if !strings.Contains(covSvcCalls(f), "stop redis") {
		t.Errorf("x calls = %q", f.Calls())
	}
	selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowService && r.name == "postgres" })
	runServicesCmds(t, m, press(m, "r"))
	selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowService && r.name == "mailhog" })
	press(m, "s")
	press(m, "e")
	press(m, "y")
	selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowSection && r.section == svcSecCompose })
	press(m, "enter")
	press(m, "o")
	// y with no Docker permission copies the fix.
	st.problem.Kind = services.ProblemNoPermission
	selectRow(t, st, func(r svcRow) bool { return r.kind == svcRowSection && r.section == svcSecCompose })
	press(m, "y")
	if len(clip) == 0 || clip[len(clip)-1] != svcPermissionFix {
		t.Errorf("clip = %q (problem kind %d)", clip, st.problem.Kind)
	}
}
