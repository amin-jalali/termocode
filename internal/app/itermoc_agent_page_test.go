package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// imAgentSnap puts one connected agent with two always-allowed tools and
// calls of two agents into the snapshot.
func imAgentSnap(m *Model) {
	imInject(m, func(sn *itermocSnap) {
		sn.listening, sn.mode = true, "auto"
		sn.agents = []imAgent{{id: 1, name: "claude-code", since: "09:41", calls: 14, asked: 2}}
		sn.calls = []imCall{
			"09:42 claude-code · tasks_run · asked, you allowed",
			"09:40 other-agent · file_read · auto",
		}
		sn.allow = map[string][]string{"claude-code": {"tasks_run", "execute_action:format-document"}}
	})
}

// Spec: an agent's page draws termocode ↔ agent with its calls and how
// often it asked, lists what it may do without asking (x revokes) and
// only ITS recent calls; pills Revoke…, Copy config, Status. Every row
// fits the pane, and a narrow pane gets one line instead of the boxes.
func TestItermocAgentPage(t *testing.T) {
	m := imPage(t, imSecAgents, agentKey(1))
	imAgentSnap(m)
	f := imPageText(m, 160, 46)
	for _, w := range []string{"claude-code", "connected since 09:41", "termocode", "listening · auto", "14 calls", "2 asked you", "↔",
		"ALWAYS ALLOWED", "tasks_run", "action format-document", "x revokes", "RECENT CALLS", "tasks_run · asked, you allowed",
		"Revoke… (x)", "Copy config (y)", "Status (T)", "Disconnect…"} {
		if !strings.Contains(f, w) {
			t.Errorf("no %q in\n%s", w, f)
		}
	}
	if strings.Contains(f, "other-agent") {
		t.Errorf("another agent's call is on this page:\n%s", f)
	}
	for _, sz := range [][2]int{{160, 46}, {80, 22}, {46, 21}} {
		for i, l := range strings.Split(imPageText(m, sz[0], sz[1]), "\n") {
			if ansi.StringWidth(l) != sz[0] {
				t.Errorf("%dx%d: row %d is %d wide", sz[0], sz[1], i, ansi.StringWidth(l))
			}
		}
	}
	if n := imPageText(m, 60, 30); !strings.Contains(n, "termocode ↔ claude-code · 14 calls") || strings.Contains(n, "╭") {
		t.Errorf("narrow page:\n%s", n)
	}
	// Nothing allowed: the page says every change asks; Revoke… is not the
	// main pill.
	imInject(m, func(sn *itermocSnap) { sn.allow = nil })
	f = imPageText(m, 160, 46)
	if !strings.Contains(f, "every call that changes something asks you first") {
		t.Errorf("no empty ALWAYS ALLOWED line:\n%s", f)
	}
}

// Spec: the listening row's page draws the server and the first agent, and
// the agents that are not running beside them, with Start pills; not
// listening, it says why and the wire is empty.
func TestItermocAgentStatusPage(t *testing.T) {
	m := imPage(t, imSecAgents, "status")
	imAgentSnap(m)
	f := imPageText(m, 160, 46)
	for _, w := range []string{"Outside agents", "listening · auto", "claude-code", "Codex · not running", "Start Claude Code (s)", "Start Codex (S)",
		"Copy config (y)", "LAST CALLS", "Stop listening…"} {
		if !strings.Contains(f, w) {
			t.Errorf("no %q in\n%s", w, f)
		}
	}
	if strings.Contains(f, "Claude Code · not running") {
		t.Errorf("a connected agent is listed as not running:\n%s", f)
	}
	imInject(m, func(sn *itermocSnap) {
		sn.listening, sn.agents, sn.calls = false, nil, nil
		sn.whySentence = "It starts when you start an agent tab."
	})
	f = imPageText(m, 160, 46)
	for _, w := range []string{"not listening", "nobody yet", "no agent yet", "It starts when you start an agent tab.", "Start listening now"} {
		if !strings.Contains(f, w) {
			t.Errorf("not listening: no %q in\n%s", w, f)
		}
	}
}

// Spec: x on an agent page asks first (Keep is first); Esc keeps the list;
// Revoke forgets only THIS agent's always-allow in config.json, other
// agents keep theirs. With nothing to revoke there is no card.
func TestItermocAgentRevoke(t *testing.T) {
	m := imPage(t, imSecAgents, agentKey(1))
	if err := m.mcpServeAllowAdd("claude-code", "tasks_run"); err != nil {
		t.Fatal(err)
	}
	if err := m.mcpServeAllowAdd("other-agent", "file_read"); err != nil {
		t.Fatal(err)
	}
	// The page reads the snapshot again after a key; the agent is put back
	// each time (no real bridge runs in a unit test).
	connect := func() {
		imInject(m, func(sn *itermocSnap) {
			sn.listening = true
			sn.agents = []imAgent{{id: 1, name: "claude-code", since: "09:41"}}
		})
	}
	connect()
	if !imPageKey(m, "x") || !m.confirmOpen || m.confirmKind != confirmKindItermocAgentRevoke {
		t.Fatal("x did not open the Revoke card")
	}
	v := ansi.Strip(m.confirm.View())
	if !strings.Contains(v, "claude-code") || !strings.Contains(v, "tasks_run") || strings.Index(v, "Keep") > strings.LastIndex(v, "Revoke") || !strings.Contains(v, "1 tool runs") {
		t.Errorf("card:\n%s", v)
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc: Keep
	if got := mcpServeAllowLoad()["claude-code"]; len(got) != 1 {
		t.Fatalf("Esc revoked: %v", got)
	}
	connect()
	if !imPageKey(m, "x") || !m.confirmOpen {
		t.Fatal("x did not open the card again")
	}
	m.confirmOpen = false
	m.handleAIConfirm("revoke")
	list := mcpServeAllowLoad()
	if len(list["claude-code"]) != 0 {
		t.Errorf("Revoke left %v", list["claude-code"])
	}
	if len(list["other-agent"]) != 1 {
		t.Errorf("Revoke touched another agent: %v", list)
	}
	connect()
	if !imPageKey(m, "x") || m.confirmOpen {
		t.Error("a card for an empty list")
	}
}

// Spec (bug: the header's sub text was cut to 6 cells even with no room,
// so it ran into the right-hand state, "outsi● connected since 09:41"):
// in a narrow pane the sub text is left out, the state stays whole and a
// space keeps them apart.
func TestItermocPageHeaderNarrowNoOverlap(t *testing.T) {
	m := imPage(t, imSecAgents, agentKey(1))
	imAgentSnap(m)
	for _, w := range []int{46, 50, 60} {
		head := strings.Split(imPageText(m, w, 24), "\n")[0]
		at := strings.Index(head, "● connected since 09:41")
		if at < 0 {
			t.Errorf("w=%d: the state is not whole: %q", w, head)
			continue
		}
		if at == 0 || head[at-1] != ' ' {
			t.Errorf("w=%d: the state touches the text before it: %q", w, head)
		}
		if strings.Contains(head, "outsi") && !strings.Contains(head, "outside agent") {
			t.Errorf("w=%d: a cut sub text: %q", w, head)
		}
	}
}
