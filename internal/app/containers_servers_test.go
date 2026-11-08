package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/theme"
)

// serversState is the Containers view with the local engine and two
// ssh:// hosts in CONTEXTS (open), the remote hosts not loaded yet.
func serversState(t *testing.T) *servicesState {
	t.Helper()
	mode := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = mode })
	st := svcHostState()
	st.contextRows = []containers.Context{
		{Name: "default", Current: true, DockerEndpoint: "unix:///var/run/docker.sock"},
		baaytonaContext,
		{Name: "broken", DockerEndpoint: "ssh://me@down.example.com"},
	}
	st.collapsed[hostSection("contexts")] = false
	st.touch()
	return st
}

// fakeServerLists answers each host's container list and counts the
// calls per host.
func fakeServerLists(t *testing.T) map[string]int {
	t.Helper()
	calls := map[string]int{}
	var mu sync.Mutex
	old := serverListContainers
	serverListContainers = func(_ context.Context, e containers.Engine) ([]containers.Container, error) {
		mu.Lock()
		calls[e.Context]++
		mu.Unlock()
		switch e.Context {
		case "baaytona":
			return []containers.Container{baaytonaDB, baaytonaRedis, baaytonaApp}, nil
		case "broken":
			return nil, errors.New("error during connect: ssh: connect to host down.example.com port 22: Connection timed out")
		}
		return nil, nil
	}
	t.Cleanup(func() { serverListContainers = old })
	return calls
}

// cursorTo moves the cursor to the first row that matches.
func cursorTo(t *testing.T, m *Model, match func(svcRow) bool) svcRow {
	t.Helper()
	if !m.servicesCursorTo(match) {
		t.Fatalf("no such row in %q", rowNames(m.services))
	}
	r, _ := m.servicesCurrentRow()
	return r
}

// openServer opens a host's header (Space) and delivers its list.
func openServer(t *testing.T, m *Model, name string) {
	t.Helper()
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostContext && r.hostID == name })
	cmd := press(m, " ")
	if cmd == nil {
		return
	}
	if msg, ok := cmd().(serverListMsg); ok {
		_ = m.onServerList(msg)
	}
}

// Every host is in the one tree at the same time (no Switch): its name
// and where it points, then (opened) its compose projects and their
// containers; a + Add Server… row ends the list.
func TestContainersServersTreeShowsEveryHost(t *testing.T) {
	calls := fakeServerLists(t)
	m0 := svcTestModel(serversState(t), 120, 40)
	m := &m0
	names := strings.Join(rowNames(m.services), "\n")
	for _, want := range []string{"default · this machine", "baaytona · deploy@65.109.212.14", "broken · me@down.example.com", "+ Add Server…"} {
		if !strings.Contains(names, want) {
			t.Fatalf("no %q in the tree:\n%s", want, names)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("hosts were listed before they were opened: %v", calls)
	}

	openServer(t, m, "baaytona")
	rows := m.services.rows()
	var under []string
	in := false
	for _, r := range rows {
		if r.host == hostContext {
			in = r.hostID == "baaytona"
			continue
		}
		if in && (r.kind == svcRowGroup || r.host == hostServerContainer) {
			under = append(under, strings.Repeat("  ", r.depth)+r.name)
		}
	}
	got := strings.Join(under, "\n")
	want := "  baaytona\n    baaytona-db-1\n    baaytona-scraper-redis\n    baaytona-web-1"
	if got != want {
		t.Fatalf("baaytona's tree:\n%s\nwant\n%s", got, want)
	}
	// Closing and opening again does not ask the host again (no polling).
	openServer(t, m, "baaytona")
	openServer(t, m, "baaytona")
	if calls["baaytona"] != 1 {
		t.Fatalf("baaytona was listed %d times, want once", calls["baaytona"])
	}
}

// A host that does not answer shows its one-line reason under its own
// header; the other hosts keep their rows.
func TestContainersServerThatFailsDoesNotBlockTheOthers(t *testing.T) {
	fakeServerLists(t)
	m0 := svcTestModel(serversState(t), 120, 40)
	m := &m0
	openServer(t, m, "broken")
	openServer(t, m, "baaytona")
	rows := m.services.rows()
	note, after := "", ""
	for i, r := range rows {
		if r.host == hostContext && r.hostID == "broken" && i+1 < len(rows) {
			note = rows[i+1].text
			if rows[i+1].kind != svcRowNote {
				note = ""
			}
		}
		if r.host == hostServerContainer && r.name == "baaytona-db-1" {
			after = r.name
		}
	}
	if note != "ssh: Connection timed out" {
		t.Fatalf("broken's note = %q", note)
	}
	if after == "" {
		t.Fatal("baaytona's containers are gone while broken fails")
	}
}

// Enter on a database container of a remote host opens it in Databases
// through an SSH tunnel to that host.
func TestContainersServerDatabaseRowOpensInDatabases(t *testing.T) {
	withConfigDir(t)
	fakeServerLists(t)
	m0 := svcTestModel(serversState(t), 120, 40)
	m := &m0
	m.databases = newDBState()
	m.databases.root, m.databases.loaded = t.TempDir(), true
	openServer(t, m, "baaytona")
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostServerContainer && r.name == "baaytona-db-1" })
	_ = press(m, "enter")
	c := m.databases.pendingConn
	if c.SSH == nil || c.SSH.Host != "65.109.212.14" || c.SSH.User != "deploy" || c.Container == nil || c.Container.Name != "baaytona-db-1" || c.Port != 5432 {
		t.Fatalf("pending = %+v ssh %+v container %+v", c, c.SSH, c.Container)
	}
	if !m.promptOpen || m.promptKind != promptKindDBPassword {
		t.Fatal("no password prompt")
	}
}

// + Add Server… asks the name, then ssh://user@host (Create Context).
func TestContainersAddServerRowAsksNameThenHost(t *testing.T) {
	m0 := svcTestModel(serversState(t), 120, 40)
	m := &m0
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostAddServer })
	_ = press(m, "enter")
	if !m.promptOpen || m.promptKind != promptKindContextName {
		t.Fatalf("no name prompt (open %v kind %d)", m.promptOpen, m.promptKind)
	}
	_ = m.onContextPrompt(promptKindContextName, "rentcar")
	if !m.promptOpen || m.promptKind != promptKindContextHost || !strings.Contains(ansi.Strip(m.prompt.View()), "ssh://") {
		t.Fatal("no host prompt after the name")
	}
}

// 80×24 in ASCII: every line fits and the host reads.
func TestContainersServersTreeAt80x24ASCII(t *testing.T) {
	fakeServerLists(t)
	st := serversState(t)
	theme.CurrentIconMode = theme.IconModeASCII // serversState puts the mode back
	st.touch()
	m0 := svcTestModel(st, 80, 24)
	m := &m0
	openServer(t, m, "baaytona")
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostServerContainer && r.name == "baaytona-db-1" })
	out := m.renderServicesSidebarRaw(29, 24, true, svcTestNow, -1, -1)
	plain := ansi.Strip(out)
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w != 29 {
			t.Fatalf("line %d is %d wide: %q", i, w, ansi.Strip(l))
		}
	}
	if !strings.Contains(plain, "baaytona-db-1") {
		t.Fatalf("the database row is not drawn:\n%s", plain)
	}
	for _, l := range strings.Split(plain, "\n") {
		if !strings.Contains(l, "baaytona-db-1") && !strings.Contains(l, "deploy@") {
			continue // the tree's own rows
		}
		for _, r := range l {
			if r > 0x7e && r != '…' && r != '▕' && r != '▌' && r != '▎' {
				t.Fatalf("non-ASCII %q in the ASCII tree row %q", r, l)
			}
		}
	}
	_ = tea.Quit
}
