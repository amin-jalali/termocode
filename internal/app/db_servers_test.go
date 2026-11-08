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

// dbServersFakes stands in for the engine: the contexts (counted) and
// each host's containers.
func dbServersFakes(t *testing.T) *int {
	t.Helper()
	t.Setenv("TERMOCODE_NO_DB_DISCOVERY", "")
	oldList, oldCtx, oldRemote, oldTargets := dbListContainers, dbListContexts, dbListContextContainers, dbProbeTargets
	var mu sync.Mutex
	ctxCalls := 0
	dbListContainers = func(context.Context, string) ([]containers.Container, error) { return nil, nil }
	dbListContexts = func(context.Context, string) ([]containers.Context, error) {
		mu.Lock()
		ctxCalls++
		mu.Unlock()
		return []containers.Context{{Name: "default", Current: true, DockerEndpoint: "unix:///var/run/docker.sock"}, baaytonaContext,
			{Name: "rentcar", DockerEndpoint: "ssh://ops@rentcar.example.com:2222"}, {Name: "broken", DockerEndpoint: "ssh://me@down.example.com"}}, nil
	}
	dbListContextContainers = func(_ context.Context, _ string, name string) ([]containers.Container, error) {
		switch name {
		case "baaytona":
			return []containers.Container{baaytonaDB, baaytonaRedis, baaytonaApp}, nil
		case "rentcar":
			return []containers.Container{{ID: "r1", Names: []string{"/rentcar-db-1"}, Image: "mariadb:11", State: "running", Ports: []containers.Port{{PrivatePort: 3306, Type: "tcp"}}, Project: "rentcar"}}, nil
		}
		return nil, errors.New("error during connect: ssh: connect to host down.example.com port 22: Connection timed out")
	}
	dbProbeTargets = nil
	t.Cleanup(func() {
		dbListContainers, dbListContexts, dbListContextContainers, dbProbeTargets = oldList, oldCtx, oldRemote, oldTargets
	})
	return &ctxCalls
}

func dbServersModel(t *testing.T) *Model {
	t.Helper()
	mode := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = mode })
	s := newDBState()
	s.root, s.visible, s.loaded = t.TempDir(), true, true
	m := dbTestModel(s)
	m.w, m.h = 80, 24
	return m
}

func dbDiscoverNow(t *testing.T, m *Model) {
	t.Helper()
	msg, ok := dbRunFor[dbDiscoverMsg](t, m.databaseDiscover())
	if !ok {
		t.Fatal("no discovery result")
	}
	_ = m.onDBDiscover(msg)
}

// dbTreeText is the rows as "depth-indented name" lines.
func dbTreeText(s *dbState) string {
	var b strings.Builder
	for _, r := range s.rows() {
		b.WriteString(strings.Repeat("  ", r.depth) + r.name + "\n")
	}
	return b.String()
}

// The Databases view has the same host tree: host, compose project,
// database container; a host that fails shows its reason under it; the
// list ends with + Add Server… (the row's mark is the +).
func TestDBServersTreeHostProjectContainer(t *testing.T) {
	dbServersFakes(t)
	m := dbServersModel(t)
	dbDiscoverNow(t, m)
	got := dbTreeText(m.databases)
	want := "SERVERS\n" +
		"baaytona · deploy@65.109.212.14\n" +
		"  baaytona\n" +
		"    baaytona-db-1\n" +
		"    baaytona-scraper-redis\n" +
		"broken · me@down.example.com\n" +
		"  ssh: Connection timed out\n" +
		"rentcar · ops@rentcar.example.com:2222\n" +
		"  rentcar\n" +
		"    rentcar-db-1\n" +
		"Add Server…\n"
	if !strings.Contains(got, want) {
		t.Fatalf("tree:\n%s\nwant (in it):\n%s", got, want)
	}
	if strings.Contains(got, "baaytona-web-1") {
		t.Fatalf("an app container is in the database tree:\n%s", got)
	}
	if strings.Contains(got, "SUGGESTED") {
		t.Fatalf("the remote containers are also under SUGGESTED:\n%s", got)
	}
}

// Picking a container adds it with the tunnel, container and port filled
// in and asks only for the password.
func TestDBServersPickAsksOnlyThePassword(t *testing.T) {
	dbServersFakes(t)
	m := dbServersModel(t)
	dbDiscoverNow(t, m)
	s := m.databases
	for i, r := range s.rows() {
		if r.name == "baaytona-db-1" {
			s.cursor = i
		}
	}
	next, _ := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = next
	c := m.databases.pendingConn
	if c.SSH == nil || c.SSH.Host != "65.109.212.14" || c.Container == nil || c.Host != "baaytona-db-1" || c.Port != 5432 || c.User != "postgres" {
		t.Fatalf("pending = %+v ssh %+v", c, c.SSH)
	}
	if !m.promptOpen || m.promptKind != promptKindDBPassword {
		t.Fatal("no password prompt")
	}
}

// + Add Server… in Databases is the Containers view's Create Context.
func TestDBServersAddServerAsksNameThenHost(t *testing.T) {
	dbServersFakes(t)
	m := dbServersModel(t)
	m.services = svcHostState()
	dbDiscoverNow(t, m)
	s := m.databases
	for i, r := range s.rows() {
		if r.name == "Add Server…" {
			s.cursor = i
		}
	}
	next, _ := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyEnter})
	*m = next
	if !m.promptOpen || m.promptKind != promptKindContextName {
		t.Fatalf("no name prompt (open %v kind %d)", m.promptOpen, m.promptKind)
	}
}

// One host list: when the Containers view already read the contexts, the
// Databases view uses them and does not ask the engine again.
func TestDBServersShareTheContainersHostList(t *testing.T) {
	calls := dbServersFakes(t)
	m := dbServersModel(t)
	st := svcHostState()
	st.contextRows = []containers.Context{{Name: "default", Current: true, DockerEndpoint: "unix:///var/run/docker.sock"}, baaytonaContext}
	st.hostLoaded["contexts"] = true
	m.services = st
	dbDiscoverNow(t, m)
	got := dbTreeText(m.databases)
	if *calls != 0 {
		t.Fatalf("the contexts were read again (%d times)", *calls)
	}
	if !strings.Contains(got, "baaytona · deploy@65.109.212.14") || strings.Contains(got, "rentcar") {
		t.Fatalf("tree does not follow the Containers host list:\n%s", got)
	}
}

// 80×24: every line of the Databases sidebar fits.
func TestDBServersTreeFits80x24(t *testing.T) {
	dbServersFakes(t)
	m := dbServersModel(t)
	dbDiscoverNow(t, m)
	out := m.renderDatabasesSidebarRaw(29, 22, true)
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w != 29 {
			t.Fatalf("line %d is %d wide: %q", i, w, ansi.Strip(l))
		}
	}
	if !strings.Contains(ansi.Strip(out), "baaytona") || !strings.Contains(ansi.Strip(out), "+ Add Server…") {
		t.Fatalf("no host drawn:\n%s", ansi.Strip(out))
	}
}
