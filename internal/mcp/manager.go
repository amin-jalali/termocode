package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
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
	StateStopped = "stopped"
	StateRunning = "running"
	StateFailed  = "failed"  // the last start failed (Problem says why); the next call tries again
	StateInvalid = "invalid" // the entry can not run (Problem says why)
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
	server             Server
}

// ServerTools is one server's tools for a chat turn.
type ServerTools struct {
	Server Server
	Tools  []Tool
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
}

// Manager is the set of configured servers.
type Manager struct {
	opts ManagerOptions

	mu       sync.Mutex
	servers  map[string]*serverState
	warnings []string
	closed   bool
}

// NewManager loads the config (it starts nothing).
func NewManager(o ManagerOptions) *Manager {
	if o.CallTimeout <= 0 {
		o.CallTimeout = 60 * time.Second
	}
	m := &Manager{opts: o, servers: map[string]*serverState{}}
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
	list, warns := Load(m.opts.UserPath, m.opts.ProjectPath)
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
		st := &serverState{cfg: s, state: StateStopped}
		if s.Problem != "" {
			st.state, st.problem = StateInvalid, s.Problem
		} else if s.Type != TypeStdio {
			st.state, st.problem = StateInvalid, "remote ("+s.Type+") servers are not supported yet"
		}
		next[s.Name] = st
	}
	for name, old := range m.servers {
		if _, ok := next[name]; !ok && old.proc != nil {
			stop = append(stop, old.proc)
		}
	}
	m.servers = next
	m.warnings = warns
	m.mu.Unlock()
	for _, p := range stop {
		go p.Close()
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
			Problem: st.problem, Tools: len(st.tools), Cached: st.cached, Trusted: m.trustedLocked(st), server: st.cfg}
		if st.proc != nil {
			s.Pid = st.proc.Pid()
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
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
	p, err := StartStdio(ctx, exp, Options{
		Roots: m.opts.Roots,
		Log:   func(line string) { m.log(name, "%s", line) },
		ToolsChanged: func() {
			m.mu.Lock()
			st.haveTools = false // listed again on the next turn
			m.mu.Unlock()
		},
	})
	if err != nil {
		m.setFailed(st, err)
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
	m.mu.Unlock()
	go func() {
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
	}()
	return p, nil
}

func (m *Manager) setFailed(st *serverState, err error) {
	m.mu.Lock()
	st.state, st.problem = StateFailed, err.Error()
	m.mu.Unlock()
	m.log(st.cfg.Name, "failed: %v", err)
}

// armIdle (re)starts the idle timer of a running server.
func (m *Manager) armIdle(st *serverState) {
	d := m.opts.IdleTimeout
	if d <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if st.proc == nil {
		return
	}
	if st.idle != nil {
		st.idle.Stop()
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
	m.mu.Lock()
	st.tools, st.haveTools, st.cached = tools, true, false
	cfg := st.cfg
	m.mu.Unlock()
	m.saveCache(cfg, p.Client.ServerInfo(), tools)
	m.log(cfg.Name, "%d tools", len(tools))
	m.armIdle(st)
	return tools, nil
}

// Tools returns the tools of every usable server for a chat turn: from
// memory or the disk cache, or — only when nothing is known — by
// starting the server once. Servers that fail are skipped (and logged).
func (m *Manager) Tools(ctx context.Context, ask AskTrust) []ServerTools {
	m.mu.Lock()
	var names []string
	for name, st := range m.servers {
		if st.state != StateInvalid {
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
			out = append(out, ServerTools{Server: cfg, Tools: tools})
		}
	}
	return out
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
	p, err := m.ensure(ctx, st, ask)
	if err != nil {
		return CallResult{}, err
	}
	timeout := m.opts.CallTimeout
	if t := st.cfg.Timeout; t > 0 {
		timeout = time.Duration(t) * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := p.Client.CallTool(cctx, tool, args)
	m.armIdle(st)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
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

// Stop stops one server (it starts again on the next call).
func (m *Manager) Stop(name string) {
	st, err := m.get(name)
	if err != nil {
		return
	}
	m.mu.Lock()
	p := st.proc
	st.proc = nil
	if st.state != StateInvalid {
		st.state, st.problem = StateStopped, ""
	}
	if st.idle != nil {
		st.idle.Stop()
		st.idle = nil
	}
	m.mu.Unlock()
	if p != nil {
		m.log(name, "stopped")
		p.Close()
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
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func(p *Process) { defer wg.Done(); p.Close() }(p)
	}
	wg.Wait()
}
