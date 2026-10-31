package app

import (
	"context"
	"errors"
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── MCP client glue (MC-3 / MC-4) ───────────────────────────────────────
//
// internal/mcp runs the servers; this file wires them into the chat:
//
//   - aiMCPTurn gives each chat turn the tools of every configured server.
//     It runs on the turn's goroutine (it may start a server once to list
//     its tools, and ask the user to trust a project server through the
//     chat channel, like a tool confirm).
//   - Mutating MCP tools use the normal confirm gate; "Always allow
//     mcp:<server>" goes into ai_auto_approve like fileWrite.
//   - Server lifecycle lines go to the Output channel "MCP".
//   - Palette: AI: MCP Servers… (status, connect, stop, tools, trust),
//     AI: MCP: Show Log, Open Config: MCP Servers.

// mcpLogChannel is the Output channel of the MCP lifecycle log.
const mcpLogChannel = "MCP"

// mcpTarget is the Manager StopMCP stops.
var mcpTarget atomic.Pointer[mcp.Manager]

// StopMCP stops every MCP server (and its children). cmd/termocode calls
// it when Bubble Tea returns, next to StopExtensions.
func StopMCP() {
	if m := mcpTarget.Load(); m != nil {
		m.Close()
	}
}

// mcpCacheDir holds the tool lists ($XDG_CACHE_HOME/termocode/mcp).
func mcpCacheDir() string {
	cache := os.Getenv("XDG_CACHE_HOME")
	if cache == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		cache = filepath.Join(home, ".cache")
	}
	return filepath.Join(cache, "termocode", "mcp")
}

func mcpConfigPath() string {
	p, _ := ai.ConfigPath()
	return p
}

func mcpUserPath() string {
	d, err := ai.ConfigDir()
	if err != nil {
		return ""
	}
	return mcp.UserPath(d)
}

// aiMCP returns the Manager, made on first use (it starts nothing).
// Timeouts are read then; a restart applies new ones.
func (m *Model) aiMCP() *mcp.Manager {
	_, _ = configureSecretStore()
	s := m.aiS()
	if s.mcp != nil {
		return s.mcp
	}
	root := aiWorkspaceRoot()
	set := mcp.LoadSettings(mcpConfigPath())
	mcp.SetGitHubClientID(set.GitHubClientID)
	out := m.output
	home, _ := os.UserHomeDir()
	s.mcp = mcp.NewManager(mcp.ManagerOptions{
		UserPath:    mcpUserPath(),
		ProjectPath: mcp.ProjectPath(root),
		CacheDir:    mcpCacheDir(),
		Expander: func() mcp.Expander {
			var storeSecretLookup func(string) (string, bool)
			if p, err := ai.CredentialsPath(); err == nil {
				if c, err := ai.LoadCredentials(p); err == nil {
					_ = c // loading registers the secrets for Redact
				}
				storeSecretLookup = ai.LookupMCPSecret
			}
			return mcp.Expander{Env: os.LookupEnv, SecretLookup: storeSecretLookup, Workspace: root, Home: home}
		},
		Roots: func() []string { return []string{root} },
		Log: func(server, line string) {
			out.Append(mcpLogChannel, clock.Now().Format("15:04:05")+"  "+server+": "+ai.Redact(aiCleanText(line)))
		},
		IdleTimeout: set.IdleTimeout,
		CallTimeout: set.CallTimeout,
		IsTrusted: func(sv mcp.Server) bool {
			return mcp.LoadSettings(mcpConfigPath()).Trusted[sv.Name] == sv.Hash()
		},
		SaveTrust: func(sv mcp.Server) { _ = mcp.SaveTrusted(mcpConfigPath(), sv.Name, sv.Hash()) },
		TrustedTools: func(name string) string {
			return mcp.LoadSettings(mcpConfigPath()).TrustedTools[name]
		},
		SaveTrustedTools: func(name, hash string) { _ = mcp.SaveTrustedTools(mcpConfigPath(), name, hash) },
		Tokens:           mcp.CredStore{Path: mcpCredPath()},
		OAuthPort:        func() int { return mcp.LoadSettings(mcpConfigPath()).OAuthPort },
		CIMDURL:          mcpCIMDURL,
		Share:            mcpShareOptions(set),
	})
	mcpTarget.Store(s.mcp)
	return s.mcp
}

// aiMCPTurn returns the goroutine part of a chat turn: it reloads
// mcp.json and builds the MCP tools (nil when mcp_enabled is false).
func (m *Model) aiMCPTurn(gen uint64, ch chan tea.Msg) func(ctx context.Context) []ai.Tool {
	set := mcp.LoadSettings(mcpConfigPath())
	if !set.Enabled {
		return nil
	}
	mgr := m.aiMCP()
	ask := mcpChatAsker(gen, ch)
	opts := m.mcpAdapterOptions(set)
	return func(ctx context.Context) []ai.Tool {
		mgr.Reload()
		var out []ai.Tool
		call := mcp.TurnCaller(mgr.Caller(ask)) // two failures: the server is out for this turn
		for _, st := range mgr.Tools(ctx, ask) {
			o := opts
			o.ToolsChanged = st.ToolsChanged
			out = append(out, mcp.AITools(st.Server, st.Tools, call, o)...)
		}
		return withMCPReader(out, opts.Spill)
	}
}

// ── Trust dialog ───────────────────────────────────────────────────────

const (
	confirmKindMCPTrust confirmKindEnum = 950 + iota
)

const (
	pickerKindMCPServers pickerKindEnum = 950 + iota
	pickerKindMCPServer
	pickerKindMCPTools
)

// aiMCPTrustMsg asks the user to trust a server (sent by the chat
// goroutine; the reply is buffered).
type aiMCPTrustMsg struct {
	gen    uint64
	server mcp.Server
	reply  chan mcp.TrustAnswer
	ch     chan tea.Msg
}

// aiMCPTrustPending is the open trust dialog: a chat turn waits on
// reply, or (reply nil) the servers picker connects afterwards.
type aiMCPTrustPending struct {
	server       string
	reply        chan mcp.TrustAnswer
	connect      bool
	signin       bool // sign in after the answer (servers picker)
	toolsChanged bool // the question was ReasonToolsChanged
}

func mcpChatAsker(gen uint64, ch chan tea.Msg) mcp.AskTrust {
	return func(ctx context.Context, s mcp.Server) mcp.TrustAnswer {
		reply := make(chan mcp.TrustAnswer, 1)
		select {
		case ch <- aiMCPTrustMsg{gen: gen, server: s, reply: reply, ch: ch}:
		case <-ctx.Done():
			return mcp.TrustDeny
		}
		select {
		case a := <-reply:
			return a
		case <-ctx.Done():
			return mcp.TrustDeny
		}
	}
}

// mcpTrustDialog builds "Start MCP server X?" with the file, command and
// env keys (never their values).
func (m *Model) mcpTrustDialog(s mcp.Server) {
	if s.TrustReason == mcp.ReasonToolsChanged {
		m.confirm = confirm.New("MCP "+s.Name+": its tools changed",
			"The tools of "+s.Name+" are not the ones you trusted before: a server can change what its tools do. "+
				"Review them in AI: MCP Servers… → "+s.Name+" → Show tools.\nUntil you trust them, each of its tools asks before it runs.",
			[]confirm.Button{
				{ID: "deny", Title: "Not now"},
				{ID: "once", Title: "Trust once", Style: confirm.StylePrimary},
				{ID: "always", Title: "Trust the new tools"},
			})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindMCPTrust
		return
	}
	from := "your mcp.json (" + s.SourcePath + ")"
	if s.Source == mcp.SourceProject {
		from = "this project's .termocode/mcp.json"
	}
	var env []string
	for k := range s.Env {
		env = append(env, k)
	}
	sort.Strings(env)
	msg := "From " + from + ". It runs on your computer with your rights: only allow servers you trust."
	if len(env) > 0 {
		msg += "\nEnv: " + strings.Join(env, ", ")
	}
	code := strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
	if s.Type != mcp.TypeStdio {
		code = s.URL
	}
	m.confirm = confirm.New("Start MCP server "+s.Name+"?", msg, []confirm.Button{
		{ID: "deny", Title: "Deny"},
		{ID: "once", Title: "Allow once", Style: confirm.StylePrimary},
		{ID: "always", Title: "Always allow"},
	}).WithCode(code, "")
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindMCPTrust
}

// onAIMCPTrust opens the trust dialog for a chat turn.
func (m *Model) onAIMCPTrust(msg aiMCPTrustMsg) tea.Cmd {
	s := m.aiS()
	if msg.gen != s.chatGen {
		msg.reply <- mcp.TrustDeny
		return waitAIChat(msg.ch)
	}
	s.mcpTrust = &aiMCPTrustPending{server: msg.server.Name, reply: msg.reply,
		toolsChanged: msg.server.TrustReason == mcp.ReasonToolsChanged}
	m.mcpTrustDialog(msg.server)
	return waitAIChat(msg.ch)
}

// answerMCPTrust handles the trust dialog's button (Esc = deny).
func (m *Model) answerMCPTrust(id string) tea.Cmd {
	s := m.aiS()
	p := s.mcpTrust
	s.mcpTrust = nil
	if p == nil {
		return nil
	}
	a := mcp.TrustDeny
	switch id {
	case "once":
		a = mcp.TrustOnce
	case "always":
		a = mcp.TrustAlways
	}
	if p.reply != nil {
		p.reply <- a
		if p.toolsChanged && a == mcp.TrustDeny {
			return m.mcpToolsChangedToast(p.server)
		}
		return nil
	}
	if p.signin && a != mcp.TrustDeny {
		m.aiMCP().Trust(p.server, a == mcp.TrustAlways)
		return m.mcpConnectServer(p.server)
	}
	if p.connect && a != mcp.TrustDeny {
		return m.mcpConnect(p.server, a)
	}
	return nil
}

// mcpHasAuthHeader: the entry sends its own Authorization (a PAT), so
// it has no OAuth sign-in.
func mcpHasAuthHeader(s mcp.Server) bool {
	for k := range s.Headers {
		if strings.EqualFold(k, "Authorization") {
			return true
		}
	}
	return false
}

// mcpToolsChangedToast warns that a server's tools changed since the
// user trusted them.
func (m *Model) mcpToolsChangedToast(name string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Warn, "MCP "+name+": its tools changed",
		"Review them in AI: MCP Servers… → "+name+" → Show tools, then Trust the new tools. Until then each tool asks first.")
	return c
}

// ── Servers picker ─────────────────────────────────────────────────────

// mcpStatusHint is a server row's hint: "local · running · pid 42 · 10
// tools"; a known service reads "amin-jalali · read only · connected ·
// 28 tools".
func mcpStatusHint(st mcp.Status) string {
	if st.State == mcp.StateDisabled {
		return "off · kept in mcp.json, not started, no tools" + mcpSourceSuffix(st)
	}
	if h := mcpServiceHint(st); h != "" {
		return h
	}
	kind := "local"
	if st.Type != mcp.TypeStdio {
		kind = "remote"
	}
	// Warnings first: the picker cuts a long hint, and "local · running ·
	// pid 4242 · …" left no room for them.
	var parts []string
	if st.ToolsChanged {
		parts = append(parts, "tools changed")
	}
	if !st.Trusted {
		parts = append(parts, "not trusted")
	}
	parts = append(parts, kind)
	switch {
	case st.State == mcp.StateInvalid:
		parts = append(parts, st.Problem)
	case st.State == mcp.StateFailed:
		parts = append(parts, "failed: "+st.Problem)
	case st.State == mcp.StateReconnecting:
		parts = append(parts, st.StateText()+" ("+st.Problem+")")
	case st.State == mcp.StateRunning && kind == "remote":
		parts = append(parts, "connected")
	default:
		parts = append(parts, st.State)
	}
	if st.SignedIn {
		parts = append(parts, "signed in")
	}
	if st.Shared {
		parts = append(parts, "shared · "+mcpSessionsText(st.Sessions))
	}
	if st.Pid > 0 {
		parts = append(parts, fmt.Sprintf("pid %d", st.Pid))
	}
	if st.Tools > 0 {
		t := fmt.Sprintf("%d tools", st.Tools)
		if st.Cached {
			t += " (cached)"
		}
		parts = append(parts, t)
	}
	if st.Source == mcp.SourceProject {
		parts = append(parts, "project")
	}
	return ai.Redact(strings.Join(parts, " · "))
}

func mcpSourceSuffix(st mcp.Status) string {
	if st.Source == mcp.SourceProject {
		return " · project"
	}
	return ""
}

func mcpStatusIcon(st mcp.Status) string {
	if st.Expired {
		return "!"
	}
	switch st.State {
	case mcp.StateRunning:
		return "●"
	case mcp.StateDisabled:
		return "○"
	case mcp.StateFailed, mcp.StateInvalid:
		return "✗"
	case mcp.StateSignIn:
		return "!"
	}
	return "○"
}

// openMCPServers lists every configured server.
func (m *Model) openMCPServers() tea.Cmd {
	mgr := m.aiMCP()
	mgr.Reload()
	list := mgr.Servers()
	items := make([]picker.Item, 0, len(list)+1)
	items = append(items, picker.Item{ID: mcpAddID, Title: "+ Connect a service…", Hint: "GitHub, Linear, GitLab, Slack, Notion …"})
	for _, st := range list {
		items = append(items, picker.Item{ID: st.Name, Title: mcpStatusIcon(st) + " " + st.Name, Hint: mcpStatusHint(st)})
	}
	m.picker = picker.NewItems(" MCP servers (Enter: actions) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPServers
	return nil
}

// openMCPServerActions is the second picker for one server.
func (m *Model) openMCPServerActions(name string) tea.Cmd {
	var st mcp.Status
	for _, x := range m.aiMCP().Servers() {
		if x.Name == name {
			st = x
		}
	}
	if st.Name == "" {
		return nil
	}
	m.aiS().mcpSel = name
	items := mcpServerActionItems(m, name)
	m.picker = picker.NewItems(" MCP "+name+" — "+mcpStatusHint(st)+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPServer
	return nil
}

// mcpServerActionItems are the rows of a server's actions picker.
func mcpServerActionItems(m *Model, name string) []picker.Item {
	var st mcp.Status
	for _, x := range m.aiMCP().Servers() {
		if x.Name == name {
			st = x
		}
	}
	if st.State == mcp.StateDisabled {
		return []picker.Item{
			{ID: "enable", Title: "Enable", Hint: "start it again; the AI gets its tools"},
			{ID: "edit", Title: "Edit in mcp.json", Hint: st.Source},
			{ID: "remove", Title: "Remove…", Hint: "delete the entry and forget its sign-in"},
		}
	}
	items := []picker.Item{
		{ID: "connect", Title: "Connect / Refresh tools", Hint: "start it and list its tools"},
		{ID: "tools", Title: "Show tools", Hint: fmt.Sprintf("%d tools", st.Tools)},
	}
	if st.Pid > 0 {
		hint := fmt.Sprintf("pid %d", st.Pid)
		if st.Shared && st.Sessions > 1 {
			hint += " · stops it for " + mcpSessionsText(st.Sessions)
		}
		items = append(items, picker.Item{ID: "stop", Title: "Stop", Hint: hint})
	}
	if !st.Trusted {
		items = append(items, picker.Item{ID: "trust", Title: "Trust (always allow this entry)", Hint: "asks again when the entry changes"})
	}
	if st.ToolsChanged {
		items = append(items, picker.Item{ID: "accept-tools", Title: "Trust the new tools", Hint: "review them with Show tools first"})
	}
	entry := mcpServerEntries(name)
	if st.Type == mcp.TypeHTTP && st.State != mcp.StateInvalid && (len(entry) == 0 || !mcpHasAuthHeader(entry[0])) {
		hint := "in the browser"
		switch {
		case st.Expired:
			hint = "the sign-in ended"
		case mcpOverSSH():
			hint = "paste the address back (SSH)"
		}
		items = append(items, picker.Item{ID: "signin", Title: "Sign in", Hint: hint})
	}
	if p, ok := mcpPresetFor(name); ok && len(entry) > 0 && st.State != mcp.StateInvalid {
		if p.ReadOnlyURL != "" || p.WriteScopes != nil {
			if mcpEntryReadOnly(entry[0], p) {
				items = append(items, picker.Item{ID: "writes", Title: "Allow writes", Hint: "create, comment, change (each write asks first)"})
			} else {
				items = append(items, picker.Item{ID: "readonly", Title: "Read only", Hint: "turn writes off"})
			}
		}
		items = append(items, picker.Item{ID: "add-account", Title: "Add another account", Hint: "a second " + p.Title + " entry"})
	}
	if st.SignedIn {
		items = append(items, picker.Item{ID: "signout", Title: "Sign out", Hint: "forget its tokens"})
	}
	for _, e := range entry {
		missing := map[string]bool{}
		for _, n := range mcpMissingSecrets(e) {
			missing[n] = true
		}
		for _, n := range e.SecretNames() {
			hint := "saved"
			if missing[n] {
				hint = "not set"
			}
			items = append(items, picker.Item{ID: "secret:" + n, Title: "Set secret " + n + "…", Hint: hint})
		}
	}
	items = append(items,
		picker.Item{ID: "edit", Title: "Edit in mcp.json", Hint: st.Source},
		picker.Item{ID: "log", Title: "Show log", Hint: "Output: MCP"},
		picker.Item{ID: "disable", Title: "Disable", Hint: "keep the entry, stop it, no tools for the AI"},
		picker.Item{ID: "remove", Title: "Remove…", Hint: "delete the entry and forget its sign-in"},
	)
	return items
}

// mcpConnectDoneMsg ends a Connect from the servers picker.
type mcpConnectDoneMsg struct {
	name string
	err  error
}

// mcpConnect starts a server and lists its tools on a Cmd goroutine.
func (m *Model) mcpConnect(name string, answer mcp.TrustAnswer) tea.Cmd {
	mgr := m.aiMCP()
	ask := func(context.Context, mcp.Server) mcp.TrustAnswer { return answer }
	return tea.Batch(m.aiInfo("MCP "+name+": starting…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return mcpConnectDoneMsg{name: name, err: mgr.Connect(ctx, ask, name)}
	})
}

func (m *Model) onMCPConnectDone(msg mcpConnectDoneMsg) tea.Cmd {
	var ce *mcp.ConnError
	if errors.As(msg.err, &ce) { // offline: the cause on the toast's one line
		return m.aiToastErr("MCP "+msg.name+": no connection", errors.New(ce.Why))
	}
	if msg.err != nil {
		return m.aiToastErr("MCP "+msg.name, msg.err)
	}
	for _, st := range m.aiMCP().Servers() {
		if st.Name == msg.name {
			if st.ToolsChanged {
				return m.mcpToolsChangedToast(msg.name)
			}
			return m.aiInfo("MCP " + msg.name + ": " + mcpStatusHint(st))
		}
	}
	return nil
}

// onMCPServerAction runs one action of the server picker.
func (m *Model) onMCPServerAction(id string) tea.Cmd {
	name := m.aiS().mcpSel
	mgr := m.aiMCP()
	var st mcp.Status
	for _, x := range mgr.Servers() {
		if x.Name == name {
			st = x
		}
	}
	if st.Name == "" {
		return nil
	}
	switch id {
	case "connect":
		if !st.Trusted {
			for _, sv := range mcpServerEntries(name) {
				m.aiS().mcpTrust = &aiMCPTrustPending{server: name, connect: true}
				m.mcpTrustDialog(sv)
				return nil
			}
		}
		return m.mcpConnect(name, mcp.TrustDeny)
	case "stop":
		if st.Shared && st.Sessions > 1 {
			return m.openMCPShareStop(name, st.Sessions)
		}
		mgr.Stop(name)
		return m.aiInfo("MCP " + name + ": stopped")
	case "tools":
		return m.openMCPTools(name)
	case "trust":
		for _, sv := range mcpServerEntries(name) {
			if err := mcp.SaveTrusted(mcpConfigPath(), sv.Name, sv.Hash()); err != nil {
				return m.aiToastErr("MCP "+name, err)
			}
		}
		return m.aiInfo("MCP " + name + ": trusted")
	case "accept-tools":
		mgr.AcceptTools(name)
		return m.aiInfo("MCP " + name + ": new tools trusted")
	case "edit":
		for _, sv := range mcpServerEntries(name) {
			m.openFileAtLine(sv.SourcePath, 1)
		}
		return nil
	case "log":
		return m.showMCPLog()
	case "signin":
		if !st.Trusted {
			for _, sv := range mcpServerEntries(name) {
				m.aiS().mcpTrust = &aiMCPTrustPending{server: name, signin: true}
				m.mcpTrustDialog(sv)
				return nil
			}
		}
		return m.mcpConnectServer(name)
	case "signout":
		return m.mcpSignOut(name)
	case "writes", "readonly":
		return m.mcpSetWrites(name, id == "writes")
	case "add-account":
		return m.mcpAddAccount(name)
	case "disable", "enable":
		return m.mcpSetDisabled(name, id == "disable")
	case "remove":
		return m.openMCPRemove(name)
	}
	if secret, ok := strings.CutPrefix(id, "secret:"); ok {
		return m.mcpAskSecret(secret)
	}
	return nil
}

// mcpServerEntries is the current mcp.json entry of name (0 or 1).
func mcpServerEntries(name string) []mcp.Server {
	list, _ := mcp.LoadAll(mcpUserPath(), mcp.ProjectPath(aiWorkspaceRoot()))
	for _, s := range list {
		if s.Name == name {
			return []mcp.Server{s}
		}
	}
	return nil
}

// openMCPTools shows a server's tools with how each one is gated, so the
// user can review them (tool poisoning).
func (m *Model) openMCPTools(name string) tea.Cmd {
	srv, tools, ok := m.aiMCP().KnownTools(name)
	if !ok || len(tools) == 0 {
		return m.aiInfo("MCP " + name + ": no tools known yet — Connect first")
	}
	trust := mcp.LoadSettings(mcpConfigPath()).TrustAnnotations
	var items []picker.Item
	for _, t := range tools {
		exposed, mutating, class := mcp.Gate(srv, t, trust)
		gate := "runs at once"
		switch {
		case !exposed:
			gate = "hidden"
		case mutating && class == "":
			gate = "always asks"
		case mutating:
			gate = "asks"
		}
		desc := aiCleanText(strings.SplitN(t.Description, "\n", 2)[0])
		if r := []rune(desc); len(r) > 80 {
			desc = string(r[:79]) + "…"
		}
		items = append(items, picker.Item{ID: t.Name, Title: t.Name + "  " + desc, Hint: gate})
	}
	m.picker = picker.NewItems(" MCP "+name+" tools ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPTools
	return nil
}

// showMCPLog opens the Output panel on the MCP channel.
func (m *Model) showMCPLog() tea.Cmd {
	m.aiMCP() // the channel exists once MCP ran; show it empty otherwise
	m.output.SetActive(mcpLogChannel)
	m.showPanelTab(panelKindOutput)
	return nil
}

// editMCPConfig opens the user mcp.json in the editor (created with an
// empty mcpServers block when missing).
func (m *Model) editMCPConfig() tea.Cmd {
	p := mcpUserPath()
	if p == "" {
		return nil
	}
	if _, err := os.Stat(p); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return m.aiToastErr("MCP config", err)
		}
		if err := os.WriteFile(p, []byte("{\n  \"mcpServers\": {\n  }\n}\n"), 0o644); err != nil {
			return m.aiToastErr("MCP config", err)
		}
	}
	m.openFileAtLine(p, 1)
	return m.aiInfo("Add servers (an mcpServers block, as other tools write it), save; the next chat message uses them")
}

// handleMCPPickerSelect routes the MCP picker kinds. ok=false: not ours.
func (m *Model) handleMCPPickerSelect(id string) (tea.Cmd, bool) {
	switch m.pickerKind {
	case pickerKindMCPServers:
		if id == mcpAddID {
			return m.openMCPConnect(), true
		}
		return m.openMCPServerActions(id), true
	case pickerKindMCPConnect:
		return m.onMCPConnectPicked(id), true
	case pickerKindMCPServer:
		return m.onMCPServerAction(id), true
	case pickerKindMCPTools:
		return nil, true
	}
	return nil, false
}

// mcpGlyph marks MCP in the chat header chip.
func mcpGlyph() string { return theme.GlyphMCP.String() }

// mcpChipLabel is the chat header chip: "⌁ N MCP" (N = connected
// servers), "⌁ Connect…" without any service, "⌁ N needs sign-in" when
// servers wait for a sign-in; "" before MCP was loaded (the header never
// creates the Manager; opening the chat does).
func (m Model) mcpChipLabel() string {
	n := m.mcpChipState()
	switch {
	case n == mcpChipNone:
		return ""
	case n == mcpChipConnect:
		return mcpGlyph() + " Connect…"
	case n <= mcpChipSignIn:
		return mcpGlyph() + " " + strconv.Itoa(mcpChipSignIn-n+1) + " needs sign-in"
	}
	return mcpGlyph() + " " + strconv.Itoa(n) + " MCP"
}

// mcpChipState values below 0 (≥ 0 is the connected count).
const (
	mcpChipNone    = -1
	mcpChipConnect = -2
	mcpChipSignIn  = -3 // -3 = 1 server needs a sign-in, -4 = 2, …
)

// mcpChipState is the chip as one int (no allocation: the header memo
// compares it every frame).
func (m Model) mcpChipState() int {
	if m.ai == nil || m.ai.mcp == nil {
		return mcpChipNone
	}
	if m.ai.mcp.Count() == 0 {
		return mcpChipConnect
	}
	if n := len(m.ai.mcp.NeedSignIn()); n > 0 {
		return mcpChipSignIn - n + 1
	}
	return m.ai.mcp.Running()
}

// onMCPChip runs a click on the chip: the wizard without services or for
// the server that needs a sign-in, else Itermoc at SERVICES.
func (m *Model) onMCPChip() tea.Cmd {
	switch n := m.mcpChipState(); {
	case n == mcpChipConnect:
		return m.openMCPConnect()
	case n <= mcpChipSignIn:
		return m.mcpConnectServer(m.aiMCP().NeedSignIn()[0])
	}
	return m.revealItermocView(true, imSecServices) // Itermoc at SERVICES; AI: MCP Servers… stays in the palette
}

// mcpSignInBar is the row above the chat input while a server waits
// for a sign-in ("⌁ GitHub needs a sign-in  Sign in  Not now"); ok=false
// when there is none (or the user said Not now).
func (m Model) mcpSignInBar() (aiLine, bool) {
	if m.ai == nil || m.ai.mcp == nil {
		return aiLine{}, false
	}
	for _, name := range m.ai.mcp.NeedSignIn() {
		if m.ai.mcpNotNow[name] {
			continue
		}
		title := name
		if p, ok := mcpPresetFor(name); ok && p.Name == name {
			title = p.Title
		}
		bg := theme.Bg(theme.BgPanel)
		chip := func(label string, t float64) string {
			return lipgloss.NewStyle().Background(theme.Mix(theme.BgPanel, theme.AIAccent, t)).
				Foreground(theme.LG(theme.TextPrimary)).Render(" " + label + " ")
		}
		head := " " + mcpGlyph() + " " + title + " needs a sign-in "
		x := runewidth.StringWidth(head)
		row := theme.FgBg(theme.TextSecondary, theme.BgPanel).Render(head) + chip("Sign in", 0.30) + bg.Render(" ") + chip("Not now", 0.10)
		return aiLine{styled: row, buttons: []aiLineButton{{x, x + 9, "mcp-signin:" + name}, {x + 10, x + 19, "mcp-notnow:" + name}}}, true
	}
	return aiLine{}, false
}

// mcpAutoApproveClasses are the "mcp:<server>" rows of AI: Agent
// Auto-Approve….
func (m *Model) mcpAutoApproveClasses() []string {
	var out []string
	for _, st := range m.aiMCP().Servers() {
		out = append(out, mcp.ClassPrefix+st.Name)
	}
	return out
}

// mcpEntryReadOnly reports an entry on the service's read-only endpoint
// or read scopes.
func mcpEntryReadOnly(e mcp.Server, p mcp.Preset) bool {
	if p.ReadOnlyURL != "" {
		return strings.TrimSuffix(e.URL, "/") == strings.TrimSuffix(p.ReadOnlyURL, "/")
	}
	if p.WriteScopes == nil {
		return false
	}
	if e.OAuth == nil {
		return false
	}
	return strings.Join(e.OAuth.Scopes, " ") == strings.Join(p.ReadScopes, " ")
}

// mcpServiceHint is the row hint of a known service (a preset entry):
// account · access · state · tools ("" for other servers).
func mcpServiceHint(st mcp.Status) string {
	p, ok := mcpPresetFor(st.Name)
	if !ok || st.Type != mcp.TypeHTTP || st.State == mcp.StateInvalid {
		return ""
	}
	entries := mcpServerEntries(st.Name)
	if len(entries) == 0 {
		return ""
	}
	e := entries[0]
	var parts []string
	acc := (mcp.CredStore{Path: mcpCredPath()}).Account(st.Name)
	if acc == "" && e.Auth != nil {
		acc = e.Auth.GH
	}
	if acc != "" {
		parts = append(parts, acc)
	}
	if p.ReadOnlyURL != "" || p.WriteScopes != nil {
		if mcpEntryReadOnly(e, p) {
			parts = append(parts, "read only")
		} else {
			parts = append(parts, "read and write")
		}
	}
	switch {
	case st.Expired:
		parts = append(parts, "sign-in expired")
	case st.State == mcp.StateSignIn:
		parts = append(parts, "sign-in needed")
	case st.State == mcp.StateFailed:
		parts = append(parts, "failed: "+st.Problem)
	case st.State == mcp.StateReconnecting:
		parts = append(parts, st.StateText())
	case st.State == mcp.StateRunning:
		parts = append(parts, "connected")
	case !st.SignedIn && (e.Auth == nil || e.Auth.GH == "") && !mcpHasAuthHeader(e):
		parts = append(parts, "not signed in")
	}
	if st.Tools > 0 {
		parts = append(parts, fmt.Sprintf("%d tools", st.Tools))
	}
	if !st.Trusted {
		parts = append(parts, "not trusted")
	}
	if st.Source == mcp.SourceProject {
		parts = append(parts, "project")
	}
	return ai.Redact(strings.Join(parts, " · "))
}

// mcpSetWrites turns a service's writes on or off: the full or the
// read-only endpoint (the sign-in stays: same resource), and, where the
// service limits writes by scope (Linear, ClickUp), the scopes and a new
// sign-in.
func (m *Model) mcpSetWrites(name string, on bool) tea.Cmd {
	p, ok := mcpPresetFor(name)
	entries := mcpServerEntries(name)
	if !ok || len(entries) == 0 {
		return nil
	}
	e := entries[0]
	if p.ReadOnlyURL != "" {
		u := p.ReadOnlyURL
		if on {
			u = p.URL
		}
		if err := mcp.SetServerKey(mcpUserPath(), name, "url", u); err != nil {
			return m.aiToastErr("MCP "+name, err)
		}
	}
	resign := false
	if p.WriteScopes != nil && e.OAuth != nil && (e.Auth == nil || e.Auth.GH == "") && !mcpHasAuthHeader(e) {
		scopes := p.ReadScopes
		if on {
			scopes = p.WriteScopes
		}
		if strings.Join(scopes, " ") != strings.Join(e.OAuth.Scopes, " ") {
			o := *e.OAuth
			o.Scopes = scopes
			if err := mcp.SetServerKey(mcpUserPath(), name, "oauth", &o); err != nil {
				return m.aiToastErr("MCP "+name, err)
			}
			resign = on // more scopes need a new consent; fewer do not
		}
	}
	mgr := m.aiMCP()
	mgr.Reload()
	mgr.Stop(name)
	if resign {
		return m.mcpConnectServer(name)
	}
	if on {
		return m.aiInfo("MCP " + name + ": writes are on (each write asks first)")
	}
	return m.aiInfo("MCP " + name + ": read only")
}

// mcpAddAccount runs the wizard for a second entry of the same service.
func (m *Model) mcpAddAccount(name string) tea.Cmd {
	p, ok := mcpPresetFor(name)
	if !ok {
		return nil
	}
	next := p.Name + "-2"
	for i := 3; len(mcpServerEntries(next)) > 0; i++ {
		next = fmt.Sprintf("%s-%d", p.Name, i)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		acc, _ := mcp.GHAccounts(ctx)
		return mcpWizEnvMsg{accounts: acc, then: p.Name, name: next, another: true}
	}
}

// mcpPromptLines are the system-prompt lines about connected services
// (plan §4.11): "GitHub is connected as amin-jalali (read only)." The
// account is a cleaned label, never a token.
func (m *Model) mcpPromptLines() string {
	if m.ai == nil || m.ai.mcp == nil {
		return ""
	}
	cs := mcp.CredStore{Path: mcpCredPath()}
	var out []string
	for _, st := range m.ai.mcp.Servers() {
		p, ok := mcpPresetFor(st.Name)
		if !ok || st.Type != mcp.TypeHTTP || st.State == mcp.StateInvalid {
			continue
		}
		entries := mcpServerEntries(st.Name)
		if len(entries) == 0 {
			continue
		}
		e := entries[0]
		acc := cs.Account(st.Name)
		if acc == "" && e.Auth != nil {
			acc = e.Auth.GH
		}
		if !st.SignedIn && acc == "" && !mcpHasAuthHeader(e) {
			continue
		}
		line := p.Title + " is connected"
		if st.Name != p.Name {
			line = p.Title + " (" + st.Name + ") is connected"
		}
		if acc != "" {
			line += " as " + mcp.CleanAccount(acc)
		}
		if p.ReadOnlyURL != "" || p.WriteScopes != nil {
			if mcpEntryReadOnly(e, p) {
				line += " (read only)"
			} else {
				line += " (read and write)"
			}
		}
		out = append(out, line+".")
	}
	return strings.Join(out, "\n")
}
