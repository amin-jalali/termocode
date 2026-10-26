package app

import (
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/mcp"
)

// imPageKey presses one key on the shown page.
func imPageKey(m *Model, s string) bool {
	nm, _, ok := m.routeToItermocPage(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	*m = nm
	return ok
}

// Spec: each state of a service has its own calm page: a running one
// shows its tool names and no card; an off one says it is kept and offers
// Enable; a failed one has a card with the problem; an untrusted one says
// so. A token in the entry's address is never on the page (only the host).
func TestItermocServicePageStates(t *testing.T) {
	m := imPage(t, imSecServices, "github")
	imInject(m, func(sn *itermocSnap) {
		sn.services = []mcp.Status{
			{Name: "github", Type: mcp.TypeHTTP, State: mcp.StateRunning, Tools: 3, Trusted: true, SignedIn: true},
			{Name: "notes", Type: mcp.TypeStdio, State: mcp.StateDisabled, Tools: 11, Trusted: true},
			{Name: "broken", Type: mcp.TypeStdio, State: mcp.StateFailed, Problem: "exec: \"nope\": not found", Trusted: true},
			{Name: "wiki", Type: mcp.TypeStdio, State: mcp.StateStopped, Tools: 4, Cached: true},
		}
		sn.svc = map[string]imSvc{
			"github": {where: "remote · api.githubcopilot.com", access: "read only", browser: true,
				tools: []string{"issues_list", "pulls_get", "repos_search"}, file: "/cfg/mcp.json"},
			"notes": {where: "local · notes-mcp", file: "/cfg/mcp.json"},
		}
	})
	cases := []struct {
		name        string
		wants, nots []string
	}{
		{"github", []string{"running", "api.githubcopilot.com", "read only", "stored in the secret store", "issues_list", "repos_search",
			"Show tools (t)", "Disable… (d)", "Log (l)", "Allow writes", "Sign out"}, []string{"Sign-in", "Enable"}},
		{"notes", []string{"off · kept in mcp.json", "Enable (d)", "Remove… (D)"}, []string{"Disable", "╭"}},
		{"broken", []string{"It does not start", "not found", "Log (l)"}, nil},
		{"wiki", []string{"Not trusted yet", "from the last run", "Trust"}, nil},
	}
	for _, c := range cases {
		m.itermocPage.name = c.name
		f := imPageText(m, 160, 46)
		for _, w := range c.wants {
			if !strings.Contains(f, w) {
				t.Errorf("%s: no %q in\n%s", c.name, w, f)
			}
		}
		for _, w := range c.nots {
			if strings.Contains(f, w) {
				t.Errorf("%s: %q should not be on the page:\n%s", c.name, w, f)
			}
		}
		for _, sz := range [][2]int{{160, 46}, {80, 22}, {46, 21}} {
			for i, l := range strings.Split(imPageText(m, sz[0], sz[1]), "\n") {
				if ansi.StringWidth(l) != sz[0] {
					t.Errorf("%s %dx%d: row %d is %d wide", c.name, sz[0], sz[1], i, ansi.StringWidth(l))
				}
			}
		}
	}
}

// Spec: the facts come from the real entry: the host of a remote address
// (never its path or query, where a token can sit), the command of a local
// one (never its arguments), and the file it lives in.
func TestItermocServiceInfoNeverShowsSecrets(t *testing.T) {
	aiTestModel(t)
	writeUserMCP(t, `{"mcpServers": {
  "remote": {"type": "http", "url": "https://mcp.example.com/v1?token=TOPSECRET"},
  "local": {"command": "/usr/bin/tool-mcp", "args": ["--key", "TOPSECRET"]}
}}`)
	r := itermocServiceInfo(mcp.Status{Name: "remote", Type: mcp.TypeHTTP}, nil)
	l := itermocServiceInfo(mcp.Status{Name: "local", Type: mcp.TypeStdio}, nil)
	if r.where != "remote · mcp.example.com" || !r.browser {
		t.Errorf("remote: %+v", r)
	}
	if l.where != "local · tool-mcp" || l.browser {
		t.Errorf("local: %+v", l)
	}
	if r.file != mcpUserPath() {
		t.Errorf("file %q", r.file)
	}
	for _, in := range []imSvc{r, l} {
		if strings.Contains(in.where+in.access+in.account+strings.Join(in.tools, ","), "TOPSECRET") {
			t.Errorf("a secret reached the page facts: %+v", in)
		}
	}
}

// Spec: d on a service page asks first (Keep on is the first button) and
// only Disable writes "disabled": true; d on an off service turns it on at
// once (safe); D opens the Remove card, Keep first; t lists the tools.
func TestItermocServiceDisableEnableRemove(t *testing.T) {
	m := imPage(t, imSecServices, "fake")
	writeUserMCP(t, fakeServiceEntry(nil))
	m.aiS().mcp = nil
	m.aiMCP()
	m.itermocS().refresh(*m, true)
	if _, ok := m.itermocSnapOf().service("fake"); !ok {
		t.Fatal("the service is not in the snapshot")
	}
	if !imPageKey(m, "d") || !m.confirmOpen || m.confirmKind != confirmKindMCPDisable {
		t.Fatalf("d did not open the Disable card (open %v kind %v)", m.confirmOpen, m.confirmKind)
	}
	if v := ansi.Strip(m.confirm.View()); !strings.Contains(v, "Disable service fake?") || !strings.Contains(v, "Keep on") {
		t.Errorf("card:\n%s", v)
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc: Keep on
	if b, _ := os.ReadFile(mcpUserPath()); strings.Contains(string(b), "disabled") {
		t.Fatalf("Esc wrote the entry: %s", b)
	}
	imPageKey(m, "d")
	m.confirmOpen = false
	m.handleAIConfirm("disable")
	if b, _ := os.ReadFile(mcpUserPath()); !strings.Contains(string(b), `"disabled": true`) {
		t.Fatalf("Disable did not write: %s", b)
	}
	m.itermocRefreshNow()
	if st := serverStatus(m, "fake"); st.State != mcp.StateDisabled {
		t.Fatalf("state %q after Disable", st.State)
	}
	if f := imPageText(m, 160, 46); !strings.Contains(f, "Enable (d)") {
		t.Errorf("the off page offers no Enable:\n%s", f)
	}
	if !imPageKey(m, "d") || m.confirmOpen {
		t.Error("Enable asked; it is safe and runs at once")
	}
	if b, _ := os.ReadFile(mcpUserPath()); strings.Contains(string(b), "disabled") {
		t.Errorf("Enable did not write: %s", b)
	}
	m.itermocRefreshNow()
	if !imPageKey(m, "D") || !m.confirmOpen || m.confirmKind != confirmKindMCPRemove {
		t.Fatal("D did not open the Remove card")
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc: Keep
	if _, err := os.Stat(mcpUserPath()); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(mcpUserPath()); !strings.Contains(string(b), `"fake"`) {
		t.Errorf("Esc on Remove removed the entry: %s", b)
	}
	if mcpToolCount(m) == 0 { // connect: the tools are known again
		t.Fatal("no tools after Enable")
	}
	if !imPageKey(m, "t") || !m.pickerOpen {
		t.Error("t did not list the tools")
	}
}

// Spec: a click on the card's Sign in pill runs the sign-in verb (the
// pill is a target inside the card).
func TestItermocServiceCardPillIsATarget(t *testing.T) {
	m := imPage(t, imSecServices, "linear")
	imInject(m, func(sn *itermocSnap) {
		sn.services = []mcp.Status{{Name: "linear", Type: mcp.TypeHTTP, State: mcp.StateSignIn, Trusted: true}}
	})
	lay := m.itermocPageLayout(160, 46)
	n := 0
	for _, h := range lay.hits {
		if h.id == "im:pg:signin" {
			n++
			row := ansi.Strip(lay.rows[h.y])
			if !strings.Contains(row, "Sign in (i)") {
				t.Errorf("the target at row %d is not on the pill: %q", h.y, row)
			}
		}
	}
	if n != 2 {
		t.Errorf("%d Sign in targets, want 2 (the card and the bar)", n)
	}
}
