package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/theme"
)

var svcTestNow = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

// svcTestState is the plan's mock: postgres healthy, redis running, api
// exited 1, mailhog not created; local web running.
func svcTestState() *servicesState {
	st := newServicesState()
	st.opened, st.loaded = true, true
	st.root = "/w"
	st.det = services.Detection{ComposeFiles: []string{"compose.yaml"}, Compose: true}
	st.project = "termocode"
	port := func(p int) []services.Port { return []services.Port{{Published: p, Target: p, Proto: "tcp"}} }
	st.compose = []services.Service{
		{Name: "postgres", Backend: "compose", Image: "postgres:16", Ports: port(5432), HasHealthcheck: true},
		{Name: "redis", Backend: "compose", Image: "redis:7", Ports: port(6379)},
		{Name: "api", Backend: "compose", Ports: port(8080), DependsOn: []string{"postgres"}},
		{Name: "mailhog", Backend: "compose", Image: "mailhog/mailhog", Ports: port(8025)},
	}
	st.status = map[string]services.Status{
		"postgres": {Name: "postgres", State: services.Healthy, Health: "healthy", Text: "Up 2 hours (healthy)", Image: "postgres:16",
			StartedAt: svcTestNow.Add(-(2*time.Hour + 14*time.Minute)), Ports: []services.Port{{Host: "0.0.0.0", Published: 5432, Target: 5432, Proto: "tcp"}}},
		"redis": {Name: "redis", State: services.Running, Text: "Up 2 hours", StartedAt: svcTestNow.Add(-2 * time.Hour),
			Ports: port(6379)},
		"api": {Name: "api", State: services.Exited, ExitCode: 1, Text: "Exited (1) 5 minutes ago"},
	}
	st.local = []services.Service{{Name: "web", Backend: "local", Command: "npm run dev", Ports: port(5173), Health: services.Probe{TCP: 5173}, Source: "Procfile"}}
	st.localSt = map[string]services.Status{"web": {Name: "web", State: services.Running, StartedAt: svcTestNow.Add(-62 * time.Minute)}}
	// The Services tests look at the PROJECT part only (the old view);
	// containers_view_test.go turns the host sections on.
	for _, sec := range hostSectionOrder {
		st.sections[sec] = false
	}
	return st
}

func svcRowsText(st *servicesState) string {
	var out []string
	for _, r := range st.rows() {
		switch r.kind {
		case svcRowSection:
			out = append(out, "#"+r.section)
		case svcRowService:
			out = append(out, r.name)
		default:
			out = append(out, "!"+r.text)
		}
	}
	return strings.Join(out, ",")
}

func TestServicesRows(t *testing.T) {
	st := svcTestState()
	if got := svcRowsText(st); got != "#compose,postgres,redis,api,mailhog,#local,web" {
		t.Errorf("rows = %s", got)
	}
	st.collapsed[svcSecCompose] = true
	if got := svcRowsText(st); got != "#compose,#local,web" {
		t.Errorf("folded rows = %s", got)
	}
	st.collapsed[svcSecCompose] = false
	st.problem = services.Problem{Kind: services.ProblemDaemonDown}
	if got := svcRowsText(st); !strings.HasPrefix(got, "#compose,!Docker is not running,!Retry,postgres") {
		t.Errorf("daemon down rows = %s", got)
	}
	if s := st.statusOf(svcSecCompose, "postgres"); s.State != services.Unknown {
		t.Errorf("rows keep the last snapshot but read unknown: %v", s.State)
	}
	st.problem = services.Problem{Kind: services.ProblemNoPermission}
	if got := svcRowsText(st); !strings.Contains(got, "!"+svcPermissionFix) {
		t.Errorf("permission rows = %s", got)
	}
	// services.json broken: a note in LOCAL.
	st.problem = services.Problem{}
	st.det.ConfigErr = errors.New(".termocode/services.json: bad")
	if got := svcRowsText(st); !strings.Contains(got, "#local,!.termocode/services.json: bad") {
		t.Errorf("config error rows = %s", got)
	}
}

func TestServicesCursorSkipsNotes(t *testing.T) {
	st := svcTestState()
	st.problem = services.Problem{Kind: services.ProblemOther, Detail: "boom"} // note + hint (the Retry row is selectable)
	m := Model{services: st, h: 40}
	st.cursor = 0
	m.servicesMove(1)
	if r, _ := m.servicesCurrentRow(); r.kind != svcRowService || r.name != "postgres" {
		t.Errorf("j from the header must skip the notes: %+v", r)
	}
	m.servicesMove(-1)
	if r, _ := m.servicesCurrentRow(); r.kind != svcRowSection {
		t.Errorf("k back to the header: %+v", r)
	}
	st.cursor = 1 // a note
	m.servicesClampCursor()
	if r, _ := m.servicesCurrentRow(); !r.selectable() {
		t.Errorf("clamp lands on %+v", r)
	}
}

// The width ladder (plan §2.3): every row is exactly w cells; uptime
// goes below 28, the port below 22.
func TestServiceRowWidths(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st := svcTestState()
	pg, _ := st.findService(svcSecCompose, "postgres")
	api, _ := st.findService(svcSecCompose, "api")
	for _, w := range []int{20, 24, 29, 40} {
		for _, svc := range []services.Service{pg, api} {
			row := renderServiceRow(svc, st.statusOf(svcSecCompose, svc.Name), false, false, w, svcTestNow)
			plain := ansi.Strip(row)
			if got := ansi.StringWidth(row); got != w {
				t.Errorf("w=%d %s: %d cells: %q", w, svc.Name, got, plain)
			}
			hasPort, hasUp := strings.Contains(plain, ":5432") || strings.Contains(plain, ":8080"),
				strings.Contains(plain, "2h 14m") || strings.Contains(plain, "exit 1")
			if hasPort != (w >= 22) || hasUp != (w >= 28) {
				t.Errorf("w=%d %s: port=%v uptime=%v in %q", w, svc.Name, hasPort, hasUp, plain)
			}
		}
	}
	row := ansi.Strip(renderServiceRow(pg, st.statusOf(svcSecCompose, "postgres"), true, true, 29, svcTestNow))
	if row != "▌◉ postgres     :5432  2h 14m" {
		t.Errorf("29-wide cursor row = %q", row)
	}
	long := services.Service{Name: "a-very-long-service-name", Backend: "compose"}
	if got := ansi.Strip(renderServiceRow(long, services.Status{State: services.NotCreated}, false, false, 29, svcTestNow)); !strings.Contains(got, "a-very…-name ") {
		t.Errorf("middle cut: %q", got)
	}
}

// Every state of plan §2.7: glyph, colour, right column, footer line 2.
func TestServiceStateLook(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeASCII
	open, closed := true, false
	svc := services.Service{Name: "db", Backend: "compose", Image: "postgres:16", Ports: []services.Port{{Published: 5432, Target: 5432}}}
	hc := svc
	hc.HasHealthcheck = true
	up := svcTestNow.Add(-2 * time.Hour)
	cases := []struct {
		name        string
		svc         services.Service
		st          services.Status
		glyph       string
		col         theme.Color256
		right, foot string
	}{
		{"not created", svc, services.Status{State: services.NotCreated}, "o", theme.TextDim, "", "not created · s start"},
		{"starting", svc, services.Status{State: services.Starting}, "~", theme.GitModified, "", "starting…"},
		{"pulling", svc, services.Status{State: services.Pulling}, "~", theme.GitModified, "pull", "pulling postgres:16 — see the services: up tab"},
		{"port open", svc, services.Status{State: services.Running, Text: "Up 2 hours", StartedAt: up, PortOpen: &open, Ports: svc.Ports}, "*", theme.GitAdded, "2h", "Up 2 hours · port 5432 open"},
		{"port closed", svc, services.Status{State: services.Running, Text: "Up 2 hours", StartedAt: up, PortOpen: &closed, Ports: svc.Ports}, "*", theme.GitModified, "2h", "Up 2 hours · port 5432 not open"},
		{"healthy", hc, services.Status{State: services.Healthy, Text: "Up 2 hours (healthy)", StartedAt: up}, "*", theme.GitAdded, "2h", "Up 2 hours (healthy)"},
		{"health starting", hc, services.Status{State: services.Starting, Health: "starting", Text: "Up 12 seconds (health: starting)", StartedAt: svcTestNow.Add(-12 * time.Second)}, "~", theme.GitModified, "12s", "Up 12 seconds (health: starting)"},
		{"unhealthy", hc, services.Status{State: services.Unhealthy, Text: "Up 2 hours (unhealthy)", StartedAt: up}, "!", theme.DiagError, "2h", "Up 2 hours (unhealthy) · l logs"},
		{"restarting", svc, services.Status{State: services.Restarting, Text: "Restarting (1) 3 seconds ago"}, "r", theme.GitModified, "—", "Restarting (1) 3 seconds ago"},
		{"exited 0", svc, services.Status{State: services.Stopped, Text: "Exited (0) 5 minutes ago"}, "o", theme.TextDim, "done", "Exited (0) 5 minutes ago"},
		{"exited 1", svc, services.Status{State: services.Exited, ExitCode: 1, Text: "Exited (1) 5 minutes ago"}, "x", theme.DiagError, "exit 1", "Exited (1) 5 minutes ago · l logs · r restart"},
		{"stopping", svc, services.Status{State: services.Stopping}, "~", theme.GitModified, "stop", "stopping…"},
		{"daemon down", svc, services.Status{State: services.Unknown}, "?", theme.TextDim, "", "Docker is off · R retry"},
	}
	for _, c := range cases {
		g, col := svcGlyph(c.st)
		right, _ := svcUptime(c.st, svcTestNow)
		if g.String() != c.glyph || col != c.col || right != c.right {
			t.Errorf("%s: glyph %q col %v right %q; want %q %v %q", c.name, g.String(), col, right, c.glyph, c.col, c.right)
		}
		if got := svcStatusLine(c.svc, c.st); got != c.foot {
			t.Errorf("%s: footer %q, want %q", c.name, got, c.foot)
		}
	}
}

func svcTestModel(st *servicesState, w, h int) Model {
	m := Model{services: st, w: w, h: h, explorerWidth: 30, showExp: true, hoverX: -1, hoverY: -1}
	m.activity.SetActive(activity.ViewServices)
	return m
}

func TestRenderServicesSidebar(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st := svcTestState()
	st.cursor = 1
	m := svcTestModel(st, 100, 30)
	out := m.renderServicesSidebarRaw(29, 30, true, svcTestNow, -1, -1)
	lines := strings.Split(out, "\n")
	if len(lines) != 30 {
		t.Fatalf("%d lines", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 29 {
			t.Errorf("line %d is %d wide: %q", i, w, ansi.Strip(l))
		}
	}
	plain := ansi.Strip(out)
	for _, want := range []string{"CONTAINERS", "compose · termocode", "3/5 up", "▸ Start all", "▪ Stop all", "↻",
		"PROJECT", "LOCAL", "postgres", "exit 1", "postgres:16 · :5432→5432", "Up 2 hours (healthy)"} {
		if !strings.Contains(plain, want) {
			t.Errorf("no %q in\n%s", want, plain)
		}
	}
	// Wide enough, the footer keeps the host.
	wide := ansi.Strip(m.renderServicesSidebarRaw(40, 30, true, svcTestNow, -1, -1))
	if !strings.Contains(wide, "postgres:16 · 0.0.0.0:5432→5432") {
		t.Errorf("wide footer:\n%s", wide)
	}
}

func TestRenderServicesEmptyAndLoading(t *testing.T) {
	st := newServicesState()
	m := svcTestModel(st, 100, 30)
	if got := ansi.Strip(m.renderServicesSidebarRaw(29, 30, false, svcTestNow, -1, -1)); !strings.Contains(got, "Looking for services") {
		t.Errorf("loading:\n%s", got)
	}
	st.loaded = true
	got := ansi.Strip(m.renderServicesSidebarRaw(29, 30, false, svcTestNow, -1, -1))
	for _, want := range []string{"No services", "compose.yaml", "o"} {
		if !strings.Contains(got, want) {
			t.Errorf("empty state has no %q:\n%s", want, got)
		}
	}
}

// CT-1: the empty-state button says what it does, not only the key.
func TestServicesEmptyButtonIsLabelled(t *testing.T) {
	st := newServicesState()
	st.loaded = true
	st.containerDetected = true // no engine: the whole view is the card
	m := svcTestModel(st, 100, 30)
	got := ansi.Strip(m.renderServicesSidebarRaw(29, 30, false, svcTestNow, -1, -1))
	if !strings.Contains(got, "Create services.json") {
		t.Errorf("button has no label:\n%s", got)
	}
}

// The render cache: same seq, cursor, hover and size → the same string.
func TestServicesRenderCache(t *testing.T) {
	st := svcTestState()
	m := svcTestModel(st, 100, 30)
	a := m.renderServicesSidebar(29, 30)
	b := m.renderServicesSidebar(29, 30)
	if a != b || st.render.key == "" {
		t.Fatal("no cache hit")
	}
	st.touch()
	st.status["redis"] = services.Status{Name: "redis", State: services.Exited, ExitCode: 2, Text: "Exited (2)"}
	if c := m.renderServicesSidebar(29, 30); c == a || !strings.Contains(ansi.Strip(c), "exit 2") {
		t.Error("a new seq must redraw")
	}
}

func TestSvcActionLayout(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeASCII
	acts := svcActionLayout(29)
	if len(acts) != 3 || acts[0].label != "> Start all" || acts[1].label != "# Stop all" || acts[2].id != "refresh" || acts[2].start != 27 {
		t.Errorf("layout = %+v", acts)
	}
	if acts := svcActionLayout(16); len(acts) != 2 || acts[0].id != "start-all" || acts[1].id != "refresh" {
		t.Errorf("narrow layout = %+v", acts)
	}
}

// The action bar labels and the service rows are hit targets (the hit-map
// tests and the hover labels see them).
func TestServicesHitTargets(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st := svcTestState()
	m := svcTestModel(st, 100, 30)
	got := map[string]string{}
	for _, h := range m.sidebarTargets() {
		got[h.ID] = h.Label
	}
	for id, label := range map[string]string{"svc:btn:start-all": "▸ Start all", "svc:btn:stop-all": "▪ Stop all",
		"svc:btn:refresh": "↻", "svc:row:compose/postgres": "", "svc:row:local/web": ""} {
		l, ok := got[id]
		if !ok {
			t.Errorf("no target %s in %v", id, got)
			continue
		}
		if label != "" && l != label {
			t.Errorf("%s label %q, want %q", id, l, label)
		}
	}
	if id := m.sidebarTargetAt(2, svcActionRow); id != "svc:btn:start-all" {
		t.Errorf("target at the start button = %q", id)
	}
	for _, id := range []string{"svc:btn:start-all", "svc:btn:stop-all", "svc:btn:refresh"} {
		if m.tooltipText(id) == "" {
			t.Errorf("%s has no hover label", id)
		}
	}
}

// Regression: the services confirm kinds shared 980 with the setup card,
// so "Run" on the first-start card was handled as the setup question.
func TestServicesConfirmKindsUnique(t *testing.T) {
	seen := map[confirmKindEnum]string{confirmKindSetupProfile: "setup", confirmKindAISaveOverwrite: "ai-save",
		confirmKindAITool: "ai-tool", confirmKindMCPTrust: "mcp", confirmKindAIHistoryDelete: "ai-history"}
	for k, name := range map[confirmKindEnum]string{confirmKindServicesLocal: "local", confirmKindServicesDown: "down", confirmKindServicesQuit: "quit"} {
		if other, ok := seen[k]; ok {
			t.Errorf("services %s uses %d, taken by %s", name, k, other)
		}
		seen[k] = name
	}
}

// CT-2: with the engine down there is one banner (in PROJECT) with a
// Retry button; the other sections stay quiet and Start all / Stop all
// are greyed out and do nothing.
func TestServicesDockerDownOneBanner(t *testing.T) {
	st := svcHostState()
	st.problem = services.Problem{Kind: services.ProblemDaemonDown}
	st.containerEngine.Reachable = false
	st.containerEngine.Problem = st.problem
	for _, sec := range hostSectionOrder {
		st.collapsed[hostSection(sec)] = false
	}
	all := svcRowsText(st)
	if n := strings.Count(all, "Docker is not running"); n != 1 {
		t.Errorf("banner appears %d times: %s", n, all)
	}
	if n := strings.Count(all, "Retry"); n != 1 {
		t.Errorf("Retry appears %d times: %s", n, all)
	}
	for _, a := range st.actionLayout(29) {
		if (a.id == "start-all" || a.id == "stop-all") != a.disabled {
			t.Errorf("%s disabled = %v", a.id, a.disabled)
		}
	}
	m := svcTestModel(st, 100, 30)
	for _, h := range m.servicesHits(29) {
		if h.ID == "svc:btn:start-all" || h.ID == "svc:btn:stop-all" {
			t.Errorf("disabled button %s is clickable", h.ID)
		}
	}
	// Enter on the Retry row runs the refresh.
	for i, r := range st.rows() {
		if r.kind == svcRowRetry {
			st.cursor = i
			break
		}
	}
	if r, _ := m.servicesCurrentRow(); r.kind != svcRowRetry {
		t.Fatalf("cursor is not on the Retry row: %+v", r)
	}
	_, cmd := m.handleServicesSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Error("Enter on Retry did nothing")
	}
	// Up again: Start all works.
	st.problem = services.Problem{}
	for _, a := range st.actionLayout(29) {
		if a.disabled {
			t.Errorf("%s greyed out with a working engine", a.id)
		}
	}
}
