package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// The MCP servers screens (ai_mcp.go): row hints, the trust card's
// answers, each action of a server, Connect with the fake server, and
// the mcp.json that is made when missing.

func covMCPModel(t *testing.T) *Model {
	t.Helper()
	m := aiTestModel(t)
	t.Cleanup(func() {
		if m.ai != nil && m.ai.mcp != nil {
			m.ai.mcp.Close()
		}
	})
	return &m
}

func TestCovAIMCPStatusHints(t *testing.T) {
	covMCPModel(t)
	for _, c := range []struct {
		st   mcp.Status
		want string
	}{
		{mcp.Status{Name: "x", Type: mcp.TypeStdio, State: mcp.StateInvalid, Problem: "no command", Trusted: true}, "local · no command"},
		{mcp.Status{Name: "x", Type: mcp.TypeStdio, State: mcp.StateFailed, Problem: "exit 1", Trusted: true}, "local · failed: exit 1"},
		{mcp.Status{Name: "x", Type: mcp.TypeHTTP, State: mcp.StateRunning, Trusted: true, SignedIn: true, Tools: 3, Cached: true, Source: mcp.SourceProject},
			"remote · connected · signed in · 3 tools (cached) · project"},
		{mcp.Status{Name: "x", Type: mcp.TypeStdio, State: mcp.StateRunning, ToolsChanged: true, Shared: true, Sessions: 2, Pid: 42},
			"tools changed · not trusted · local · running · shared · "},
		{mcp.Status{Name: "x", State: mcp.StateDisabled, Source: mcp.SourceProject}, "off · kept in mcp.json, not started, no tools · project"},
	} {
		if got := mcpStatusHint(c.st); !strings.HasPrefix(got, c.want) {
			t.Errorf("hint = %q, want %q", got, c.want)
		}
	}
	for st, want := range map[string]string{mcp.StateRunning: "●", mcp.StateDisabled: "○", mcp.StateFailed: "✗", mcp.StateInvalid: "✗", mcp.StateSignIn: "!", "stopped": "○"} {
		if got := mcpStatusIcon(mcp.Status{State: st}); got != want {
			t.Errorf("icon %s = %q", st, got)
		}
	}
	if mcpStatusIcon(mcp.Status{State: mcp.StateRunning, Expired: true}) != "!" {
		t.Error("an expired sign-in is not marked")
	}
	// A known service: account, access and state.
	writeUserMCP(t, `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/readonly","auth":{"gh":"octo"}},`+
		`"linear":{"type":"http","url":"https://mcp.linear.app/mcp"}}}`)
	for _, c := range []struct {
		st   mcp.Status
		want string
	}{
		{mcp.Status{Name: "github", Type: mcp.TypeHTTP, State: mcp.StateSignIn, Trusted: true}, "octo · read only · sign-in needed"},
		{mcp.Status{Name: "github", Type: mcp.TypeHTTP, State: mcp.StateFailed, Problem: "500"}, "octo · read only · failed: 500 · not trusted"},
		{mcp.Status{Name: "linear", Type: mcp.TypeHTTP, State: "stopped", Trusted: true, Source: mcp.SourceProject}, "read and write · not signed in · project"},
		{mcp.Status{Name: "linear", Type: mcp.TypeHTTP, State: mcp.StateRunning, Expired: true, Trusted: true}, "read and write · sign-in expired"},
	} {
		if got := mcpStatusHint(c.st); got != c.want {
			t.Errorf("service hint = %q, want %q", got, c.want)
		}
	}
}

// The trust card: a stale chat is denied; Once / Always answer the chat;
// a denied tools change warns; from the servers list Allow connects or
// signs in.
func TestCovAIMCPTrustAnswers(t *testing.T) {
	m := covMCPModel(t)
	if m.answerMCPTrust("once") != nil {
		t.Error("an answer with nothing pending did something")
	}
	reply := make(chan mcp.TrustAnswer, 1)
	m.onAIMCPTrust(aiMCPTrustMsg{gen: m.aiS().chatGen + 9, server: mcp.Server{Name: "x"}, reply: reply, ch: make(chan tea.Msg, 1)})
	if got := <-reply; got != mcp.TrustDeny {
		t.Errorf("stale chat answer = %v", got)
	}
	for id, want := range map[string]mcp.TrustAnswer{"once": mcp.TrustOnce, "always": mcp.TrustAlways, "deny": mcp.TrustDeny} {
		m.aiS().mcpTrust = &aiMCPTrustPending{server: "x", reply: reply}
		m.answerMCPTrust(id)
		if got := <-reply; got != want {
			t.Errorf("%s = %v", id, got)
		}
	}
	m.aiS().mcpTrust = &aiMCPTrustPending{server: "x", reply: reply, toolsChanged: true}
	if m.answerMCPTrust("deny") == nil || !strings.Contains(covToastText(m), "its tools changed") {
		t.Errorf("tools changed toast = %q", covToastText(m))
	}
	<-reply
	m.aiS().mcpTrust = &aiMCPTrustPending{server: "x", connect: true}
	if m.answerMCPTrust("once") == nil {
		t.Error("Allow once from the list did not connect")
	}
	m.aiS().mcpTrust = &aiMCPTrustPending{server: "x", signin: true}
	if m.answerMCPTrust("always") == nil {
		t.Error("Always from Sign in did not sign in")
	}
	m.aiS().mcpTrust = &aiMCPTrustPending{server: "x", connect: true}
	if m.answerMCPTrust("deny") != nil {
		t.Error("Deny from the list did something")
	}
}

// Server actions on the fake server: Connect asks to trust first; Trust
// saves; Connect lists the tools; the result names them; a failed
// connect is a toast; secrets rows show saved / not set.
func TestCovAIMCPServerActionsWithFake(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	m := covMCPModel(t)
	pp := mcp.ProjectPath(aiWorkspaceRoot())
	_ = os.MkdirAll(filepath.Dir(pp), 0o755)
	if err := os.WriteFile(pp, []byte(mcptest.Config(map[string]map[string]string{"fake": nil})), 0o644); err != nil {
		t.Fatal(err)
	}
	m.aiS().mcpSel = "fake"
	if m.onMCPServerAction("connect") != nil || !m.confirmOpen || m.confirmKind != confirmKindMCPTrust {
		t.Fatalf("Connect on an untrusted server did not ask to trust it: %+v", serverStatus(m, "fake"))
	}
	m.confirmOpen = false
	m.aiS().mcpTrust = nil
	m.aiS().mcpSel = "fake"
	if m.onMCPServerAction("signin") != nil || !m.confirmOpen {
		t.Error("Sign in on an untrusted server did not ask to trust it")
	}
	m.confirmOpen = false
	m.aiS().mcpTrust = nil
	m.onMCPServerAction("trust")
	if !serverStatus(m, "fake").Trusted {
		t.Fatal("Trust did not trust")
	}
	done, ok := covCollect[mcpConnectDoneMsg](m.onMCPServerAction("connect"))
	if !ok || done.err != nil {
		t.Fatalf("connect: %+v", done)
	}
	if m.onMCPConnectDone(done) == nil || !strings.Contains(covToastText(m), "MCP fake: ") || !strings.Contains(covToastText(m), "tools") {
		t.Errorf("connect done toast = %q", covToastText(m))
	}
	if m.onMCPConnectDone(mcpConnectDoneMsg{name: "fake", err: errors.New("spawn failed")}) == nil {
		t.Error("a failed connect gave no toast")
	}
	if m.onMCPConnectDone(mcpConnectDoneMsg{name: "gone"}) != nil {
		t.Error("a connect of a server that is gone did something")
	}
	m.onMCPServerAction("accept-tools")
	m.onMCPServerAction("edit")
	m.onMCPServerAction("log")
	if m.onMCPServerAction("no-such-action") != nil {
		t.Error("an unknown action did something")
	}
	m.aiS().mcpSel = "gone"
	if m.onMCPServerAction("connect") != nil {
		t.Error("an action on a server that is gone did something")
	}
	// Secret rows: one saved, one not.
	writeUserMCP(t, `{"mcpServers":{"api":{"type":"http","url":"https://api.example/mcp","headers":{"Authorization":"Bearer ${secret:api_tok}","X-Other":"${secret:other_tok}"}}}}`)
	m.aiMCP().Reload()
	if err := (mcp.CredStore{Path: mcpCredPath()}).SetSecret("api_tok", "tok-cov-api-123456789"); err != nil {
		t.Fatal(err)
	}
	hints := map[string]string{}
	for _, it := range mcpServerActionItems(m, "api") {
		hints[it.ID] = it.Hint
	}
	if hints["secret:api_tok"] != "saved" || hints["secret:other_tok"] != "not set" {
		t.Errorf("secret rows = %v", hints)
	}
	if _, ok := hints["signin"]; ok {
		t.Error("an entry with its own Authorization header offers Sign in")
	}
	m.aiS().mcpSel = "api"
	m.onMCPServerAction("secret:other_tok")
	if !m.promptOpen {
		t.Error("Set secret did not ask for it")
	}
}

// Edit in mcp.json makes the file with an empty block when missing.
func TestCovAIMCPEditConfigCreates(t *testing.T) {
	m := covMCPModel(t)
	p := mcpUserPath()
	_ = os.Remove(p)
	if m.editMCPConfig() == nil {
		t.Error("no hint toast")
	}
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), `"mcpServers"`) {
		t.Errorf("mcp.json: %q %v", b, err)
	}
	t.Setenv("XDG_CACHE_HOME", "")
	if !strings.HasSuffix(mcpCacheDir(), "/.cache/termocode/mcp") {
		t.Errorf("cache dir = %q", mcpCacheDir())
	}
}
