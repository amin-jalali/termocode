package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/services/servicestest"
	"github.com/amin-jalali/termocode/internal/theme"
)

// pumpHost runs a command tree and feeds every answer back to the
// services handler, like the Bubble Tea loop. Commands that wait for an
// event (the watcher) are dropped after a short wait; the test cleanup
// stops the watcher.
func pumpHost(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	pumpHostUntil(t, m, cmd, nil)
}

// pumpHostUntil is pumpHost for a slow step (a detect or an
// encrypted-file save under -race on a busy runner): every command runs
// on its own goroutine at once, so a watcher that never returns blocks
// nothing, and the pump waits up to 30 s until done (asked after each
// message is handled) reports true, then 500 ms for what follows. With done nil it is pumpHost.
func pumpHostUntil(t *testing.T, m *Model, cmd tea.Cmd, done func(tea.Msg) bool) {
	t.Helper()
	t.Cleanup(func() { m.services.stopHostWatch(); m.services.stopWatch() })
	if done == nil {
		pumpHostSerial(t, m, cmd)
		return
	}
	results := make(chan tea.Msg, 64)
	n := 0
	start := func(c tea.Cmd) {
		if c == nil {
			return
		}
		if n++; n > 200 {
			t.Fatal("command loop")
		}
		go func() { results <- c() }()
	}
	start(cmd)
	deadline := time.After(30 * time.Second)
	for {
		quiet := time.After(500 * time.Millisecond)
		if done != nil {
			quiet = nil
		}
		select {
		case got := <-results:
			if b, ok := got.(tea.BatchMsg); ok {
				for _, c := range b {
					start(c)
				}
				continue
			}
			next, _ := m.handleServicesMsg(got)
			if done != nil && done(got) {
				done = nil
			}
			start(next)
		case <-deadline:
			if done != nil {
				t.Fatal("the awaited message never came")
			}
		case <-quiet:
			return
		}
	}
}

func pumpHostSerial(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0; n++ {
		if n > 200 {
			t.Fatal("command loop")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		result := make(chan tea.Msg, 1)
		go func() { result <- c() }()
		select {
		case got := <-result:
			if b, ok := got.(tea.BatchMsg); ok {
				queue = append(queue, b...)
				continue
			}
			next, _ := m.handleServicesMsg(got)
			queue = append(queue, next)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// contextsLoaded reports the detect has filled CONTEXTS.
func contextsLoaded(m *Model) func(tea.Msg) bool {
	return func(tea.Msg) bool { return len(m.services.contextRows) > 0 }
}

// ctxSession is a Containers view with every section open, detected and
// loaded from the fake docker (docker27 fixtures: contexts default + prod).
func ctxSession(t *testing.T) (*Model, *servicestest.Fake) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("DOCKER_CONTEXT", "")
	f := hostFake(t)
	st := svcTestState()
	st.root = t.TempDir()
	st.sections = map[string]bool{}
	st.visible = true
	for _, s := range hostSectionOrder {
		st.collapsed[hostSection(s)] = false
	}
	m := svcTestModel(st, 100, 80)
	pumpHostUntil(t, &m, m.containersDetect(), contextsLoaded(&m)) // detect is slow on a busy runner
	if !st.containerEngine.Reachable || len(st.contextRows) != 2 {
		t.Fatalf("setup: engine=%+v contexts=%v", st.containerEngine, st.contextRows)
	}
	return &m, f
}

func selectContext(t *testing.T, st *servicesState, name string) {
	t.Helper()
	for i, r := range st.rows() {
		if r.kind == svcRowHostItem && r.host == hostContext && r.hostID == name {
			st.cursor = i
			return
		}
	}
	t.Fatalf("no context row %q:\n%s", name, svcRowsText(st))
}

func press(m *Model, s string) tea.Cmd {
	k := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	switch s {
	case "enter":
		k = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		k = tea.KeyMsg{Type: tea.KeyEsc}
	}
	next, cmd := m.handleServicesSidebarKey(k)
	*m = next
	return cmd
}

func plainLines(m *Model, w, h int) []string {
	return strings.Split(ansi.Strip(m.renderServicesSidebarRaw(w, h, true, svcTestNow, -1, -1)), "\n")
}

func callsWithPrefix(calls []string, prefix string) []string {
	var out []string
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func TestContextsSectionListsContextsWithGlyphs(t *testing.T) {
	defer func(m theme.IconMode) { theme.CurrentIconMode = m }(theme.CurrentIconMode)
	for mode, want := range map[theme.IconMode][2]string{theme.IconModeUnicode: {"◉ default", "◦ prod"}, theme.IconModeASCII: {"* default", "o prod"}} {
		theme.CurrentIconMode = mode
		st := svcHostState()
		st.collapsed[hostSection("contexts")] = false
		st.contextRows[0].DockerEndpoint = "unix:///var/run/docker.sock"
		st.contextRows[1].DockerEndpoint = "ssh://deploy@prod"
		m := svcTestModel(st, 100, 60)
		lines := plainLines(&m, 29, 60)
		if lineWith(lines, "CONTEXTS") == "" || lineWith(lines, want[0]) == "" || lineWith(lines, want[1]) == "" {
			t.Errorf("mode %v: want %q and %q:\n%s", mode, want[0], want[1], strings.Join(lines, "\n"))
		}
		selectContext(t, st, "prod")
		l1, l2 := st.hostFooter(st.rows()[st.cursor])
		if l1 != "prod" || l2 != "ssh://deploy@prod" {
			t.Errorf("footer = %q / %q, want name and endpoint", l1, l2)
		}
	}
}

func TestContextsAutoHiddenWithOnlyDefaultAndForPodman(t *testing.T) {
	st := svcHostState()
	st.contextRows = st.contextRows[:1]
	m := svcTestModel(st, 100, 60)
	if lineWith(plainLines(&m, 29, 60), "CONTEXTS") != "" {
		t.Error("CONTEXTS shown with only the default context")
	}
	st.contextRows = append(st.contextRows, containers.Context{Name: "prod"})
	st.touch()
	if lineWith(plainLines(&m, 29, 60), "CONTEXTS") == "" {
		t.Error("CONTEXTS hidden although a second context exists")
	}
	// CP-12: podman connections are CONTEXTS now. With two connections the
	// section shows; with none (only the local socket) it hides like
	// docker with only `default`.
	st.containerEngine.Kind = "podman"
	st.touch()
	if lineWith(plainLines(&m, 29, 60), "CONTEXTS") == "" {
		t.Error("CONTEXTS hidden for podman with two connections")
	}
	st.contextRows = nil
	st.touch()
	if lineWith(plainLines(&m, 29, 60), "CONTEXTS") != "" {
		t.Error("CONTEXTS shown for podman without connections")
	}
	// Hidden sections never run their command and never get a cursor.
	for _, r := range st.rows() {
		if r.host == hostContext || r.section == hostSection("contexts") {
			t.Errorf("hidden section has a row: %+v", r)
		}
	}
}

func TestContextEnterSwitchesThisSessionOnly(t *testing.T) {
	m, f := ctxSession(t)
	st := m.services
	oldPID := f.PID("events.pid")
	if oldPID == 0 || !servicestest.Alive(oldPID) {
		t.Fatal("watcher is not running before the switch")
	}
	before := len(f.CtxCalls())
	selectContext(t, st, "prod")
	pumpHost(t, m, press(m, "enter"))
	if st.sessionContext != "prod" || st.containerEngine.Context != "prod" {
		t.Fatalf("session context = %q engine context = %q", st.sessionContext, st.containerEngine.Context)
	}
	calls := f.CtxCalls()[before:]
	seen := map[string]bool{}
	for _, c := range calls {
		if !strings.HasPrefix(c, "prod|") {
			t.Errorf("call after the switch without DOCKER_CONTEXT=prod: %q", c)
		}
		for _, sub := range []string{"version", "ps", "images", "network ls", "volume ls", "events"} {
			if strings.HasPrefix(strings.TrimPrefix(c, "prod|"), sub) {
				seen[sub] = true
			}
		}
	}
	for _, sub := range []string{"version", "ps", "images", "network ls", "volume ls", "events"} {
		if !seen[sub] {
			t.Errorf("no %q call after the switch (full refresh + watcher restart): %q", sub, calls)
		}
	}
	if f.Count("context use") != 0 {
		t.Error("a session switch ran `docker context use`")
	}
	if !servicestest.WaitFor(3*time.Second, func() bool { p := f.PID("events.pid"); return p != oldPID && servicestest.Alive(p) }) {
		t.Errorf("watcher was not restarted (pid %d, now %d)", oldPID, f.PID("events.pid"))
	}
	if !servicestest.WaitGone(oldPID, 3*time.Second) {
		t.Error("the old watcher is still running")
	}
	lines := plainLines(m, 29, 80)
	if lineWith(lines, "docker 27 · prod") == "" || lineWith(lines, "docker 27 · default") != "" {
		t.Errorf("engine line does not show prod:\n%s", strings.Join(lines, "\n"))
	}
	if g := lineWith(lines, "prod"); !strings.Contains(g, "prod") {
		t.Fatal("no prod row")
	}
	// Enter on the active context is a no-op, not a second refresh.
	n := len(f.CtxCalls())
	selectContext(t, st, "prod")
	pumpHost(t, m, press(m, "enter"))
	if len(f.CtxCalls()) != n {
		t.Errorf("Enter on the active context ran engine calls: %q", f.CtxCalls()[n:])
	}
}

func TestContextMakeDefaultNeedsTheCardAndRunsExactCommand(t *testing.T) {
	m, f := ctxSession(t)
	st := m.services
	selectContext(t, st, "prod")
	if cmd := press(m, "D"); cmd != nil {
		t.Fatal("D ran something before the card")
	}
	if !m.confirmOpen {
		t.Fatal("no confirm card")
	}
	card := strings.Join(strings.Fields(ansi.Strip(m.confirm.View())), " ")
	if !strings.Contains(card, "context use prod") || !strings.Contains(card, "every terminal") {
		t.Fatalf("card does not name the exact command and the effect: %s", card)
	}
	// Esc runs nothing.
	next, cmd := m.Update(confirm.CloseMsg{})
	*m = next.(Model)
	pumpHost(t, m, cmd)
	if f.Count("context use") != 0 || m.confirmOpen {
		t.Fatalf("Esc ran a command or kept the card: %q", f.Calls())
	}
	// Confirm runs exactly `context use prod`, once, without the session override.
	selectContext(t, st, "prod")
	press(m, "D")
	cmd = m.handleConfirmSelect("use")
	before := len(f.CtxCalls())
	pumpHost(t, m, cmd)
	uses := callsWithPrefix(f.CtxCalls()[before:], "")
	var use []string
	for _, c := range uses {
		if strings.Contains(c, "context use") {
			use = append(use, c)
		}
	}
	if len(use) != 1 || use[0] != "|context use prod" {
		t.Fatalf("context use calls = %q", use)
	}
	if st.sessionContext != "" {
		t.Errorf("make default changed the session context to %q", st.sessionContext)
	}
	if f.Count("context ls") < 2 {
		t.Error("CONTEXTS was not refreshed after make default")
	}
}

func TestContextRemoteFailureShowsNoteAndCanSwitchBack(t *testing.T) {
	m, f := ctxSession(t)
	st := m.services
	f.Set(t, "fail-prod.err", "error during connect: ssh: connect to host prod port 22: Connection timed out\n")
	selectContext(t, st, "prod")
	pumpHost(t, m, press(m, "enter"))
	lines := plainLines(m, 60, 80)
	text := strings.Join(lines, "\n")
	if lineWith(lines, "context prod: ") == "" || !strings.Contains(text, "Connection timed out") {
		t.Fatalf("no remote failure note naming the context:\n%s", text)
	}
	if !strings.Contains(text, "R retry · Enter on CONTEXTS to switch back") {
		t.Fatalf("note has no way back:\n%s", text)
	}
	if lineWith(lines, "CONTEXTS") == "" || lineWith(lines, "default") == "" {
		t.Fatalf("CONTEXTS is gone while the remote fails:\n%s", text)
	}
	// Enter on default switches back and everything loads again.
	selectContext(t, st, "default")
	pumpHost(t, m, press(m, "enter"))
	lines = plainLines(m, 60, 80)
	if st.sessionContext != "default" || !st.containerEngine.Reachable || lineWith(lines, "context prod: ") != "" {
		t.Fatalf("did not recover: ctx=%q reachable=%v\n%s", st.sessionContext, st.containerEngine.Reachable, strings.Join(lines, "\n"))
	}
	if len(st.containerRows) == 0 {
		t.Error("containers were not listed after switching back")
	}
}

func TestContextPaletteAndMenuAreUserOnly(t *testing.T) {
	for _, id := range []string{"containers-context-use", "containers-context-default"} {
		if p, ok := aiActionPolicies[id]; !ok || p.Gate != aiGateNever {
			t.Errorf("%s: AI policy %+v ok=%v, want never", id, p, ok)
		}
	}
	titles := map[string]string{}
	for _, e := range PaletteCatalog() {
		titles[e.ID] = e.Title
	}
	if titles["containers-context-use"] != "Containers: Switch Context…" || titles["containers-context-default"] != "Containers: Make Context Default…" {
		t.Errorf("palette titles = %q / %q", titles["containers-context-use"], titles["containers-context-default"])
	}
	st := svcHostState()
	m := svcTestModel(st, 100, 40)
	for _, id := range []string{"containers-context-use", "containers-context-default"} {
		if _, err := m.dispatchContainerAIAction(id, "prod"); err == nil {
			t.Errorf("the AI action dispatcher accepted %s", id)
		}
	}
	if st.sessionContext != "" || m.confirmOpen {
		t.Error("an AI dispatch changed the context or opened a card")
	}
}

func TestContextPaletteSwitchAndDefaultUseSelectedRow(t *testing.T) {
	m, f := ctxSession(t)
	st := m.services
	selectContext(t, st, "prod")
	cmd, ok := m.dispatchServicesPalette("containers-context-default")
	if !ok {
		t.Fatal("palette id not handled")
	}
	pumpHost(t, m, cmd)
	if !m.confirmOpen || !strings.Contains(ansi.Strip(m.confirm.View()), "context use") {
		t.Fatal("Make Context Default… did not open the card")
	}
	m.confirmOpen = false
	selectContext(t, st, "prod")
	cmd, ok = m.dispatchServicesPalette("containers-context-use")
	if !ok {
		t.Fatal("palette id not handled")
	}
	pumpHost(t, m, cmd)
	if st.sessionContext != "prod" || f.Count("context use") != 0 {
		t.Errorf("session=%q, context use calls=%d", st.sessionContext, f.Count("context use"))
	}
}

func TestContextDoubleClickIsEnter(t *testing.T) {
	m, _ := ctxSession(t)
	st := m.services
	idx := -1
	for i, r := range st.rows() {
		if r.host == hostContext && r.hostID == "prod" {
			idx = i
		}
	}
	y := -1
	for i, li := range svcBodyLines(st.rows(), st.cursor, svcBodyH(m.h)) {
		if li == idx {
			y = svcListTop + i
		}
	}
	if y < 0 {
		t.Fatal("prod row is off screen")
	}
	next, cmd := m.handleServicesSidebarMouse(3, y, tea.MouseLeft)
	*m = next
	if cmd != nil || st.sessionContext != "" || st.cursor != idx {
		t.Fatalf("single click: cmd=%v session=%q cursor=%d want %d", cmd != nil, st.sessionContext, st.cursor, idx)
	}
	y = -1
	for i, li := range svcBodyLines(st.rows(), st.cursor, svcBodyH(m.h)) {
		if li == idx {
			y = svcListTop + i
		}
	}
	next, cmd = m.handleServicesSidebarMouse(3, y, tea.MouseLeft)
	*m = next
	pumpHost(t, m, cmd)
	if st.sessionContext != "prod" {
		t.Errorf("double click did not switch: %q", st.sessionContext)
	}
}

func TestContainersEngineSettingChoosesEngine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// The machine may have a real podman on PATH (CI runners do).
	sys := t.TempDir()
	if err := os.WriteFile(filepath.Join(sys, "podman"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", sys+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := hostFake(t) // only docker is installed
	t.Setenv("PATH", f.Bin+string(os.PathListSeparator)+fakeToolsDir(t))
	cfg := loadSettings()
	cfg.ContainersEngine = "podman"
	if err := saveSettings(cfg); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings().ContainersEngine; got != "podman" {
		t.Fatalf("containers_engine = %q after save", got)
	}
	st := svcTestState()
	st.root = t.TempDir()
	st.sections = map[string]bool{}
	st.visible = true
	st.collapsed[hostSection("containers")] = false
	m := svcTestModel(st, 100, 40)
	pumpHost(t, &m, m.containersDetect())
	if st.containerEngine.Binary != "" {
		t.Errorf("containers_engine=podman used %q", st.containerEngine.Binary)
	}
	found := false
	for _, e := range SettingsCatalog() {
		if e.Key == "containers_engine" && e.Default == "auto" && len(e.Choices) == 3 {
			found = true
		}
	}
	if !found {
		t.Error("containers_engine is not in the settings catalog (docgen)")
	}
	if p, ok := aiSettingPolicies["containers_engine"]; !ok || p.Gate == aiGateAuto {
		t.Errorf("AI setting policy = %+v ok=%v", p, ok)
	}
}

// fakeToolsDir holds links to the few tools the fake engine script runs, so
// a test can drop the rest of PATH (and any real docker or podman on it).
func fakeToolsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"cat", "sleep", "tail"} {
		p, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(p, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestContextSwitchRefreshBeforeDetectWatchesNewContext: after a switch
// the project refresh and the detect with DOCKER_CONTEXT run at once, and
// the refresh often answers first. Its answer must not start the watcher
// with the old engine (no context); the detect's answer starts it on prod.
func TestContextSwitchRefreshBeforeDetectWatchesNewContext(t *testing.T) {
	m, f := ctxSession(t)
	st := m.services
	t.Cleanup(func() { st.stopHostWatch(); st.stopWatch() })
	before := len(f.CtxCalls())
	b, ok := m.switchContext("prod")().(tea.BatchMsg)
	if !ok {
		t.Fatal("switch is not a batch")
	}
	results := make(chan tea.Msg, len(b))
	for _, c := range b {
		if c != nil {
			go func(c tea.Cmd) { results <- c() }(c)
		}
	}
	var snap, detect tea.Msg
	deadline := time.After(30 * time.Second)
	for snap == nil || detect == nil {
		select {
		case got := <-results:
			switch got.(type) {
			case servicesSnapshotMsg:
				snap = got
			case containerHostMsg:
				detect = got
			}
		case <-deadline:
			t.Fatalf("no refresh/detect answer (refresh %v, detect %v)", snap != nil, detect != nil)
		}
	}
	m.handleServicesMsg(snap) // the refresh wins the race
	m.handleServicesMsg(detect)
	if st.containerEngine.Context != "prod" {
		t.Fatalf("engine context = %q", st.containerEngine.Context)
	}
	hasProdEvents := func() bool {
		for _, c := range f.CtxCalls()[before:] {
			if strings.HasPrefix(c, "prod|events") {
				return true
			}
		}
		return false
	}
	if !servicestest.WaitFor(10*time.Second, hasProdEvents) {
		t.Errorf("watcher did not restart on prod: %q", f.CtxCalls()[before:])
	}
	for _, c := range f.CtxCalls()[before:] {
		if !strings.HasPrefix(c, "prod|") {
			t.Errorf("call after the switch without DOCKER_CONTEXT=prod: %q", c)
		}
	}
}
