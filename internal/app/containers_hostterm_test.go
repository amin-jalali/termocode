package app

import (
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/containers"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Open Terminal on a host row: an ssh:// host opens a terminal tab
// running ssh to it (user, host and port from the context URI, as argv,
// never a shell line); the local host opens a plain shell in the project
// folder. The spawn is a fake: ssh never runs.

func fakeHostTerminals(t *testing.T) *[]terminalJob {
	t.Helper()
	var got []terminalJob
	old := spawnHostTerminal
	spawnHostTerminal = func(_ *Model, j terminalJob) error {
		got = append(got, j)
		return nil
	}
	t.Cleanup(func() { spawnHostTerminal = old })
	return &got
}

func TestHostRowOpenTerminal(t *testing.T) {
	got := fakeHostTerminals(t)
	fakeServerLists(t)
	st := serversState(t)
	st.contextRows = append(st.contextRows,
		containers.Context{Name: "box", DockerEndpoint: "ssh://ops@10.0.0.9:2222"},
		containers.Context{Name: "evil", DockerEndpoint: "ssh://-oProxyCommand=touch@x"})
	st.touch()
	m0 := svcTestModel(st, 120, 40)
	m := &m0

	// t on a remote host row.
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostContext && r.hostID == "baaytona" })
	_ = press(m, "t")
	// The row's menu has it too.
	r := cursorTo(t, m, func(r svcRow) bool { return r.host == hostContext && r.hostID == "box" })
	_ = m.handleContextRowMenuSelect("terminal", r)
	if len(*got) != 2 {
		t.Fatalf("%d terminals opened, want 2", len(*got))
	}
	if a := (*got)[0].Argv; !reflect.DeepEqual(a, []string{"ssh", "--", "deploy@65.109.212.14"}) {
		t.Errorf("baaytona argv %q", a)
	}
	if a := (*got)[1].Argv; !reflect.DeepEqual(a, []string{"ssh", "-p", "2222", "--", "ops@10.0.0.9"}) {
		t.Errorf("box argv %q", a)
	}
	if (*got)[0].Cmdline != "" || !strings.Contains((*got)[0].Label, "baaytona") {
		t.Errorf("job %+v: want argv only and a label naming the host", (*got)[0])
	}

	// A host that would be read as an ssh option opens nothing.
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostContext && r.hostID == "evil" })
	_ = press(m, "t")
	if len(*got) != 2 {
		t.Fatalf("a hostile host opened a terminal: %+v", (*got)[2:])
	}

	// The local host: a plain shell in the project folder.
	cursorTo(t, m, func(r svcRow) bool { return r.host == hostContext && r.hostID == "default" })
	_ = press(m, "t")
	if len(*got) != 3 || (*got)[2].Dir != st.root || len((*got)[2].Argv) != 1 {
		t.Fatalf("local terminal %+v, want one shell in %s", *got, st.root)
	}
}

// Open Terminal on a host of the Databases view's SERVERS: t on its row.
func TestDBServersHostOpenTerminal(t *testing.T) {
	got := fakeHostTerminals(t)
	dbServersFakes(t)
	m := dbServersModel(t)
	dbDiscoverNow(t, m)
	s := m.databases
	for i, r := range s.rows() {
		if strings.HasPrefix(r.name, "rentcar · ") {
			s.cursor = i
		}
	}
	next, _ := m.handleDatabasesSidebarKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	*m = next
	if len(*got) != 1 || !reflect.DeepEqual((*got)[0].Argv, []string{"ssh", "-p", "2222", "--", "ops@rentcar.example.com"}) {
		t.Fatalf("terminals %+v", *got)
	}
}

// From the page: containers.host_terminal waits for the laptop's card
// (Cancel first, the ssh argv in it), runs nothing before the yes, then
// opens the same terminal tab; an unlisted host is refused.
func TestWebHostTerminal(t *testing.T) {
	got := fakeHostTerminals(t)
	m, c, _ := webServersHub(t)
	if f := webTestCallBg(t, m, c, webproto.OpContainersHostTerminal, `{"name":"baaytona"}`); webTestErr(f) != "not_found" {
		t.Fatalf("a host before the list: %v", f)
	}
	var hs webTestHosts
	webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersServers, `{}`), &hs)
	for _, h := range []struct{ name, inCard string }{{"baaytona", "ssh -- deploy@65.109.212.14"}, {"broken", "ssh -p 2222 -- ops@10.9.9.9"}, {"default", "project folder"}} {
		n := len(*got)
		var ref webproto.AskRef
		webTestData(t, webTestCallBg(t, m, c, webproto.OpContainersHostTerminal, `{"name":"`+h.name+`"}`), &ref)
		if ref.Card == "" || !m.confirmOpen || m.confirmKind != confirmKindWebAsk || m.confirm.SafeID() != "cancel" {
			t.Fatalf("%s: no laptop card", h.name)
		}
		if v := m.confirm.View(); !strings.Contains(strings.Join(strings.Fields(ansi.Strip(v)), " "), h.inCard) {
			t.Errorf("%s card:\n%s", h.name, v)
		}
		if len(*got) != n {
			t.Fatalf("%s: a terminal opened before the laptop's yes", h.name)
		}
		webTestLaptopSelect(t, m, "confirm")
		if len(*got) != n+1 {
			t.Fatalf("%s: no terminal after the yes", h.name)
		}
	}
	if a := (*got)[0].Argv; !reflect.DeepEqual(a, []string{"ssh", "--", "deploy@65.109.212.14"}) {
		t.Errorf("baaytona argv %q", a)
	}
	if a := webTestAudit(m); !strings.Contains(a, "containers.host_terminal") {
		t.Errorf("not audited:\n%s", a)
	}
}
