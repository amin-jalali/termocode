package app

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/containers"
)

// The owner's layout: CONTAINERS itself is the hosts tree. The local
// engine is the first host, open, with its compose projects and
// containers; every other host follows (closed until opened, read once);
// + Add Server… is the last row of CONTAINERS. No "docker 27 · default"
// line, no CONTEXTS section.
//
//	▼ CONTAINERS
//	  ▼ default · this machine (docker 27)   4/6
//	    ▼ termocode  2/3
//	    ...
//	  ▶ baaytona · deploy@65.109.212.14
//	  + Add Server…

// containersSketch is the CONTAINERS section as the owner sketches it:
// one row per line, two spaces per level.
func containersSketch(st *servicesState) string {
	var out []string
	in := false
	for _, r := range st.rows() {
		if r.kind == svcRowSection {
			in = r.section == hostSection("containers")
			continue
		}
		if !in {
			continue
		}
		text := r.name
		if r.kind == svcRowHint || r.kind == svcRowNote {
			text = "!" + r.text
		}
		if r.count != "" {
			text += "  " + r.count
		}
		out = append(out, strings.Repeat("  ", r.depth)+text)
	}
	return strings.Join(out, "\n")
}

func TestContainersSectionIsTheHostTree(t *testing.T) {
	calls := fakeServerLists(t)
	m0 := svcTestModel(serversState(t), 120, 40)
	m := &m0
	want := strings.Join([]string{
		"default · this machine (docker 27)  4/6",
		"  termocode  2/3",
		"    postgres-1",
		"    redis-1",
		"    api-1",
		"  shop  1/1",
		"    web-1",
		"  other  1/2",
		"    nginx",
		"    builder",
		"baaytona · deploy@65.109.212.14",
		"broken · me@down.example.com",
		"+ Add Server…",
	}, "\n")
	if got := containersSketch(m.services); got != want {
		t.Fatalf("CONTAINERS:\n%s\nwant\n%s", got, want)
	}
	if len(calls) != 0 {
		t.Fatalf("remote hosts were read before they were opened: %v", calls)
	}
	for _, r := range m.services.rows() {
		if r.kind == svcRowSection && r.section == hostSection("contexts") {
			t.Fatal("a CONTEXTS section is still drawn: the hosts are in CONTAINERS")
		}
	}
	plain := ansi.Strip(m.renderServicesSidebarRaw(60, 40, true, svcTestNow, -1, -1))
	if strings.Contains(plain, "docker 27 · default") || strings.Contains(plain, "CONTEXTS") {
		t.Fatalf("the old footer line or CONTEXTS is drawn:\n%s", plain)
	}
	if l := lineWith(strings.Split(plain, "\n"), "default · this machine"); !strings.Contains(l, "4/6") {
		t.Fatalf("the local host's header has no counts: %q", l)
	}

	// Opening a remote host puts its projects and containers under it,
	// inside CONTAINERS, before the next host; a failing host says why
	// under its own header.
	openServer(t, m, "baaytona")
	openServer(t, m, "broken")
	want = strings.Join([]string{
		"baaytona · deploy@65.109.212.14  3/3",
		"  baaytona  3/3",
		"    baaytona-db-1",
		"    baaytona-scraper-redis",
		"    baaytona-web-1",
		"broken · me@down.example.com",
		"  !ssh: Connection timed out",
		"+ Add Server…",
	}, "\n")
	if got := containersSketch(m.services); !strings.HasSuffix(got, want) {
		t.Fatalf("CONTAINERS after opening the hosts:\n%s\nwant it to end with\n%s", got, want)
	}
}

// Space folds the local host like any other; its containers keep their
// keys (they are the same rows as before).
func TestContainersLocalHostFoldsAndKeepsItsKeys(t *testing.T) {
	fakeServerLists(t)
	m0 := svcTestModel(serversState(t), 120, 40)
	m := &m0
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostContext && r.hostID == "default" })
	_ = press(m, " ")
	if strings.Contains(containersSketch(m.services), "postgres-1") {
		t.Fatalf("the local host did not fold:\n%s", containersSketch(m.services))
	}
	_ = press(m, " ")
	r := cursorTo(t, m, func(r svcRow) bool { return r.host == hostContainer && r.name == "postgres-1" })
	if r.depth != 2 {
		t.Fatalf("a local container sits at depth %d, want 2 (host, project, container)", r.depth)
	}
	if c, ok := m.selectedContainer(); !ok || c.ID != "c1" {
		t.Fatalf("the local container row lost its keys: %+v %v", c, ok)
	}
}

// Enter on another host still uses it in this session (CONTEXTS' old
// action), and the host row keeps its menu.
func TestContainersHostRowStillUsesTheContext(t *testing.T) {
	fakeServerLists(t)
	m0 := svcTestModel(serversState(t), 120, 40)
	m := &m0
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostContext && r.hostID == "baaytona" })
	_ = press(m, "enter")
	if m.services.sessionContext != "baaytona" {
		t.Fatalf("Enter on a host did not use it (session context %q)", m.services.sessionContext)
	}
}

// A container of another host: j moves past it, s and x start and stop
// it on that host (its own context), and the host is read again.
func TestContainersRemoteContainerKeys(t *testing.T) {
	calls := fakeServerLists(t)
	var got []string
	old := serverStartStop
	serverStartStop = func(_ context.Context, e containers.Engine, verb, id string) error {
		got = append(got, e.Context+" "+verb+" "+id)
		return nil
	}
	t.Cleanup(func() { serverStartStop = old })
	m0 := svcTestModel(serversState(t), 120, 40)
	m := &m0
	openServer(t, m, "baaytona")
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostServerContainer && r.name == "baaytona-db-1" })
	_ = press(m, "j")
	if r, _ := m.servicesCurrentRow(); r.name != "baaytona-scraper-redis" {
		t.Fatalf("j on a remote container moved to %q", r.name)
	}
	for _, key := range []string{"x", "s"} {
		runHostTreeCmds(m, press(m, key))
	}
	if want := "baaytona stop c0ffee02 · baaytona start c0ffee02"; strings.Join(got, " · ") != want {
		t.Fatalf("remote actions %q, want %q", strings.Join(got, " · "), want)
	}
	if calls["baaytona"] != 3 {
		t.Fatalf("baaytona was read %d times, want once to open and once after each action", calls["baaytona"])
	}
}

// runHostTreeCmds runs a command and its batches, and gives the model the host
// tree's messages they answer.
func runHostTreeCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runHostTreeCmds(m, c)
		}
	case serverActionMsg:
		runHostTreeCmds(m, m.onServerAction(msg))
	case serverListMsg:
		_ = m.onServerList(msg)
	}
}
