package extproc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// ProtocolVersion is sent in "initialize"; it changes only when a method
// changes in a way old extensions would get wrong.
const ProtocolVersion = 1

// ErrUnknown: no process extension has that name.
var ErrUnknown = errors.New("no such process extension")

// MaxReadFile caps readFile (bytes).
const MaxReadFile = 4 << 20

// Config sets up a Host. Zero values pick the defaults.
type Config struct {
	Root    string // workspace root: readFile / openFile stay inside it
	Version string // termocode version, sent in initialize

	MaxRestarts int                             // restarts after crashes before giving up (default 3)
	Backoff     func(attempt int) time.Duration // wait before restart n (default 0.5 s, 1 s, 2 s … max 10 s)
	InitTimeout time.Duration                   // initialize must answer within (default 10 s)
	CallTimeout time.Duration                   // command/execute must answer within (default 30 s)
	StopTimeout time.Duration                   // grace time on stop before the kill (default 2 s)
}

func (c *Config) defaults() {
	if c.MaxRestarts == 0 {
		c.MaxRestarts = 3
	}
	if c.Backoff == nil {
		c.Backoff = func(n int) time.Duration {
			d := 500 * time.Millisecond << max(n-1, 0)
			return min(d, 10*time.Second)
		}
	}
	if c.InitTimeout == 0 {
		c.InitTimeout = 10 * time.Second
	}
	if c.CallTimeout == 0 {
		c.CallTimeout = 30 * time.Second
	}
	if c.StopTimeout == 0 {
		c.StopTimeout = 2 * time.Second
	}
}

// State is where one process extension is in its life.
type State int

const (
	StateIdle       State = iota // not started yet (lazy)
	StateStarting                // process started, waiting for initialize
	StateRunning                 // initialized
	StateRestarting              // crashed, waiting to start again
	StateStopping                // shutting down
	StateStopped                 // stopped by termocode
	StateFailed                  // could not start, or crashed too often
)

func (s State) String() string {
	return [...]string{"idle", "starting", "running", "restarting", "stopping", "stopped", "failed"}[s]
}

// EventKind tells what an Event reports.
type EventKind int

const (
	EvStarted EventKind = iota + 1 // initialize answered (Pid, Gen)
	EvMessage                      // a request or notification for the UI (Msg)
	EvExited                       // the process ended (Err, Restarting)
	EvFailed                       // gave up: start failed or too many crashes (Err)
)

func (k EventKind) String() string {
	switch k {
	case EvStarted:
		return "started"
	case EvMessage:
		return "message"
	case EvExited:
		return "exited"
	case EvFailed:
		return "failed"
	}
	return "?"
}

// Event is what the Host reports to the UI, in order per extension.
type Event struct {
	Ext        string
	Kind       EventKind
	Gen        int // which run of the process (1, 2, … after restarts)
	Pid        int
	Msg        Message
	Err        string
	Restarting bool
}

// Status describes one process extension.
type Status struct {
	Name     string
	State    State
	Pid      int
	Restarts int
	Err      string
}

// uiMethods are the extension → termocode methods the UI handles. A
// request among them (other than prompt / pick, which the UI answers
// with Host.Reply) is answered with null at once.
var uiMethods = map[string]bool{
	"registerCommand":    true,
	"registerStatusItem": true,
	"statusItem/update":  true,
	"registerPanel":      true,
	"panel/setContent":   true,
	"notify":             true,
	"preview":            true,
	"openFile":           true,
	"prompt":             true,
	"pick":               true,
}

// Host runs the process extensions. All its methods are safe from any
// goroutine; only Set, StopAll and Close wait (up to Config.StopTimeout
// per extension, in parallel) and must not run on the UI loop.
type Host struct {
	cfg    Config
	events chan Event
	closed chan struct{}
	once   sync.Once

	mu    sync.Mutex
	procs map[string]*proc
	order []string
	log   []string
	gen   int // last run number, host-wide (Event.Gen)
}

const logMax = 500

// NewHost makes a Host with no extensions.
func NewHost(cfg Config) *Host {
	cfg.defaults()
	return &Host{
		cfg:    cfg,
		events: make(chan Event, 1024),
		closed: make(chan struct{}),
		procs:  map[string]*proc{},
	}
}

// Done is closed by Close.
func (h *Host) Done() <-chan struct{} { return h.closed }

// Events delivers what the extensions do. Read it all the time: a full
// channel slows the extensions down (never the reader).
func (h *Host) Events() <-chan Event { return h.events }

// Log returns the last lines of the extension log (stderr, log calls,
// starts, exits).
func (h *Host) Log() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.log...)
}

func (h *Host) logf(name, format string, args ...any) {
	line := time.Now().Format("15:04:05") + " " + name + ": " + fmt.Sprintf(format, args...)
	h.mu.Lock()
	h.log = append(h.log, line)
	if len(h.log) > logMax {
		h.log = append([]string(nil), h.log[len(h.log)-logMax:]...)
	}
	h.mu.Unlock()
}

// Set replaces the extension list: it stops every running process and
// keeps the process extensions of exts (Lua ones are ignored). Nothing
// starts yet: call StartAtStartup (the UI does, once it knows the new
// list). Every later event has a Gen above the returned base, so events
// of the old processes still in the channel can be told apart.
func (h *Host) Set(exts []ext.Extension) (base int) {
	h.StopAll()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.procs = map[string]*proc{}
	h.order = nil
	for _, x := range exts {
		if !x.IsProcess() {
			continue
		}
		h.procs[x.Name] = &proc{h: h, x: x}
		h.order = append(h.order, x.Name)
	}
	return h.gen
}

// StartAtStartup starts the extensions that start with termocode
// (ext.Manifest.StartsAtStartup). It does not wait.
func (h *Host) StartAtStartup() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range h.order {
		if p := h.procs[n]; p.x.Manifest.StartsAtStartup() {
			p.startLocked()
		}
	}
}

// Status lists the process extensions in name order.
func (h *Host) Status() []Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Status, 0, len(h.order))
	for _, n := range h.order {
		p := h.procs[n]
		out = append(out, Status{Name: n, State: p.state, Pid: p.pid, Restarts: p.restarts, Err: p.lastErr})
	}
	return out
}

// Start starts name in the background when it is not running.
func (h *Host) Start(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p := h.procs[name]; p != nil {
		p.startLocked()
	}
}

// ensure starts name if needed and waits until it is initialized.
func (h *Host) ensure(ctx context.Context, name string) (*Conn, error) {
	for {
		h.mu.Lock()
		p := h.procs[name]
		if p == nil {
			h.mu.Unlock()
			return nil, fmt.Errorf("%s: %w", name, ErrUnknown)
		}
		switch p.state {
		case StateRunning:
			c := p.conn
			h.mu.Unlock()
			return c, nil
		case StateFailed:
			err := p.lastErr
			h.mu.Unlock()
			return nil, fmt.Errorf("extension %s failed: %s", name, err)
		case StateStopping:
			h.mu.Unlock()
			return nil, fmt.Errorf("extension %s is stopping", name)
		case StateIdle, StateStopped:
			p.startLocked()
		}
		ready := p.ready
		h.mu.Unlock()
		select {
		case <-ready:
		case <-ctx.Done():
			return nil, fmt.Errorf("extension %s did not start: %w", name, ErrTimeout)
		}
	}
}

// Execute runs command id (as registered, "<ext>.<id>") in extension
// name, starting it first when needed. args are passed as "args".
func (h *Host) Execute(ctx context.Context, name, id string, args []string) error {
	ictx, cancel := context.WithTimeout(ctx, h.cfg.InitTimeout)
	c, err := h.ensure(ictx, name)
	cancel()
	if err != nil {
		return err
	}
	cctx, cancel := context.WithTimeout(ctx, h.cfg.CallTimeout)
	defer cancel()
	params := map[string]any{"id": id}
	if args != nil {
		params["args"] = args
	}
	_, err = c.Call(cctx, "command/execute", params)
	return err
}

// Event sends an editor event ("save", "open", …) for path to every
// running extension, and starts the ones that activate on it (they get
// the event once initialized). It never waits.
func (h *Host) Event(event, path string) {
	params := map[string]any{"event": event, "path": path}
	h.mu.Lock()
	var wake []string
	for _, n := range h.order {
		p := h.procs[n]
		switch {
		case p.state == StateRunning:
			_ = p.conn.Notify("event/notify", params)
		case (p.state == StateIdle || p.state == StateStopped) && p.x.Manifest.ActivatesOn(event):
			p.startLocked()
			wake = append(wake, n)
		}
	}
	h.mu.Unlock()
	for _, n := range wake {
		go func(n string) {
			ctx, cancel := context.WithTimeout(context.Background(), h.cfg.InitTimeout)
			defer cancel()
			if c, err := h.ensure(ctx, n); err == nil {
				_ = c.Notify("event/notify", params)
			}
		}(n)
	}
}

// Notify sends a notification to name if it is running.
func (h *Host) Notify(name, method string, params any) {
	h.mu.Lock()
	p := h.procs[name]
	var c *Conn
	if p != nil && p.state == StateRunning {
		c = p.conn
	}
	h.mu.Unlock()
	if c != nil {
		_ = c.Notify(method, params)
	}
}

// Reply answers request id of run gen of extension name (prompt, pick).
// A reply for an older run is dropped.
func (h *Host) Reply(name string, gen int, id json.RawMessage, result any, rerr *Error) {
	h.mu.Lock()
	p := h.procs[name]
	var c *Conn
	if p != nil && p.gen == gen {
		c = p.conn
	}
	h.mu.Unlock()
	if c != nil {
		_ = c.Reply(id, result, rerr)
	}
}

// StopAll stops every process: "shutdown" request, "exit" notification,
// stdin closed, then after Config.StopTimeout a kill of the whole process
// group. It returns when all are gone.
func (h *Host) StopAll() {
	h.mu.Lock()
	var wait []chan struct{}
	for _, p := range h.procs {
		if p.done == nil {
			continue
		}
		select {
		case <-p.done:
			continue
		default:
		}
		if p.state != StateStopping {
			p.state = StateStopping
			close(p.stop)
		}
		wait = append(wait, p.done)
	}
	h.mu.Unlock()
	for _, d := range wait {
		<-d
	}
}

// Close stops everything; the Host cannot be used after.
func (h *Host) Close() {
	h.once.Do(func() { close(h.closed) })
	h.StopAll()
}

// ── one extension ────────────────────────────────────────────────────────

type proc struct {
	h *Host
	x ext.Extension

	// guarded by h.mu
	state       State
	gen         int
	pid         int
	conn        *Conn
	ready       chan struct{} // closed when this start is initialized or ended
	readyClosed bool
	restarts    int
	lastErr     string
	stop        chan struct{} // closed to stop the supervisor
	done        chan struct{} // closed when the supervisor returned
}

// startLocked starts the supervisor if it is not running. h.mu is held.
func (p *proc) startLocked() {
	switch p.state {
	case StateIdle, StateStopped:
	default:
		return
	}
	p.state = StateStarting
	p.ready = make(chan struct{})
	p.readyClosed = false
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	p.restarts = 0
	p.lastErr = ""
	go p.supervise(p.stop, p.done)
}

func (p *proc) closeReadyLocked() {
	if !p.readyClosed {
		p.readyClosed = true
		close(p.ready)
	}
}

// emit hands ev to the UI; it gives up when the host closes or this
// extension is being stopped (nobody may read any more).
func (p *proc) emit(ev Event, stop <-chan struct{}) {
	ev.Ext = p.x.Name
	select {
	case p.h.events <- ev:
	case <-p.h.closed:
	case <-stop:
		select { // still try, without waiting
		case p.h.events <- ev:
		default:
		}
	}
}

// stableRun: a run this long resets the crash counter.
const stableRun = time.Minute

func (p *proc) supervise(stop, done chan struct{}) {
	defer close(done)
	h := p.h
	for {
		began := time.Now()
		exitErr, stopped, startErr := p.runOnce(stop)
		h.mu.Lock()
		p.conn = nil
		p.pid = 0
		if stopped {
			p.state = StateStopped
			p.closeReadyLocked()
			gen := p.gen
			h.mu.Unlock()
			h.logf(p.x.Name, "stopped")
			p.emit(Event{Kind: EvExited, Gen: gen, Err: exitErr}, stop)
			return
		}
		if startErr != nil {
			p.state = StateFailed
			p.lastErr = startErr.Error()
			p.closeReadyLocked()
			h.mu.Unlock()
			h.logf(p.x.Name, "cannot start: %v", startErr)
			p.emit(Event{Kind: EvFailed, Gen: p.gen, Err: startErr.Error()}, stop)
			return
		}
		if time.Since(began) >= stableRun {
			p.restarts = 0
		}
		p.restarts++
		p.lastErr = exitErr
		gen := p.gen
		if p.restarts > h.cfg.MaxRestarts {
			p.state = StateFailed
			p.closeReadyLocked()
			h.mu.Unlock()
			msg := fmt.Sprintf("crashed %d times, last: %s; not restarted (Extensions: Reload starts it again)", p.restarts, exitErr)
			h.logf(p.x.Name, "%s", msg)
			p.emit(Event{Kind: EvExited, Gen: gen, Err: exitErr}, stop)
			p.emit(Event{Kind: EvFailed, Gen: gen, Err: msg}, stop)
			return
		}
		attempt := p.restarts
		p.state = StateRestarting
		p.closeReadyLocked() // wake waiters; they wait again on the new one
		p.ready = make(chan struct{})
		p.readyClosed = false
		h.mu.Unlock()
		h.logf(p.x.Name, "crashed (%s); restart %d of %d", exitErr, attempt, h.cfg.MaxRestarts)
		p.emit(Event{Kind: EvExited, Gen: gen, Err: exitErr, Restarting: true}, stop)
		if d := h.cfg.Backoff(attempt); d > 0 {
			t := time.NewTimer(d)
			select {
			case <-t.C:
			case <-stop:
				t.Stop()
			}
		}
		select {
		case <-stop:
			h.mu.Lock()
			p.state = StateStopped
			p.closeReadyLocked()
			h.mu.Unlock()
			return
		default:
		}
		h.mu.Lock()
		p.state = StateStarting
		h.mu.Unlock()
	}
}

// command builds the exec.Cmd: cwd is the extension folder, a relative
// command with a slash is relative to it.
func (p *proc) command() *exec.Cmd {
	run := p.x.Manifest.Run
	name := run.Command
	if !filepath.IsAbs(name) && strings.ContainsRune(name, '/') {
		name = filepath.Join(p.x.Dir, name)
	}
	cmd := spawn.Command(name, run.Args...)
	cmd.Dir = p.x.Dir
	env := append(os.Environ(),
		"TERMOCODE_EXTENSION="+p.x.Name,
		"TERMOCODE_WORKSPACE="+p.h.cfg.Root,
		"TERMOCODE_VERSION="+p.h.cfg.Version,
	)
	for k, v := range run.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env
	setProcGroup(cmd)
	return cmd
}

// runOnce runs the process until it exits or stop closes. It returns the
// exit reason, whether it was a stop, and a start error.
func (p *proc) runOnce(stop <-chan struct{}) (exitErr string, stopped bool, startErr error) {
	h := p.h
	cmd := p.command()
	inR, inW, err := os.Pipe()
	if err != nil {
		return "", false, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return "", false, err
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return "", false, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	exited, err := startAndWait(cmd)
	// The child has its copies now (or failed); close ours.
	inR.Close()
	outW.Close()
	errW.Close()
	if err != nil {
		inW.Close()
		outR.Close()
		errR.Close()
		return "", false, err
	}
	pid := cmd.Process.Pid

	h.mu.Lock()
	h.gen++
	p.gen = h.gen
	gen := p.gen
	p.pid = pid
	conn := NewConn(outR, inW, p.handler(gen, stop))
	p.conn = conn
	h.mu.Unlock()
	h.logf(p.x.Name, "started (pid %d)", pid)

	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		sc := bufio.NewScanner(errR)
		sc.Buffer(make([]byte, 64<<10), 64<<10)
		for sc.Scan() {
			h.logf(p.x.Name, "stderr: %s", sc.Text())
		}
	}()
	go p.initialize(conn, gen, pid, stop)

	var werr error
	select {
	case werr = <-exited:
	case <-stop:
		stopped = true
		werr = p.shutdown(conn, pid, exited)
	}
	conn.Close()
	killGroup(pid) // children it left behind
	errR.Close()
	<-stderrDone
	if werr != nil {
		exitErr = werr.Error()
	} else {
		exitErr = "exited"
	}
	return exitErr, stopped, nil
}

// shutdown asks the process to stop and kills its group after the grace
// time.
func (p *proc) shutdown(conn *Conn, pid int, exited <-chan error) error {
	grace := p.h.cfg.StopTimeout
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), grace/2)
		defer cancel()
		_, _ = conn.Call(ctx, "shutdown", nil)
		_ = conn.Notify("exit", nil)
		// Give the writer a moment to send "exit", then EOF on stdin.
		conn.Close()
	}()
	select {
	case err := <-exited:
		return err
	case <-deadline.C:
	}
	p.h.logf(p.x.Name, "did not stop in %v; killed", grace)
	killGroup(pid)
	return <-exited
}

func (p *proc) initialize(conn *Conn, gen, pid int, stop <-chan struct{}) {
	h := p.h
	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.InitTimeout)
	defer cancel()
	_, err := conn.Call(ctx, "initialize", map[string]any{
		"protocolVersion":  ProtocolVersion,
		"name":             p.x.Name,
		"extensionDir":     p.x.Dir,
		"workspaceRoot":    h.cfg.Root,
		"termocodeVersion": h.cfg.Version,
		"capabilities": map[string]any{
			"methods": []string{"registerCommand", "registerStatusItem", "statusItem/update",
				"registerPanel", "panel/setContent", "notify", "preview", "prompt", "pick",
				"openFile", "readFile", "log"},
			"events": []string{"save", "open"},
		},
	})
	if err != nil {
		if errors.Is(err, ErrClosed) {
			return // it exited; supervise handles that
		}
		h.logf(p.x.Name, "initialize failed: %v", err)
		killGroup(pid)
		return
	}
	h.mu.Lock()
	if p.gen != gen || p.state == StateStopping {
		h.mu.Unlock()
		return
	}
	p.state = StateRunning
	p.closeReadyLocked()
	h.mu.Unlock()
	h.logf(p.x.Name, "initialized")
	p.emit(Event{Kind: EvStarted, Gen: gen, Pid: pid}, stop)
}

// handler answers what the host can answer itself (readFile, log) and
// passes UI methods on as events.
func (p *proc) handler(gen int, stop <-chan struct{}) Handler {
	h := p.h
	return func(c *Conn, m Message) {
		switch m.Method {
		case "log":
			var a struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(m.Params, &a)
			h.logf(p.x.Name, "%s", a.Message)
			if m.IsRequest() {
				_ = c.Reply(m.ID, nil, nil)
			}
			return
		case "readFile":
			if !m.IsRequest() {
				return
			}
			go func() {
				text, err := p.readFile(m.Params)
				if err != nil {
					_ = c.Reply(m.ID, nil, &Error{Code: CodeRequestFailed, Message: err.Error()})
					return
				}
				_ = c.Reply(m.ID, map[string]string{"text": text}, nil)
			}()
			return
		case "openFile":
			var a map[string]any
			_ = json.Unmarshal(m.Params, &a)
			path, _ := a["path"].(string)
			abs, err := InWorkspace(h.cfg.Root, path)
			if err != nil {
				if m.IsRequest() {
					_ = c.Reply(m.ID, nil, &Error{Code: CodeRequestFailed, Message: err.Error()})
				}
				h.logf(p.x.Name, "openFile %q refused: %v", path, err)
				return
			}
			a["path"] = abs
			m.Params, _ = json.Marshal(a)
		}
		if !uiMethods[m.Method] {
			h.logf(p.x.Name, "unknown method %q", m.Method)
			if m.IsRequest() {
				_ = c.Reply(m.ID, nil, &Error{Code: CodeMethodNotFound, Message: "method not found: " + m.Method})
			}
			return
		}
		if m.IsRequest() && m.Method != "prompt" && m.Method != "pick" {
			_ = c.Reply(m.ID, nil, nil)
		}
		p.emit(Event{Kind: EvMessage, Gen: gen, Msg: m}, stop)
	}
}

func (p *proc) readFile(params json.RawMessage) (string, error) {
	var a struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(params, &a); err != nil || a.Path == "" {
		return "", errors.New("readFile needs a path")
	}
	abs, err := InWorkspace(p.h.cfg.Root, a.Path)
	if err != nil {
		return "", err
	}
	f, err := os.Open(abs)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxReadFile+1))
	if err != nil {
		return "", err
	}
	if len(b) > MaxReadFile {
		return "", fmt.Errorf("file is bigger than %d bytes", MaxReadFile)
	}
	return string(b), nil
}
