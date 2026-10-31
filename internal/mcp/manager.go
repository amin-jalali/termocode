package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
)

// Manager holds every configured server (MC-3). Nothing runs at startup:
// it reads mcp.json and the on-disk tool cache only. A server starts on
// the first tools/call, on Connect, or when a chat turn needs its tool
// list and there is no cache yet; an idle timer stops it again. No timer
// exists while nothing runs, so termocode stays at 0 % CPU.
//
// All methods are safe for concurrent use: the chat agent calls from its
// goroutine, the UI reads Servers.

// Server states (Status.State).
const (
	StateStopped  = "stopped"
	StateRunning  = "running"
	StateFailed   = "failed"  // the last start failed (Problem says why); the next call tries again
	StateInvalid  = "invalid" // the entry can not run (Problem says why)
	StateSignIn   = "sign-in needed"
	StateDisabled = "off" // "disabled": true in the entry: kept, never started, no tools
	// StateReconnecting: a remote server could not be reached; it tries
	// again by itself (Status.RetryIn, reconnect.go).
	StateReconnecting = "reconnecting"
)

// TrustAnswer is the user's answer to "start this server?".
type TrustAnswer int

const (
	TrustDeny   TrustAnswer = iota
	TrustOnce               // this termocode session only
	TrustAlways             // saved (per entry hash) through SaveTrust
)

// AskTrust asks the user about an untrusted server (a project entry). It
// may block; nil means deny.
type AskTrust func(ctx context.Context, s Server) TrustAnswer

// ErrNotTrusted is returned for a server the user did not allow.
var ErrNotTrusted = errors.New("server not trusted: the user did not allow it (AI: MCP Servers… to trust it)")

// ErrToolsChanged is returned by a call to a server whose tools changed
// since the user trusted them, before the user was asked about it.
var ErrToolsChanged = errors.New("the server's tools changed since you trusted them: review them in AI: MCP Servers… → Show tools; do not retry this call now")

// ManagerOptions wire the Manager to the app. All fields are optional.
type ManagerOptions struct {
	UserPath, ProjectPath string
	CacheDir              string // tool lists per server; "" = no cache
	// Expander is called before each start, so new secrets apply.
	Expander func() Expander
	Roots    func() []string
	Log      func(server, line string)
	// IdleTimeout stops a running server after this long without a call
	// (0: never). CallTimeout caps one tools/call unless the entry sets
	// "timeout".
	IdleTimeout time.Duration
	CallTimeout time.Duration
	// IsTrusted reports a saved "Always" answer; SaveTrust stores one.
	// User entries are always trusted (the user wrote them).
	IsTrusted func(s Server) bool
	SaveTrust func(s Server)
	// TrustedTools is the hash (ToolsHash) of the tool list the user
	// accepted for a server ("" none yet); SaveTrustedTools stores one.
	// The first list of a server is accepted by itself; a later list
	// that differs asks again (ReasonToolsChanged). Both nil: no check.
	TrustedTools     func(name string) string
	SaveTrustedTools func(name, hash string)

	// Tokens keeps OAuth sign-ins (CredStore in production); nil: remote
	// servers can only use static headers.
	Tokens TokenStore
	// HTTP is the client for remote servers and OAuth (nil: default).
	HTTP *http.Client
	// OAuthPort is mcp_oauth_port (0: a free port per sign-in).
	OAuthPort func() int
	// CIMDURL is termocode's OAuth client metadata document.
	CIMDURL string

	// Share runs stdio servers once for every session of this user
	// (share.go); nil: each session runs its own (mcp_share off).
	Share *ShareOptions

	// ReconnectBase is the first wait before a remote server that can
	// not be reached tries again (0: 2 s; < 0: no reconnects); each try
	// doubles it up to ReconnectCap (0: 2 min), ReconnectMax tries (0: 8).
	ReconnectBase, ReconnectCap time.Duration
	ReconnectMax                int
}

// Status is one row of the servers list.
type Status struct {
	Name, Type, Source string
	State              string
	Problem            string
	Pid                int
	Tools              int  // known tools (live or cached)
	Cached             bool // tool list from the disk cache, server not asked yet
	Trusted            bool
	SignedIn           bool // an OAuth sign-in is stored (remote servers)
	// Expired: the stored sign-in has ended and can not be refreshed
	// (ClickUp has no refresh grant): sign in again.
	Expired bool
	// ToolsChanged: the tools differ from the list the user trusted and
	// the user did not accept the new list (its tools all ask first).
	ToolsChanged bool
	// Shared: the server runs once for every session of this user;
	// Sessions is how many use it now (this one too).
	Shared   bool
	Sessions int
	// OldTransport: the remote server runs on the old HTTP+SSE transport.
	OldTransport bool
	// RetryIn is the wait before the next reconnect (StateReconnecting).
	RetryIn time.Duration
	server  Server
}

// ServerTools is one server's tools for a chat turn.
type ServerTools struct {
	Server Server
	Tools  []Tool
	// ToolsChanged: the user did not accept the server's new tool list,
	// so every tool asks first (AdapterOptions.ToolsChanged).
	ToolsChanged bool
}

type serverState struct {
	op sync.Mutex // one start / list / call / stop at a time

	// guarded by Manager.mu
	cfg       Server
	proc      *Process
	tools     []Tool
	haveTools bool
	cached    bool
	state     string
	problem   string
	idle      *time.Timer
	idleGen   int
	trusted   string // entry hash trusted for this session
	denied    string // entry hash denied for this session
	toolsHash string // ToolsHash of tools
	toolsOK   string // tools hash accepted for this session
	toolsNo   string // tools hash the user did not accept (asked once)
	authMeta  string // resource_metadata URL of the last 401
	authScope string // scope of the last 401
	stepUp    bool   // a 403 insufficient_scope asked for more scopes

	retryN     int         // reconnect tries since the last success
	retryAt    time.Time   // when the waiting reconnect runs
	retryTimer *time.Timer // the waiting reconnect (nil none)
	retryGen   int
}

// Manager is the set of configured servers.
type Manager struct {
	opts ManagerOptions

	mu       sync.Mutex
	servers  map[string]*serverState
	warnings []string
	closed   bool
	retrying sync.WaitGroup  // reconnect tries in flight
	life     context.Context // ends with Close (cuts reconnect tries)
	end      context.CancelFunc
}

// NewManager loads the config (it starts nothing).
func NewManager(o ManagerOptions) *Manager {
	if o.CallTimeout <= 0 {
		o.CallTimeout = 60 * time.Second
	}
	m := &Manager{opts: o, servers: map[string]*serverState{}}
	m.life, m.end = context.WithCancel(context.Background())
	m.Reload()
	return m
}

func (m *Manager) log(server, format string, args ...any) {
	if m.opts.Log != nil {
		m.opts.Log(server, fmt.Sprintf(format, args...))
	}
}

// Reload re-reads mcp.json. A server whose entry changed or was removed
// is stopped and forgets its tools; the others keep running.
func (m *Manager) Reload() []string {
	list, warns := LoadAll(m.opts.UserPath, m.opts.ProjectPath)
	var stop []*Process
	m.mu.Lock()
	next := map[string]*serverState{}
	for _, s := range list {
		old := m.servers[s.Name]
		if old != nil && old.cfg.Hash() == s.Hash() && old.cfg.Source == s.Source {
			old.cfg = s
			next[s.Name] = old
			continue
		}
		if old != nil && old.proc != nil {
			stop = append(stop, old.proc)
		}
		if old != nil {
			m.stopRetryLocked(old)
		}
		st := &serverState{cfg: s, state: StateStopped}
		if s.Problem != "" {
			st.state, st.problem = StateInvalid, s.Problem
		}
		if s.Disabled {
			st.state, st.problem = StateDisabled, ""
		}
		next[s.Name] = st
	}
	for name, old := range m.servers {
		if _, ok := next[name]; !ok {
			m.stopRetryLocked(old)
			if old.proc != nil {
				stop = append(stop, old.proc)
			}
		}
	}
	m.servers = next
	changed := strings.Join(warns, "\n") != strings.Join(m.warnings, "\n")
	m.warnings = warns
	m.mu.Unlock()
	if changed { // once per new set, not on every chat turn
		for _, w := range warns {
			m.log("config", "%s", w)
		}
	}
	for _, p := range stop {
		crash.Go("an MCP server stop", func() { p.Close() })
	}
	return warns
}

// Warnings are the config problems of the last load.
func (m *Manager) Warnings() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.warnings...)
}

func (m *Manager) trustedLocked(st *serverState) bool {
	if st.cfg.Source == SourceUser {
		return true
	}
	h := st.cfg.Hash()
	if st.trusted == h {
		return true
	}
	return m.opts.IsTrusted != nil && m.opts.IsTrusted(st.cfg)
}

// Servers lists every configured server, sorted by name.
func (m *Manager) Servers() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.servers))
	for _, st := range m.servers {
		m.loadCacheLocked(st)
		s := Status{Name: st.cfg.Name, Type: st.cfg.Type, Source: st.cfg.Source, State: st.state,
			Problem: st.problem, Tools: len(st.tools), Cached: st.cached, Trusted: m.trustedLocked(st),
			ToolsChanged: m.toolsChangedLocked(st) && st.toolsOK != st.toolsHash, server: st.cfg}
		if st.cfg.Type == TypeHTTP && m.opts.Tokens != nil {
			var e ai.MCPOAuth
			e, s.SignedIn = m.opts.Tokens.Load(st.cfg.Name)
			s.Expired = s.SignedIn && e.RefreshToken == "" && e.Expired(time.Now())
		}
		if st.state == StateReconnecting {
			s.RetryIn = time.Until(st.retryAt)
		}
		if st.proc != nil {
			s.Pid = st.proc.Pid()
			if st.proc.shared {
				s.Shared, s.Sessions = true, st.proc.Sessions()
			}
			s.OldTransport = st.proc.legacy
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Count is the number of configured servers (cheap: the chat header
// reads it every frame).
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.servers)
}

// NeedSignIn lists the servers whose last answer was "sign-in needed",
// sorted (cheap: the chat header reads it every frame).
func (m *Manager) NeedSignIn() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for name, st := range m.servers {
		if st.state == StateSignIn {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// Running counts running servers.
func (m *Manager) Running() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, st := range m.servers {
		if st.proc != nil {
			n++
		}
	}
	return n
}

func (m *Manager) get(name string) (*serverState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	if !ok {
		return nil, fmt.Errorf("no MCP server %q in mcp.json", name)
	}
	return st, nil
}

// ensure starts the server when it does not run. Caller holds st.op.
func (m *Manager) ensure(ctx context.Context, st *serverState, ask AskTrust) (*Process, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("MCP is shut down")
	}
	if st.state == StateInvalid {
		p := st.problem
		m.mu.Unlock()
		return nil, errors.New(p)
	}
	if st.state == StateDisabled {
		m.mu.Unlock()
		return nil, fmt.Errorf("MCP server %q is off (Enable it in AI: MCP Servers…)", st.cfg.Name)
	}
	if st.proc != nil {
		p := st.proc
		m.mu.Unlock()
		return p, nil
	}
	cfg := st.cfg
	hash := cfg.Hash()
	trusted := m.trustedLocked(st)
	denied := st.denied == hash
	m.mu.Unlock()

	if !trusted {
		if denied || ask == nil {
			return nil, ErrNotTrusted
		}
		a := ask(ctx, cfg)
		m.mu.Lock()
		switch a {
		case TrustOnce, TrustAlways:
			st.trusted = hash
		default:
			st.denied = hash
		}
		m.mu.Unlock()
		if a == TrustAlways && m.opts.SaveTrust != nil {
			m.opts.SaveTrust(cfg)
		}
		if a == TrustDeny {
			m.log(cfg.Name, "not started: the user denied it")
			return nil, ErrNotTrusted
		}
	}

	var x Expander
	if m.opts.Expander != nil {
		x = m.opts.Expander()
	}
	exp, err := cfg.Expand(x)
	if err != nil {
		m.setFailed(st, err)
		return nil, err
	}
	name := cfg.Name
	opts := Options{
		StartTimeout: time.Duration(cfg.StartTimeout) * time.Second,
		Roots:        m.opts.Roots,
		Log:          func(line string) { m.log(name, "%s", line) },
		ToolsChanged: func() {
			m.mu.Lock()
			st.haveTools = false // listed again on the next turn
			m.mu.Unlock()
			// The disk cache holds the old list: drop it, or the next
			// turn would read it back instead of asking the server.
			if p := m.cachePath(name); p != "" {
				_ = os.Remove(p)
			}
		},
	}
	var p *Process
	if cfg.Type == TypeHTTP {
		p, err = m.startHTTP(ctx, st, exp, opts)
	} else {
		p, err = m.startStdio(ctx, exp, opts)
	}
	if err != nil {
		if !errors.Is(err, ErrSignInNeeded) {
			m.setFailed(st, err)
		}
		if cfg.Type == TypeHTTP && retryable(err) {
			m.scheduleReconnect(st, err)
		} else {
			m.mu.Lock()
			m.stopRetryLocked(st)
			st.retryN = 0
			m.mu.Unlock()
		}
		return nil, err
	}
	info := p.Client.ServerInfo()
	m.log(name, "connected: %s %s, MCP %s", info.Name, info.Version, p.Client.Version())
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		p.Close()
		return nil, errors.New("MCP is shut down")
	}
	st.proc, st.state, st.problem = p, StateRunning, ""
	m.stopRetryLocked(st)
	st.retryN = 0
	m.mu.Unlock()
	crash.Go("an MCP server watcher", func() {
		<-p.Done()
		m.mu.Lock()
		if st.proc == p {
			st.proc, st.state = nil, StateStopped
			if st.idle != nil {
				st.idle.Stop()
				st.idle = nil
			}
		}
		m.mu.Unlock()
	})
	return p, nil
}

// startStdio uses the shared server when sharing is on and possible,
// else its own process.
func (m *Manager) startStdio(ctx context.Context, exp Server, opts Options) (*Process, error) {
	if err := CheckRuntime(exp.Command); err != nil {
		return nil, err // before a broker or a process starts
	}
	m.mu.Lock()
	share := m.opts.Share
	m.mu.Unlock()
	if share != nil {
		var roots []string
		if m.opts.Roots != nil && exp.Share != ShareUser {
			roots = m.opts.Roots()
		}
		if exp.Share == ShareUser && exp.Cwd == "" {
			// Not the folder of the project that happened to start it.
			if home, err := os.UserHomeDir(); err == nil {
				exp.Cwd = home
			}
		}
		p, err := startShared(ctx, share, exp, roots, opts)
		if !errors.Is(err, errShareUnavailable) {
			return p, err
		}
		m.log(exp.Name, "not shared (%v): running its own process", err)
	}
	return StartStdio(ctx, exp, opts)
}

func (m *Manager) setFailed(st *serverState, err error) {
	m.mu.Lock()
	st.state, st.problem = StateFailed, err.Error()
	m.mu.Unlock()
	m.log(st.cfg.Name, "failed: %v", err)
}

// SetTimeouts changes the idle and call timeouts (the Settings modal);
// running servers get the new idle timeout at once. call <= 0 keeps
// the call timeout.
func (m *Manager) SetTimeouts(idle, call time.Duration) {
	m.mu.Lock()
	m.opts.IdleTimeout = idle
	if call > 0 {
		m.opts.CallTimeout = call
	}
	var running []*serverState
	for _, st := range m.servers {
		if st.proc != nil {
			running = append(running, st)
		}
	}
	m.mu.Unlock()
	for _, st := range running {
		m.armIdle(st)
	}
}

// SetShare turns shared servers on (o) or off (nil) for the next starts;
// running servers keep how they run.
func (m *Manager) SetShare(o *ShareOptions) {
	m.mu.Lock()
	m.opts.Share = o
	m.mu.Unlock()
}

// Timeouts returns the idle and call timeouts in use.
func (m *Manager) Timeouts() (idle, call time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.opts.IdleTimeout, m.opts.CallTimeout
}

// armIdle (re)starts the idle timer of a running server.
func (m *Manager) armIdle(st *serverState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.opts.IdleTimeout
	if st.proc == nil {
		return
	}
	if st.idle != nil {
		st.idle.Stop()
		st.idle = nil
	}
	if d <= 0 {
		return
	}
	st.idleGen++
	gen := st.idleGen
	st.idle = time.AfterFunc(d, func() { m.idleStop(st, gen) })
}

func (m *Manager) idleStop(st *serverState, gen int) {
	if !st.op.TryLock() { // a call is running: try again later
		m.armIdle(st)
		return
	}
	defer st.op.Unlock()
	m.mu.Lock()
	if gen != st.idleGen || st.proc == nil {
		m.mu.Unlock()
		return
	}
	p := st.proc
	st.proc, st.state, st.idle = nil, StateStopped, nil
	m.mu.Unlock()
	m.log(st.cfg.Name, "idle: stopped")
	p.Close()
}

// ── Tool lists ─────────────────────────────────────────────────────────

type cacheFile struct {
	Hash   string     `json:"hash"`
	Server ServerInfo `json:"server"`
	Tools  []Tool     `json:"tools"`
}

func (m *Manager) cachePath(name string) string {
	if m.opts.CacheDir == "" {
		return ""
	}
	return filepath.Join(m.opts.CacheDir, name+".tools.json")
}

// loadCacheLocked fills st.tools from disk when nothing is known yet.
func (m *Manager) loadCacheLocked(st *serverState) {
	if st.haveTools || st.state == StateInvalid {
		return
	}
	p := m.cachePath(st.cfg.Name)
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var c cacheFile
	if json.Unmarshal(b, &c) != nil || c.Hash != st.cfg.Hash() {
		return
	}
	st.tools, st.haveTools, st.cached = c.Tools, true, true
	st.toolsHash = ToolsHash(c.Tools)
}

func (m *Manager) saveCache(cfg Server, info ServerInfo, tools []Tool) {
	p := m.cachePath(cfg.Name)
	if p == "" {
		return
	}
	b, err := json.MarshalIndent(cacheFile{Hash: cfg.Hash(), Server: info, Tools: tools}, "", "  ")
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(p), 0o755) == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}

// list asks a server for its tools (starting it) and caches them.
func (m *Manager) list(ctx context.Context, st *serverState, ask AskTrust) ([]Tool, error) {
	st.op.Lock()
	defer st.op.Unlock()
	p, err := m.ensure(ctx, st, ask)
	if err != nil {
		return nil, err
	}
	lctx, cancel := context.WithTimeout(ctx, initTimeout)
	defer cancel()
	tools, err := p.Client.ListTools(lctx)
	if err != nil {
		m.log(st.cfg.Name, "tools/list failed: %v", err)
		return nil, err
	}
	m.storeTools(st, p, tools)
	m.armIdle(st)
	return tools, nil
}

// storeTools keeps a fresh tool list (memory + disk cache) and checks
// it against the list the user trusted: the first list is accepted by
// itself, a different one is logged as changed.
func (m *Manager) storeTools(st *serverState, p *Process, tools []Tool) {
	h := ToolsHash(tools)
	m.mu.Lock()
	st.tools, st.haveTools, st.cached, st.toolsHash = tools, true, false, h
	cfg := st.cfg
	changed := m.toolsChangedLocked(st)
	m.mu.Unlock()
	m.saveCache(cfg, p.Client.ServerInfo(), tools)
	m.log(cfg.Name, "%d tools", len(tools))
	if m.opts.TrustedTools != nil && m.opts.SaveTrustedTools != nil && m.opts.TrustedTools(cfg.Name) == "" {
		m.opts.SaveTrustedTools(cfg.Name, h)
	}
	if changed {
		m.log(cfg.Name, "its tools changed since you trusted them: review them with Show tools")
	}
}

// toolsChangedLocked reports a tool list that differs from the trusted
// one (whatever the user answered this session).
func (m *Manager) toolsChangedLocked(st *serverState) bool {
	if m.opts.TrustedTools == nil || st.toolsHash == "" {
		return false
	}
	want := m.opts.TrustedTools(st.cfg.Name)
	return want != "" && want != st.toolsHash
}

// ToolsHash identifies a tool list: names, descriptions, schemas and
// annotations, in any order.
func ToolsHash(tools []Tool) string {
	sorted := append([]Tool(nil), tools...)
	for i := range sorted {
		// Size hints and output schemas are not a change the user reviews
		// (the hash stays as before they were read).
		sorted[i].Meta, sorted[i].OutputSchema = nil, nil
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	b, _ := json.Marshal(sorted)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// AcceptTools trusts a server's current tool list (saved), so its tools
// use the normal gates again.
func (m *Manager) AcceptTools(name string) {
	m.mu.Lock()
	st, ok := m.servers[name]
	h := ""
	if ok {
		m.loadCacheLocked(st)
		h = st.toolsHash
		st.toolsOK = h
	}
	m.mu.Unlock()
	if h != "" && m.opts.SaveTrustedTools != nil {
		m.opts.SaveTrustedTools(name, h)
	}
}

// checkTools asks the user about a changed tool list once per session
// and per list. ok=false: the user did not accept it (the tools then all
// ask first).
func (m *Manager) checkTools(ctx context.Context, st *serverState, ask AskTrust) (ok bool) {
	m.mu.Lock()
	changed := m.toolsChangedLocked(st)
	h, cfg := st.toolsHash, st.cfg
	accepted, refused := st.toolsOK == h, st.toolsNo == h
	m.mu.Unlock()
	switch {
	case !changed || accepted:
		return true
	case refused || ask == nil:
		return false
	}
	q := cfg
	q.TrustReason = ReasonToolsChanged
	a := ask(ctx, q)
	m.mu.Lock()
	if a == TrustDeny {
		st.toolsNo = h
	} else {
		st.toolsOK = h
	}
	m.mu.Unlock()
	if a == TrustAlways && m.opts.SaveTrustedTools != nil {
		m.opts.SaveTrustedTools(cfg.Name, h)
	}
	return a != TrustDeny
}

// Tools returns the tools of every usable server for a chat turn: from
// memory or the disk cache, or — only when nothing is known — by
// starting the server once. Servers that fail are skipped (and logged).
func (m *Manager) Tools(ctx context.Context, ask AskTrust) []ServerTools {
	m.mu.Lock()
	var names []string
	for name, st := range m.servers {
		if st.state != StateInvalid && st.state != StateSignIn && st.state != StateDisabled { // no 401 round trip per turn
			if st.state == StateReconnecting && !st.haveTools {
				continue // the reconnect timer starts it, not each turn
			}
			names = append(names, name)
		}
	}
	m.mu.Unlock()
	sort.Strings(names)
	var out []ServerTools
	for _, name := range names {
		st, err := m.get(name)
		if err != nil {
			continue
		}
		m.mu.Lock()
		m.loadCacheLocked(st)
		tools, have, cfg := st.tools, st.haveTools, st.cfg
		denied := st.denied == cfg.Hash()
		m.mu.Unlock()
		if !have {
			if denied {
				continue
			}
			if tools, err = m.list(ctx, st, ask); err != nil {
				continue
			}
		}
		if len(tools) > 0 {
			ok := m.checkTools(ctx, st, ask)
			out = append(out, ServerTools{Server: cfg, Tools: tools, ToolsChanged: !ok})
		}
	}
	return out
}

// KnownTools returns a server's tools from memory or the disk cache; it
// never starts the server (the UI's Show tools).
func (m *Manager) KnownTools(name string) (Server, []Tool, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.servers[name]
	if !ok {
		return Server{}, nil, false
	}
	m.loadCacheLocked(st)
	return st.cfg, st.tools, st.haveTools
}

// Connect starts a server and lists its tools (the UI's Connect /
// Refresh tools).
func (m *Manager) Connect(ctx context.Context, ask AskTrust, name string) error {
	st, err := m.get(name)
	if err != nil {
		return err
	}
	_, err = m.list(ctx, st, ask)
	return err
}

// Call runs one tool, starting the server when needed.
func (m *Manager) Call(ctx context.Context, ask AskTrust, server, tool string, args map[string]any) (CallResult, error) {
	st, err := m.get(server)
	if err != nil {
		return CallResult{}, err
	}
	st.op.Lock()
	defer st.op.Unlock()
	m.mu.Lock()
	wasRunning := st.proc != nil
	m.mu.Unlock()
	p, err := m.ensure(ctx, st, ask)
	if err != nil {
		return CallResult{}, err
	}
	if !wasRunning && m.opts.TrustedTools != nil {
		// A fresh start: list again, so a changed tool list is noticed
		// before the call runs.
		lctx, cancel := context.WithTimeout(ctx, initTimeout)
		tools, lerr := p.Client.ListTools(lctx)
		cancel()
		if lerr == nil {
			m.storeTools(st, p, tools)
		}
		m.mu.Lock()
		blocked := m.toolsChangedLocked(st) && st.toolsOK != st.toolsHash && st.toolsNo != st.toolsHash
		m.mu.Unlock()
		if blocked {
			m.armIdle(st)
			return CallResult{}, fmt.Errorf("MCP %s: %w", server, ErrToolsChanged)
		}
	}
	m.mu.Lock()
	timeout := m.opts.CallTimeout
	m.mu.Unlock()
	if t := st.cfg.Timeout; t > 0 {
		timeout = time.Duration(t) * time.Second
	}
	res, err := p.Client.CallToolTimeout(ctx, tool, args, timeout)
	if errors.Is(err, ErrSessionExpired) {
		// The remote server forgot the session: connect again, once.
		m.log(server, "session ended by the server: connecting again")
		m.mu.Lock()
		if st.proc == p {
			st.proc, st.state = nil, StateStopped
		}
		m.mu.Unlock()
		if p, err = m.ensure(ctx, st, ask); err != nil {
			return CallResult{}, err
		}
		res, err = p.Client.CallToolTimeout(ctx, tool, args, timeout)
	}
	var ae *AuthError
	if errors.As(err, &ae) && ae.InsufficientScope && m.stepUp(st, ae) {
		m.armIdle(st)
		return res, fmt.Errorf("MCP %s: %w", server, ErrMoreAccess)
	}
	if err != nil && st.cfg.Type == TypeHTTP && isConnErr(err) && m.idempotent(st, tool) {
		// A read-only or idempotent call is safe to run again (TS-11):
		// one new connection, one more try.
		m.log(server, "%s: the connection dropped: trying once more (the tool is safe to repeat)", tool)
		m.mu.Lock()
		if st.proc == p {
			st.proc, st.state = nil, StateStopped
		}
		m.mu.Unlock()
		crash.Go("an MCP server stop", func() { p.Close() })
		if np, nerr := m.ensure(ctx, st, ask); nerr == nil {
			p = np
			res, err = p.Client.CallToolTimeout(ctx, tool, args, timeout)
		} else {
			return res, nerr // ensure has set the reconnect up
		}
	}
	if err != nil && st.cfg.Type == TypeHTTP && isConnErr(err) {
		// The network went away under a running connection: drop it and
		// reconnect later (the call is not run again).
		m.mu.Lock()
		if st.proc == p {
			st.proc = nil
		}
		m.mu.Unlock()
		crash.Go("an MCP server stop", func() { p.Close() })
		m.setFailed(st, err)
		m.scheduleReconnect(st, err)
		return res, err
	}
	m.armIdle(st)
	if errors.Is(err, ErrCallTimeout) {
		m.log(server, "%s: no answer in %v", tool, timeout)
		return res, fmt.Errorf("MCP %s: no answer in %v (raise \"timeout\" for this server in mcp.json)", server, timeout)
	}
	return res, err
}

// Caller binds ask for the adapter (AITools).
func (m *Manager) Caller(ask AskTrust) CallFunc {
	return func(ctx context.Context, server, tool string, args map[string]any) (CallResult, error) {
		return m.Call(ctx, ask, server, tool, args)
	}
}

// Stop stops one server (it starts again on the next call). A shared
// server stops for every session that uses it.
func (m *Manager) Stop(name string) {
	st, err := m.get(name)
	if err != nil {
		return
	}
	m.mu.Lock()
	p := st.proc
	st.proc = nil
	if st.state != StateInvalid && st.state != StateDisabled {
		st.state, st.problem = StateStopped, ""
	}
	if st.idle != nil {
		st.idle.Stop()
		st.idle = nil
	}
	m.stopRetryLocked(st)
	st.retryN = 0
	m.mu.Unlock()
	if p != nil {
		m.log(name, "stopped")
		p.stopShared()
	}
}

// Close stops every server and refuses new starts. cmd/termocode calls
// it on quit (app.StopMCP).
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	var procs []*Process
	for _, st := range m.servers {
		if st.proc != nil {
			procs = append(procs, st.proc)
			st.proc, st.state = nil, StateStopped
		}
		if st.idle != nil {
			st.idle.Stop()
			st.idle = nil
		}
		m.stopRetryLocked(st)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		crash.Go("an MCP server stop", func() { defer wg.Done(); p.Close() })
	}
	wg.Wait()
	m.end()
	m.retrying.Wait()
}
