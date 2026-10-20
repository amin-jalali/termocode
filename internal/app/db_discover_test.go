package app

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DB-16: the Databases view finds servers by itself. Fakes only: a fake
// container lister and a test listener on a random port.

func discoverModel(t *testing.T, root string) *Model {
	t.Helper()
	t.Setenv("TERMOCODE_NO_DB_DISCOVERY", "")
	m := &Model{activity: activity.New(), w: 100, h: 30}
	m.databases = newDBState()
	m.databases.root = root
	m.databases.loaded = true
	return m
}

func fakeContainers(t *testing.T, rows ...containers.Container) {
	t.Helper()
	old := dbListContainers
	dbListContainers = func(context.Context, string) ([]containers.Container, error) { return rows, nil }
	t.Cleanup(func() { dbListContainers = old })
}

func runDiscover(t *testing.T, m *Model) {
	t.Helper()
	cmd := m.databaseDiscover()
	if cmd == nil {
		t.Fatal("no discovery started")
	}
	msg, ok := cmd().(dbDiscoverMsg)
	if !ok {
		t.Fatal("discovery did not return its message")
	}
	m.onDBDiscover(msg)
}

func suggestionNames(s *dbState) []string {
	var out []string
	for _, r := range s.rows() {
		if r.kind == dbNodeSuggestion {
			out = append(out, r.name)
		}
	}
	return out
}

func TestDiscoverReadsContainersWithoutServicesSnapshot(t *testing.T) {
	fakeContainers(t, containers.Container{ID: "abc", Names: []string{"pg-dev"}, Image: "postgres:16", Ports: []containers.Port{{PublicPort: 15432}}})
	old := dbProbeTargets
	dbProbeTargets = nil
	t.Cleanup(func() { dbProbeTargets = old })
	m := discoverModel(t, t.TempDir())
	if m.services != nil {
		t.Fatal("test needs no Services snapshot")
	}
	runDiscover(t, m)
	got := suggestionNames(m.databases)
	if len(got) != 1 || got[0] != "pg-dev (container)" {
		t.Fatalf("suggestions = %q", got)
	}
}

func TestDiscoverLocalPortBecomesSuggestion(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no local listener")
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	fakeContainers(t)
	old := dbProbeTargets
	dbProbeTargets = []dbProbeTarget{{port, "postgres"}, {1, "redis"}}
	t.Cleanup(func() { dbProbeTargets = old })
	m := discoverModel(t, t.TempDir())
	runDiscover(t, m)
	want := "PostgreSQL on localhost:" + itoa(port)
	got := suggestionNames(m.databases)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("suggestions = %q, want [%q]", got, want)
	}
}

func TestDiscoverSkipsSavedAndContainerPorts(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no local listener")
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	old := dbProbeTargets
	dbProbeTargets = []dbProbeTarget{{port, "postgres"}}
	t.Cleanup(func() { dbProbeTargets = old })

	// A saved connection to that port.
	fakeContainers(t)
	m := discoverModel(t, t.TempDir())
	m.databases.conns = []dbclient.Conn{{ID: "app", Driver: "postgres", Host: "localhost", Port: port}}
	runDiscover(t, m)
	if got := suggestionNames(m.databases); len(got) != 0 {
		t.Fatalf("saved port still suggested: %q", got)
	}
	// A container that publishes that port.
	fakeContainers(t, containers.Container{ID: "c1", Names: []string{"db"}, Image: "postgres:16", Ports: []containers.Port{{PublicPort: port}}})
	m = discoverModel(t, t.TempDir())
	runDiscover(t, m)
	got := suggestionNames(m.databases)
	if len(got) != 1 || got[0] != "db (container)" {
		t.Fatalf("suggestions = %q, want the container only", got)
	}
}

func TestDiscoverKeepsHiddenSuggestionsHidden(t *testing.T) {
	fakeContainers(t, containers.Container{ID: "abc", Names: []string{"pg"}, Image: "postgres:16"})
	old := dbProbeTargets
	dbProbeTargets = nil
	t.Cleanup(func() { dbProbeTargets = old })
	m := discoverModel(t, t.TempDir())
	m.databases.hidden["suggest-container-abc"] = true
	runDiscover(t, m)
	if got := suggestionNames(m.databases); len(got) != 0 {
		t.Fatalf("hidden suggestion shown: %q", got)
	}
}

func TestDiscoverTextIsSanitizedAndNeverProbesOtherHosts(t *testing.T) {
	fakeContainers(t, containers.Container{ID: "abc", Names: []string{"pg\x1b[31m\nsecret"}, Image: "postgres:16"})
	old := dbProbeTargets
	dbProbeTargets = nil
	t.Cleanup(func() { dbProbeTargets = old })
	m := discoverModel(t, t.TempDir())
	runDiscover(t, m)
	for _, n := range suggestionNames(m.databases) {
		if strings.ContainsAny(n, "\x1b\n") || strings.Contains(n, "secret") {
			t.Fatalf("unsafe suggestion text %q", n)
		}
	}
	if dbProbeHost != "127.0.0.1" {
		t.Fatalf("probe host = %q", dbProbeHost)
	}
}

func TestDiscoverIsOffWhenSwitchedOffAndOnlyOnOpenAndRefresh(t *testing.T) {
	m := discoverModel(t, t.TempDir())
	t.Setenv("TERMOCODE_NO_DB_DISCOVERY", "1")
	if m.databaseDiscover() != nil {
		t.Fatal("discovery ran although switched off")
	}
	t.Setenv("TERMOCODE_NO_DB_DISCOVERY", "")
	fakeContainers(t)
	if cmd := m.revealDatabasesView(false); cmd == nil {
		t.Fatal("opening the view started no discovery")
	}
	if cmd := m.revealDatabasesView(false); cmd != nil {
		t.Fatal("a second reveal of the open view started another discovery")
	}
	if m.databaseRefresh() == nil {
		t.Fatal("R started no discovery")
	}
}

func TestDiscoverStaleResultIsDropped(t *testing.T) {
	fakeContainers(t, containers.Container{ID: "abc", Names: []string{"pg"}, Image: "postgres:16"})
	m := discoverModel(t, t.TempDir())
	cmd := m.databaseDiscover()
	m.databases.generation++ // the view was hidden or refreshed meanwhile
	m.onDBDiscover(cmd().(dbDiscoverMsg))
	if got := suggestionNames(m.databases); len(got) != 0 {
		t.Fatalf("stale result applied: %q", got)
	}
	if m.databaseDiscover() == nil {
		t.Fatal("a stale run blocks the next discovery")
	}
}

func TestDiscoverAddLocalSuggestionSavesWithoutPassword(t *testing.T) {
	root := t.TempDir()
	m := discoverModel(t, root)
	m.databases.nodes["suggest-local-5432"] = dbNode{kind: dbNodeSuggestion, name: "PostgreSQL on localhost:5432", path: "suggest-local-5432", driver: "postgres", host: "localhost", port: 5432}
	if cmd := m.databaseAddSuggestion("suggest-local-5432"); cmd != nil {
		t.Fatalf("unexpected command %T", cmd)
	}
	if len(m.databases.conns) != 1 || m.databases.conns[0].ID != "local-postgres" || m.databases.conns[0].Port != 5432 || m.databases.conns[0].Password != "" {
		t.Fatalf("conns = %#v", m.databases.conns)
	}
}

func TestEmptyStateOffersAddAndRemote(t *testing.T) {
	m := discoverModel(t, t.TempDir())
	m.showExp = true
	out := m.renderDatabasesSidebarRaw(32, 20, true)
	for _, want := range []string{"a add connection", "R refresh", strings.TrimSpace(dbRemoteLabel()), "No databases yet"} {
		if !strings.Contains(out, want) {
			t.Fatalf("empty state lacks %q:\n%s", want, out)
		}
	}
	m.databases.nodes["suggest-local-5432"] = dbNode{kind: dbNodeSuggestion, name: "PostgreSQL on localhost:5432", path: "suggest-local-5432", driver: "postgres", host: "localhost", port: 5432}
	out = m.renderDatabasesSidebarRaw(40, 20, true)
	for _, want := range []string{strings.TrimSpace(dbRemoteLabel()), "SUGGESTED", "PostgreSQL on localhost:5432"} {
		if !strings.Contains(out, want) {
			t.Fatalf("empty state with a suggestion lacks %q:\n%s", want, out)
		}
	}
}

func TestAddRemoteRunsSSHStepsAfterAddSteps(t *testing.T) {
	m := discoverModel(t, t.TempDir())
	m.w, m.h = 100, 30
	m.databaseAddRemote()
	if !m.pickerOpen || !strings.Contains(m.picker.View(), "remote") {
		t.Fatal("no remote picker")
	}
	m.databaseStartAdd("postgres")
	s := m.databases
	s.pendingConn.Host, s.pendingConn.ID, s.pendingConn.Database = "db.internal", "app", "app"
	m.databaseRetestPending()
	if m.promptKind != promptKindDBSSHHost {
		t.Fatalf("after the add steps the SSH steps did not start (prompt kind %v)", m.promptKind)
	}
}

func TestDoubleClickRows(t *testing.T) {
	s := newDBState()
	s.loaded = true
	s.conns = []dbclient.Conn{{ID: "a", Name: "a", Driver: "postgres", Host: "h", Port: 1}}
	s.nodes["a"] = dbNode{kind: dbNodeConnection, name: "a", path: "a", conn: "a", loaded: true, children: []string{"a/g"}}
	s.nodes["a/g"] = dbNode{kind: dbNodeGroup, name: "Tables", path: "a/g", conn: "a", loaded: true, children: []string{"a/g/t"}}
	s.nodes["a/g/t"] = dbNode{kind: dbNodeTable, name: "users", path: "a/g/t", conn: "a"}
	s.sessions["a"] = nil
	m := Model{databases: s, w: 40, h: 20, showExp: true, activity: activity.New()}
	click := func(y int) {
		m, _ = m.handleDatabasesSidebarMouse(3, y, tea.MouseLeft)
	}
	click(5) // the group: one click selects only
	if s.folded["a/g"] || s.cursor != 1 {
		t.Fatalf("single click changed folding or missed the row (cursor %d)", s.cursor)
	}
	click(5)
	if !s.folded["a/g"] {
		t.Fatal("double click did not fold the group")
	}
	s.lastClickAt = time.Time{}
	click(5)
	time.Sleep(450 * time.Millisecond)
	click(5)
	if !s.folded["a/g"] {
		t.Fatal("two slow clicks counted as a double click")
	}
}
