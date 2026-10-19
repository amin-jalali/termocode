package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
	"github.com/amin-jalali/termocode/internal/theme"
)

// svcHostState is svcTestState with the host sections on and a docker
// host loaded: the termocode project (2 of 3 up), a shop project and two
// standalone containers.
func svcHostState() *servicesState {
	st := svcTestState()
	st.sections = map[string]bool{}
	st.visible = true
	st.containerDetected = true
	st.containerEngine = containers.Engine{Binary: "docker", Kind: "docker", Reachable: true, Version: containers.Version{Version: "27.3.1"}}
	lbl := func(project, service string) map[string]string {
		return map[string]string{"com.docker.compose.project": project, "com.docker.compose.service": service, "com.docker.compose.container-number": "1"}
	}
	st.containerRows = []containers.Container{
		{ID: "c1", Names: []string{"termocode-postgres-1"}, Image: "postgres:16", State: "running", Status: "Up 2 hours (healthy)", Project: "termocode", Labels: lbl("termocode", "postgres"), PortsText: "0.0.0.0:5432->5432/tcp"},
		{ID: "c2", Names: []string{"termocode-redis-1"}, Image: "redis:7", State: "running", Status: "Up 2 hours", Project: "termocode", Labels: lbl("termocode", "redis")},
		{ID: "c3", Names: []string{"termocode-api-1"}, Image: "api", State: "exited", Status: "Exited (1) 3 minutes ago", Project: "termocode", Labels: lbl("termocode", "api")},
		{ID: "c4", Names: []string{"shop-web-1"}, Image: "shop", State: "running", Status: "Up 3 hours", Project: "shop", Labels: lbl("shop", "web")},
		{ID: "c5", Names: []string{"nginx"}, Image: "nginx:1.27", State: "running", Status: "Up 3 days", PortsText: "0.0.0.0:8081->80/tcp"},
		{ID: "c6", Names: []string{"builder"}, Image: "golang:1.23", State: "paused", Status: "Up 1 hour (Paused)"},
	}
	st.imageRows = []containers.Image{
		{ID: "sha256:aaa", RepoTags: []string{"postgres:16"}, Size: "421MB"},
		{ID: "sha256:bbb", RepoTags: []string{"postgres:15-alpine"}, Size: "239MB"},
		{ID: "sha256:ccc", RepoTags: []string{"redis:7"}, Size: "1.2GB"},
	}
	st.networkRows = []containers.Network{{Name: "bridge", Driver: "bridge", Scope: "local"}, {Name: "termocode_default", Driver: "bridge", Scope: "local"}}
	st.volumeRows = []containers.Volume{{Name: "termocode_db", Driver: "local"}}
	st.contextRows = []containers.Context{{Name: "default", Current: true}, {Name: "prod"}}
	for _, sec := range hostSectionOrder {
		st.hostLoaded[sec] = true
	}
	return st
}

func svcRender(st *servicesState, w, h int) []string {
	m := svcTestModel(st, 100, h)
	return strings.Split(ansi.Strip(m.renderServicesSidebarRaw(w, h, true, svcTestNow, -1, -1)), "\n")
}

func lineWith(lines []string, sub string) string {
	for _, l := range lines {
		if strings.Contains(l, sub) {
			return l
		}
	}
	return ""
}

// Tester item 2: the summary row and PROJECT keep the project's count
// (compose + local, 3/5 up), not the host's containers; PROJECT counts
// the compose services only (LOCAL is its own header, no double count).
func TestContainersSummaryKeepsProjectCount(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	lines := svcRender(svcHostState(), 29, 60)
	if got := lines[svcSummaryRow]; !strings.Contains(got, "compose · termocode") || !strings.HasSuffix(strings.TrimRight(got, " "), "3/5 up") {
		t.Errorf("summary row = %q, want the project's 3/5 up", got)
	}
	if got := strings.TrimRight(lineWith(lines, "PROJECT"), " "); !strings.HasSuffix(got, " 4") {
		t.Errorf("PROJECT header = %q, want the 4 compose services", got)
	}
	if got := strings.TrimRight(lineWith(lines, "LOCAL"), " "); !strings.HasSuffix(got, " 1") {
		t.Errorf("LOCAL header = %q", got)
	}
}

// Tester item 8: the engine line names the real active context.
func TestContainersEngineLineNamesActiveContext(t *testing.T) {
	st := svcHostState()
	st.contextRows = []containers.Context{{Name: "default"}, {Name: "prod", Current: true}}
	lines := svcRender(st, 29, 60)
	if lineWith(lines, "docker 27 · prod") == "" || lineWith(lines, "docker 27 · default") != "" {
		t.Errorf("engine line does not name the active context:\n%s", strings.Join(lines, "\n"))
	}
	st.contextRows = nil
	st.containerEngine.Kind = "podman"
	st.touch()
	if l := lineWith(svcRender(st, 29, 60), "podman 27"); l == "" || strings.Contains(l, "default") {
		t.Errorf("podman engine line = %q (no fixed context)", l)
	}
}

// Tester item 4: under a project group the row shows the service name
// (postgres-1) in full, not a cut "termocode-…".
func TestContainersServiceNamesUnderGroup(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	lines := svcRender(svcHostState(), 29, 60)
	for _, want := range []string{"postgres-1", "redis-1", "api-1", "nginx", "builder"} {
		if lineWith(lines, want) == "" {
			t.Errorf("no %q row:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	for _, l := range lines {
		if strings.Contains(l, "termocode-") {
			t.Errorf("row keeps the project prefix: %q", l)
		}
	}
	if l := lineWith(lines, "postgres-1"); !strings.Contains(l, ":5432") || !strings.Contains(l, "2h") {
		t.Errorf("postgres-1 row = %q, want port and uptime", l)
	}
	c := containers.Container{Names: []string{"proj_db_1"}}
	if got := containerDisplayName(c, "proj"); got != "db_1" {
		t.Errorf("no labels: %q", got)
	}
}

// Tester item 10: groups are typed rows (no parsing of display text): a
// project named "web 2" keeps its name; the group shows dots and up/total.
func TestContainersGroupRowsTyped(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st := svcHostState()
	st.containerRows = append(st.containerRows, containers.Container{ID: "c9", Names: []string{"x"}, State: "running", Project: "web 2"})
	var groups []svcRow
	for _, r := range st.rows() {
		if r.kind == svcRowGroup {
			groups = append(groups, r)
		}
	}
	if len(groups) < 4 || groups[0].name != "termocode" || !groups[0].current || groups[0].count != "2/3" {
		t.Fatalf("groups = %+v", groups)
	}
	found := false
	for _, g := range groups {
		if g.name == "web 2" && g.section == hostGroup("containers", "web 2") {
			found = true
		}
	}
	if !found {
		t.Errorf("group \"web 2\" lost its name: %+v", groups)
	}
	line := ansi.Strip(renderHostGroup(groups[0], false, false, false, 29))
	if !strings.Contains(line, "▾ termocode · ·") || !strings.HasSuffix(line, "2/3") {
		t.Errorf("group row = %q", line)
	}
}

// Tester item 10: rows are exactly w cells and the columns line up.
func TestContainersHostRowWidthsAndColumns(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st := svcHostState()
	for _, sec := range hostSectionOrder {
		st.collapsed[hostSection(sec)] = false
	}
	rows := st.rows()
	for _, w := range []int{18, 22, 29, 44, 60} {
		for _, r := range rows {
			var line string
			switch r.kind {
			case svcRowHostItem:
				line = st.renderHostItem(r, true, true, w)
			case svcRowGroup:
				line = renderHostGroup(r, false, true, true, w)
			default:
				continue
			}
			if got := ansi.StringWidth(line); got != w {
				t.Errorf("w=%d %q: %d cells", w, ansi.Strip(line), got)
			}
		}
	}
	ends := map[int]bool{}
	for _, r := range rows {
		if r.host == hostImage {
			line := ansi.Strip(st.renderHostItem(r, false, false, 29))
			ends[ansi.StringWidth(line[:strings.LastIndex(line, "B")])] = true
		}
	}
	if len(ends) != 1 {
		t.Errorf("image sizes do not line up: %v", ends)
	}
	starts := map[int]bool{}
	for _, r := range rows {
		if r.host == hostNetwork {
			line := ansi.Strip(st.renderHostItem(r, false, false, 29))
			starts[ansi.StringWidth(line[:strings.LastIndex(line, " bridge ")])] = true
		}
	}
	if len(starts) != 1 {
		t.Errorf("network drivers do not line up: %v", starts)
	}
}

// Tester item 5: problem and empty texts fit a normal sidebar (29): the
// "what to do" part is never cut off.
func TestContainersHelpTextsFit(t *testing.T) {
	const w = 29
	states := map[string]func(st *servicesState){
		"down": func(st *servicesState) {
			st.containerEngine.Reachable = false
			st.containerEngine.Problem = services.Problem{Kind: services.ProblemDaemonDown}
		},
		"no engine": func(st *servicesState) { st.containerEngine = containers.Engine{} },
		"permission": func(st *servicesState) {
			st.containerEngine.Reachable = false
			st.containerEngine.Problem = services.Problem{Kind: services.ProblemNoPermission}
		},
		"empty": func(st *servicesState) {
			st.containerRows, st.imageRows, st.networkRows, st.volumeRows, st.contextRows = nil, nil, nil, nil, nil
		},
		"empty project": func(st *servicesState) { st.det, st.compose, st.local = services.Detection{}, nil, nil },
	}
	for name, set := range states {
		st := svcHostState()
		for _, sec := range hostSectionOrder {
			st.collapsed[hostSection(sec)] = false
		}
		set(st)
		for _, r := range st.rows() {
			if r.kind != svcRowNote && r.kind != svcRowHint { // a code row is copied with y
				continue
			}
			line := ansi.Strip(renderServiceNote(r, w))
			if strings.Contains(line, "…") && r.text != "Loading…" {
				t.Errorf("%s: cut off: %q", name, line)
			}
		}
	}
}

// Tester item 6: REGISTRIES promises nothing that does not exist.
func TestContainersRegistriesEmptyText(t *testing.T) {
	st := svcHostState()
	st.collapsed[hostSection("registries")] = false
	got := svcRowsText(st)
	if !strings.Contains(got, "!Not signed in yet") || strings.Contains(got, "connect") {
		t.Errorf("registries rows = %s", got)
	}
}

// Tester item 10: no "?" on HELP or REGISTRIES; "?" only for engine
// sections when the engine does not answer.
func TestContainersHeaderCounts(t *testing.T) {
	st := svcHostState()
	if c := st.hostSectionCount("help"); c != "" {
		t.Errorf("HELP count = %q", c)
	}
	if c := st.hostSectionCount("registries"); c != "0" {
		t.Errorf("REGISTRIES count = %q", c)
	}
	if c := st.hostSectionCount("containers"); c != "6" {
		t.Errorf("CONTAINERS count = %q", c)
	}
	st.containerEngine.Reachable = false
	if st.hostSectionCount("containers") != "?" || st.hostSectionCount("registries") == "?" || st.hostSectionCount("help") != "" {
		t.Errorf("down counts: %q %q %q", st.hostSectionCount("containers"), st.hostSectionCount("registries"), st.hostSectionCount("help"))
	}
	st = svcHostState()
	st.hostLoaded["networks"] = false
	if c := st.hostSectionCount("networks"); c != "" {
		t.Errorf("folded, never loaded NETWORKS count = %q, want none", c)
	}
}

// Tester item 10: folded headers stack without blank rows; an open
// section gets one blank row before the next header.
func TestContainersFoldedHeadersStack(t *testing.T) {
	lines := svcRender(svcHostState(), 29, 60)
	at := func(sub string) int {
		for i, l := range lines {
			if strings.Contains(l, sub) {
				return i
			}
		}
		return -1
	}
	img, reg, net := at("IMAGES"), at("REGISTRIES"), at("NETWORKS")
	if img < 0 || reg != img+1 || net != reg+1 {
		t.Errorf("folded headers at %d %d %d:\n%s", img, reg, net, strings.Join(lines, "\n"))
	}
	if strings.TrimSpace(lines[img-1]) != "" {
		t.Errorf("no blank row after the open CONTAINERS section: %q", lines[img-1])
	}
}

// Tester item 7: an empty project with host sections shows the PROJECT
// help (o) and no dead Start all / Stop all; with nothing to show on the
// host it is the old "No services" card with key o.
func TestContainersEmptyProject(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	theme.CurrentIconMode = theme.IconModeUnicode
	st := svcHostState()
	st.det, st.compose, st.local = services.Detection{}, nil, nil
	m := svcTestModel(st, 160, 50)
	plain := ansi.Strip(m.renderServicesSidebarRaw(29, 40, true, svcTestNow, -1, -1))
	if strings.Contains(plain, "Start all") || strings.Contains(plain, "Stop all") || !strings.Contains(plain, "o adds services.json") || !strings.Contains(plain, "CONTAINERS") {
		t.Errorf("empty project with a host:\n%s", plain)
	}
	for _, h := range m.servicesHits(29) {
		if h.ID == "svc:btn:start-all" || h.ID == "svc:btn:stop-all" {
			t.Errorf("dead button target %s", h.ID)
		}
	}
	for name, set := range map[string]func(){
		"no engine":   func() { st.containerEngine = containers.Engine{} },
		"host hidden": func() { st.containerDetected = false; st.applySectionChoice([]string{"project"}) },
	} {
		set()
		st.touch()
		plain = ansi.Strip(m.renderServicesSidebarRaw(29, 40, true, svcTestNow, -1, -1))
		for _, want := range []string{"No services", "compose.yaml", "o"} {
			if !strings.Contains(plain, want) {
				t.Errorf("%s: card has no %q:\n%s", name, want, plain)
			}
		}
		if strings.Contains(plain, "CONTAINERS ") && strings.Contains(plain, "PROJECT") {
			t.Errorf("%s: not the card:\n%s", name, plain)
		}
	}
}

// runHostCmd runs a command tree and feeds every host answer back, like
// the Bubble Tea loop would.
func runHostCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0; n++ {
		if n > 100 {
			t.Fatal("command loop")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		result := make(chan tea.Msg, 1)
		go func() { result <- c() }()
		var got tea.Msg
		select {
		case got = <-result:
		case <-time.After(2 * time.Second):
			m.services.stopHostWatch()
			m.services.stopWatch()
			select {
			case got = <-result:
			case <-time.After(time.Second):
				t.Fatal("command did not finish")
			}
		}
		switch msg := got.(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case containerHostMsg:
			next, _ := m.handleServicesMsg(msg)
			queue = append(queue, next)
		}
	}
}

func hostFake(t *testing.T) *servicestest.Fake {
	t.Helper()
	f := servicestest.New(t)
	dir := filepath.Join("..", "containers", "testdata", "docker27")
	for _, name := range []string{"version.json", "ps.json", "network-ls.json", "volume-ls.json", "context-ls.json", "images.json"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		f.Set(t, name, string(b))
	}
	t.Setenv("PATH", f.PathEnv())
	return f
}

// Tester item 3: every open section loads after detection; one opened
// while detection runs loads too; a folded section is not listed.
func TestContainersDetectLoadsOpenSections(t *testing.T) {
	f := hostFake(t)
	st := svcTestState()
	st.root = t.TempDir()
	st.sections = map[string]bool{}
	st.visible = true
	st.collapsed[hostSection("networks")] = false
	m := svcTestModel(st, 100, 60)
	cmd := m.containersDetect()
	if cmd == nil {
		t.Fatal("no detection")
	}
	if got := svcRowsText(st); !strings.Contains(got, "#host/networks,!Loading…") {
		t.Errorf("before the answer: %s", got)
	}
	st.collapsed[hostSection("volumes")] = false // opened while detecting
	runHostCmd(t, &m, cmd)
	got := svcRowsText(st)
	for _, bad := range []string{"No networks", "No volumes", "Loading…"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q after detection: %s", bad, got)
		}
	}
	if !st.hostLoaded["networks"] || !st.hostLoaded["volumes"] || len(st.networkRows) == 0 || len(st.volumeRows) == 0 {
		t.Errorf("open sections not loaded: %v", st.hostLoaded)
	}
	if f.Count("images") != 0 {
		t.Errorf("folded IMAGES was listed: %q", f.Calls())
	}
	if st.activeContext() != "default" {
		t.Errorf("active context = %q", st.activeContext())
	}
}

// Tester items 3 and 9: unfolding a section that never loaded (Space or
// a click on its header) finds the engine if needed and lists it.
func TestContainersOpeningSectionLoads(t *testing.T) {
	hostFake(t)
	for _, how := range []string{"space", "click"} {
		st := svcTestState()
		st.root = t.TempDir()
		st.sections = map[string]bool{}
		st.visible = true
		st.collapsed[hostSection("containers")] = true // nothing open: no detection yet
		m := svcTestModel(st, 100, 60)
		m.focus = FocusExplorer
		if cmd := m.containersDetect(); cmd != nil {
			t.Fatal("detection with every section folded")
		}
		var idx int
		for i, r := range st.rows() {
			if r.section == hostSection("networks") && r.kind == svcRowSection {
				idx = i
			}
		}
		seq := st.seq
		var cmd tea.Cmd
		if how == "space" {
			st.cursor = idx
			m, cmd = m.handleServicesSidebarKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
		} else {
			y := -1
			for i, li := range svcBodyLines(st.rows(), st.cursor, svcBodyH(m.h)) {
				if li == idx {
					y = svcListTop + i
				}
			}
			m, cmd = m.handleServicesSidebarMouse(3, y, tea.MouseLeft)
		}
		if st.collapsed[hostSection("networks")] || st.seq == seq || cmd == nil {
			t.Fatalf("%s: folded=%v seq %d→%d cmd=%v", how, st.collapsed[hostSection("networks")], seq, st.seq, cmd != nil)
		}
		runHostCmd(t, &m, cmd)
		if !st.hostLoaded["networks"] || len(st.networkRows) == 0 {
			t.Errorf("%s: networks not loaded: %s", how, svcRowsText(st))
		}
	}
}

// Tester item 10: the ⋯ menu keeps the toggled row selected and has a
// Refresh item.
func TestContainersMenuKeepsSelection(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	st := svcHostState()
	m := svcTestModel(st, 100, 40)
	m.openContainersSectionsMenu("")
	view := ansi.Strip(m.menu.View())
	if !m.menuOpen || m.menuKind != menuKindContainers || !strings.Contains(view, "Images") || !strings.Contains(view, "Refresh all") {
		t.Fatalf("menu:\n%s", view)
	}
	m.handleContainersMenuSelect("section:images")
	if st.sectionShown("images") || !m.menuOpen || m.menu.SelectedID() != "section:images" {
		t.Errorf("after toggle: shown=%v open=%v selected=%q", st.sectionShown("images"), m.menuOpen, m.menu.SelectedID())
	}
	saved := loadSettings().ContainersSections
	if containsString(saved, "images") || !containsString(saved, "containers") {
		t.Errorf("containers_sections = %v", saved)
	}
}

// Tester item 10: session fold keys keep only known, non-default values
// and folded groups that still exist.
func TestContainersFoldKeysPruned(t *testing.T) {
	st := svcHostState()
	st.applyFoldKeys([]string{"bogus", "project", "!images", "containers", "grp/containers/gone", "grp/containers/shop", "grp/nope/x", "!grp/images/redis"})
	if st.collapsed["bogus"] || st.collapsed[hostSection("bogus")] || st.collapsed["grp/nope/x"] {
		t.Errorf("unknown keys applied: %v", st.collapsed)
	}
	got := strings.Join(st.foldKeys(), ",")
	if got != "!images,containers,grp/containers/shop" {
		t.Errorf("fold keys = %s", got)
	}
}

func TestContainersRefreshPaletteNeedsNoSelectedRow(t *testing.T) {
	st := svcHostState()
	st.loaded = true
	m := svcTestModel(st, 100, 40)
	st.cursor = 0
	cmd, ok := m.dispatchServicesPalette("containers-refresh")
	if !ok || cmd == nil {
		t.Fatalf("refresh action unavailable without a selected container: ok=%v cmd=%v", ok, cmd != nil)
	}
}

func TestContainersStartPaletteUsesSelectedContainer(t *testing.T) {
	st := svcHostState()
	st.loaded = true
	m := svcTestModel(st, 100, 40)
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.host == hostContainer && r.hostID == "c5" {
			st.cursor = i
			break
		}
	}
	if cmd, ok := m.dispatchServicesPalette("containers-start"); !ok || cmd == nil {
		t.Fatalf("start action unavailable: ok=%v cmd=%v", ok, cmd != nil)
	}
	if !st.busy[containerBusyPrefix+"c5"].Busy() || st.busy[containerBusyPrefix+"c1"].Busy() {
		t.Fatalf("palette action did not use the selected row: busy=%v", st.busy)
	}
}

func TestContainersAIPaletteUsesNamedContainer(t *testing.T) {
	st := svcHostState()
	m := svcTestModel(st, 100, 40)
	st.cursor = 0 // PROJECT header, not the requested container.
	if _, err := m.dispatchContainerAIAction("containers-stop", "nginx"); err != nil {
		t.Fatal(err)
	}
	if !st.busy[containerBusyPrefix+"c5"].Busy() || st.busy[containerBusyPrefix+"c1"].Busy() {
		t.Fatalf("action did not target nginx: busy=%v", st.busy)
	}
	if _, err := m.dispatchContainerAIAction("containers-stop", ""); err == nil {
		t.Fatal("missing target was accepted")
	}
}

func TestContainersAIComposeActionFindsNamedProject(t *testing.T) {
	st := svcHostState()
	dir, err := os.MkdirTemp("/tmp", "s")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	for i := range st.containerRows {
		if st.containerRows[i].Project == "shop" {
			st.containerRows[i].WorkingDir = dir
			st.containerRows[i].ConfigFiles = []string{"compose.yaml"}
		}
	}
	m := svcTestModel(st, 160, 50)
	st.cursor = 0
	if _, err := m.dispatchContainerAIAction("containers-compose-down", "shop"); err != nil {
		t.Fatal(err)
	}
	if !m.confirmOpen || m.pendingContainerProject != "shop" || m.pendingContainerDir != dir {
		t.Fatalf("compose action did not open the shop confirmation card: open=%v project=%q dir=%q", m.confirmOpen, m.pendingContainerProject, m.pendingContainerDir)
	}
	card := strings.Join(strings.Fields(ansi.Strip(m.confirm.View())), " ")
	if !strings.Contains(card, "shop") || !strings.Contains(card, dir) {
		t.Fatalf("confirmation card does not identify shop and its folder:\n%s", card)
	}
	if st.cursor != 0 {
		t.Fatalf("compose action changed cursor from 0 to %d", st.cursor)
	}
}

// The hidden-view perf guard covers the new loaders too: nothing starts
// while the view is hidden.
func TestContainersHiddenViewLoadsNothing(t *testing.T) {
	st := svcHostState()
	st.visible = false
	m := svcTestModel(st, 100, 40)
	if m.containersLoadVisible() != nil || m.containersReloadAll() != nil || m.containersOpened("networks") != nil {
		t.Error("hidden view scheduled engine work")
	}
}
