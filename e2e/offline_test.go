//go:build e2e

package e2e

import (
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// SCN-13: termocode with no network. Nothing here reaches a real host:
// every address is a local port that refuses (a listener opened and
// closed again) or a local server that accepts and never answers, and
// the proxy variables point at a refusing port, so a request for any
// outside host fails at once, without a DNS lookup.

// refusedAddr is a local "host:port" where nothing listens.
func refusedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	return addr
}

// silentAddr is a local "host:port" that accepts connections and never
// sends a byte, like a network that swallows packets.
func silentAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return l.Addr().String()
}

// offlineEnv points every proxy variable at a refusing port, so any
// outside host fails at once.
func offlineEnv(t *testing.T) []string {
	t.Helper()
	proxy := "http://" + refusedAddr(t)
	var env []string
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"} {
		env = append(env, k+"="+proxy)
	}
	// The local fakes are reached directly (Go never proxies them; curl,
	// and so git, would without this).
	return append(env, "NO_PROXY=127.0.0.1,localhost", "no_proxy=127.0.0.1,localhost")
}

// startOffline starts termocode with config.json body cfg (may be "").
func startOffline(t *testing.T, ws, cfg string, env ...string) *Term {
	t.Helper()
	home := t.TempDir()
	if cfg != "" {
		writeFile(t, filepath.Join(home, ".config", "termocode", "config.json"), cfg)
	}
	tm := Start(t, StartOpts{Home: home, Workspace: ws, Cols: 140, Rows: 40,
		Env: append(offlineEnv(t), env...)})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	return tm
}

func aiOfflineConfig(addr string) string {
	return `{"ai_provider": "openai", "ai_model": "gpt-offline", "ai_base_url": "http://` + addr + `/v1"}`
}

// paletteWorks opens and closes the command palette: the UI still
// takes keys while something waits on the network.
func (tm *Term) paletteWorks() {
	tm.t.Helper()
	tm.Send(KeyF1)
	tm.WaitText("Command Palette", uiTimeout)
	tm.Send(KeyEsc)
	tm.WaitGone("Command Palette", uiTimeout)
}

// Spec (SCN-13): the AI server refuses: the chat says so in plain
// words at once, with no memory card under it.
func TestOfflineAIServerRefused(t *testing.T) {
	tm := startOffline(t, NewRepo(t), aiOfflineConfig(refusedAddr(t)), "TERMOCODE_AI_API_KEY=sk-offline-FAKE")
	tm.Send("\x1ba")
	tm.WaitText("AI CHAT", uiTimeout)
	tm.settle()
	tm.Type("are you there")
	tm.Send(KeyEnter)
	tm.WaitText("No connection to the AI server", uiTimeout)
	tm.WaitText("refused the", uiTimeout)
	tm.settle()
	if tm.Has("dial tcp") || tm.Has("Save lessons") {
		t.Fatalf("raw error or memory card:\n%s", tm.Screen())
	}
	tm.paletteWorks()
}

// Spec (SCN-13): the AI server accepts and never answers: the chat
// shows it waits, the UI still takes keys, and Ctrl+C stops the turn.
func TestOfflineAIServerSilent(t *testing.T) {
	tm := startOffline(t, NewRepo(t), aiOfflineConfig(silentAddr(t)), "TERMOCODE_AI_API_KEY=sk-offline-FAKE")
	tm.Send("\x1ba")
	tm.WaitText("AI CHAT", uiTimeout)
	tm.settle()
	tm.Type("are you there")
	tm.Send(KeyEnter)
	tm.WaitText("waiting for the model", uiTimeout)
	tm.paletteWorks()
	tm.Send("\x1ba") // back to the chat
	tm.settle()
	tm.Send("\x03") // Ctrl+C: stop
	tm.WaitGone("waiting for the model", uiTimeout)
	tm.WaitGone("Ctrl+C stop", uiTimeout)
}

// Spec (SCN-13): fetch from a server that never answers ends with a
// clear "too slow" error (the test shortens git's time to 3 s), and a
// push after it runs instead of "already running"; the refused push
// says it could not connect. The UI takes keys meanwhile.
func TestOfflineGitRemote(t *testing.T) {
	ws := NewRepo(t)
	gitIn(t, ws, "remote", "add", "origin", "http://"+silentAddr(t)+"/repo.git")
	tm := startOffline(t, ws, "", "GIT_HTTP_LOW_SPEED_TIME=3")
	tm.Palette("Git: Fetch")
	tm.WaitText("Fetching", uiTimeout)
	tm.paletteWorks()
	tm.WaitText("Fetch failed", 30*time.Second)
	tm.WaitText("too slow", uiTimeout)

	gitIn(t, ws, "remote", "set-url", "origin", "http://"+refusedAddr(t)+"/repo.git")
	tm.Palette("Git: Push")
	tm.WaitText("Push failed", 30*time.Second)
	tm.WaitText("Failed to connect", uiTimeout)
	if tm.Has("already running") {
		t.Fatalf("the failed fetch still blocks:\n%s", tm.Screen())
	}
}

// Spec (SCN-13): the HTTP client: a refused request and one that gets no
// answer before its timeout each end with one clear line.
func TestOfflineHTTPClient(t *testing.T) {
	ws := NewRepo(t)
	writeFile(t, filepath.Join(ws, "api.http"), "### refused\n# @name refused\nGET http://"+refusedAddr(t)+"/x\n\n"+
		"### silent\n# @name silent\n# @timeout 2s\nGET http://"+silentAddr(t)+"/x\n")
	tm := startOffline(t, ws, "")
	for _, c := range []struct{ name, want string }{
		{"refused", "Could not connect."},
		{"silent", "timed out after 2 s"},
	} {
		tm.Palette("HTTP: Send Request…")
		tm.WaitText(c.name, uiTimeout)
		tm.Type(c.name)
		tm.settle()
		tm.Send(KeyEnter)
		tm.WaitText(c.want, 15*time.Second)
		tm.Send("q")
		tm.WaitGone("HTTP Response", uiTimeout)
	}
}

// Spec (SCN-13): Connect on a remote MCP server that can not be reached
// says "no connection" and why.
func TestOfflineMCPRemote(t *testing.T) {
	ref := refusedAddr(t)
	cfg := `{"mcpServers": {"far": {"type": "http", "url": "http://` + ref + `/mcp"}}}`
	home := t.TempDir()
	writeFile(t, filepath.Join(home, ".config", "termocode", "mcp.json"), cfg)
	tm := Start(t, StartOpts{Home: home, Workspace: NewRepo(t), Cols: 140, Rows: 40, Env: offlineEnv(t)})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()
	tm.Palette("AI: MCP Servers")
	tm.WaitText("remote · stopped", uiTimeout)
	tm.Type("far")
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("Connect / Refresh tools", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitText("MCP far: no connection", 20*time.Second)
	tm.WaitText("refused the connection", uiTimeout)
}

// Spec (SCN-13): installing a language server offline (the download
// goes to a proxy that refuses) says so in one toast.
func TestOfflineLSPInstall(t *testing.T) {
	tm := startOffline(t, NewRepo(t), "")
	tm.Palette("LSP: Manage Language Servers")
	tm.Type("lua_ls")
	tm.WaitText("lua_ls (Lua)", uiTimeout)
	tm.settle()
	tm.Send(KeyEnter)
	tm.WaitFor("the install card", uiTimeout, func(ls []string) bool {
		s := strings.Join(ls, "\n")
		return strings.Contains(s, "Install") && strings.Contains(s, "lua-language-server")
	})
	tm.settle()
	tm.Send(KeyEnter) // Install
	tm.WaitText("Offline — can't install lua_ls", 30*time.Second)
	tm.WaitText("check your network connection", uiTimeout)
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
