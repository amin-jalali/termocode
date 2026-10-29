//go:build unix

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/extproc"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/spawn"
)

// RunShareIfHelper runs the share broker (and exits) when ShareEnv is
// set. termocode's main calls it first; so does a test binary's TestMain.
// The server config comes on stdin; the broker answers "ok" or
// "error: …" on stdout, then serves until the server stops.
func RunShareIfHelper() {
	sock := os.Getenv(ShareEnv)
	if sock == "" {
		return
	}
	_ = os.Unsetenv(ShareEnv)
	os.Exit(runShareBroker(sock, os.Stdin, os.Stdout))
}

func runShareBroker(sock string, in io.Reader, out io.Writer) int {
	var cfg shareConfig
	if err := json.NewDecoder(in).Decode(&cfg); err != nil {
		fmt.Fprintf(out, "error: bad config: %v\n", err)
		return 1
	}
	b, err := startBroker(sock, cfg, session.CheckPeer)
	if err != nil {
		fmt.Fprintf(out, "error: %s\n", strings.ReplaceAll(err.Error(), "\n", " "))
		return 1
	}
	fmt.Fprintln(out, "ok")
	if c, ok := out.(io.Closer); ok {
		_ = c.Close()
	}
	<-b.done
	return 0
}

// broker owns one server process and serves it to many sessions.
type broker struct {
	cfg   shareConfig
	grace time.Duration
	ln    net.Listener
	peer  func(net.Conn) error
	cmd   *exec.Cmd
	srv   *extproc.Conn
	init  json.RawMessage // the server's initialize result
	exit  chan struct{}   // closed when the server process exited
	done  chan struct{}   // closed when the broker is finished

	mu      sync.Mutex
	clients map[uint64]*shareClient
	nextID  uint64
	timer   *time.Timer
	closed  bool
	stop    sync.Once
}

// shareClient is one connected session.
type shareClient struct {
	id    uint64
	conn  *extproc.Conn
	ready bool                          // initialized (guarded by broker.mu)
	calls map[string]context.CancelFunc // its request id → cancel (broker.mu)
}

// startBroker listens on sock, starts the server and runs its handshake.
func startBroker(sock string, cfg shareConfig, peer func(net.Conn) error) (*broker, error) {
	b := &broker{cfg: cfg, peer: peer, clients: map[uint64]*shareClient{},
		exit: make(chan struct{}), done: make(chan struct{})}
	b.grace = time.Duration(cfg.GraceMS) * time.Millisecond
	if b.grace <= 0 {
		b.grace = DefaultShareGrace
	}
	ln, err := ListenUnix(sock)
	if err != nil {
		return nil, err
	}
	b.ln = ln
	if err := b.startServer(); err != nil {
		_ = ln.Close()
		return nil, err
	}
	go func() {
		<-b.exit
		b.shutdown()
	}()
	b.mu.Lock()
	b.armLocked()
	b.mu.Unlock()
	go b.accept()
	return b, nil
}

// log sends a line to every session's MCP log.
func (b *broker) log(level, line string) {
	for _, c := range b.readyClients() {
		_ = c.conn.Notify("notifications/message", map[string]any{"level": level, "data": line})
	}
}

func (b *broker) readyClients() []*shareClient {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []*shareClient
	for _, c := range b.clients {
		if c.ready {
			out = append(out, c)
		}
	}
	return out
}

func (b *broker) pid() int {
	if b.cmd == nil || b.cmd.Process == nil {
		return 0
	}
	return b.cmd.Process.Pid
}

func (b *broker) startServer() error {
	if b.cfg.Command == "" {
		return errors.New("no command")
	}
	path, err := exec.LookPath(b.cfg.Command)
	if err != nil {
		return fmt.Errorf("%s not found (install it, or set command to a full path)", b.cfg.Command)
	}
	cmd := spawn.Command(path, b.cfg.Args...)
	cmd.Dir = b.cfg.Cwd
	cmd.Env = childEnv(os.Environ(), b.cfg.Env)
	setProcGroup(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", b.cfg.Command, err)
	}
	b.cmd = cmd
	go func() {
		sc := bufio.NewScanner(stderr)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			b.log("stderr", sc.Text())
		}
	}()
	go func() {
		_ = cmd.Wait()
		killGroup(cmd.Process.Pid)
		close(b.exit)
	}()
	b.srv = extproc.NewConnWith(stdout, stdin, b.fromServer, extproc.ConnOptions{
		Framing: extproc.FramingNewline,
		Skipped: func(line string) { b.log("stdout (not JSON)", line) },
		OnCancel: func(conn *extproc.Conn, id json.RawMessage) {
			_ = conn.Notify("notifications/cancelled", map[string]any{"requestId": id, "reason": "cancelled by the user"})
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), initTimeout)
	defer cancel()
	raw, err := b.srv.Call(ctx, "initialize", map[string]any{
		"protocolVersion": ProtocolVersion,
		"capabilities":    map[string]any{"roots": map[string]any{"listChanged": false}},
		"clientInfo":      map[string]any{"name": "termocode", "version": "1"},
	})
	if err == nil {
		var res initializeResult
		if err = json.Unmarshal(raw, &res); err == nil && !supportedVersion(res.ProtocolVersion) {
			err = fmt.Errorf("the server speaks MCP %q; termocode supports %s", res.ProtocolVersion, strings.Join(SupportedVersions, ", "))
		}
	}
	if err != nil {
		b.killServer()
		return fmt.Errorf("initialize: %w", err)
	}
	b.init = raw
	return b.srv.Notify("notifications/initialized", nil)
}

// killServer ends the server: stdin closed, then SIGTERM, then SIGKILL.
func (b *broker) killServer() {
	if b.srv != nil {
		b.srv.Close()
	}
	pid := b.pid()
	for _, sig := range []func(int){termGroup, killGroup} {
		select {
		case <-b.exit:
			return
		case <-time.After(time.Second):
		}
		sig(pid)
	}
	<-b.exit
}

// armLocked starts the grace timer when no session is left.
func (b *broker) armLocked() {
	if b.closed || len(b.clients) > 0 {
		return
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	b.timer = time.AfterFunc(b.grace, func() {
		b.mu.Lock()
		empty := len(b.clients) == 0
		b.mu.Unlock()
		if empty {
			b.shutdown()
		}
	})
}

// shutdown stops everything once: no new sessions, every session sees
// EOF, the server stops.
func (b *broker) shutdown() {
	b.stop.Do(func() {
		b.mu.Lock()
		b.closed = true
		if b.timer != nil {
			b.timer.Stop()
		}
		var conns []*extproc.Conn
		for _, c := range b.clients {
			conns = append(conns, c.conn)
		}
		b.mu.Unlock()
		_ = b.ln.Close() // removes the socket file
		for _, c := range conns {
			c.Close()
		}
		b.killServer()
		close(b.done)
	})
}

func (b *broker) accept() {
	for {
		nc, err := b.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		if b.peer == nil || b.peer(nc) != nil {
			_ = nc.Close()
			continue
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			_ = nc.Close()
			continue
		}
		b.nextID++
		c := &shareClient{id: b.nextID, calls: map[string]context.CancelFunc{}}
		b.clients[c.id] = c
		if b.timer != nil {
			b.timer.Stop()
		}
		c.conn = extproc.NewConnWith(nc, nc, func(conn *extproc.Conn, m extproc.Message) { b.fromClient(c, conn, m) },
			extproc.ConnOptions{Framing: extproc.FramingNewline})
		b.mu.Unlock()
		go b.watch(c)
	}
}

// watch forgets a session when its connection ends.
func (b *broker) watch(c *shareClient) {
	<-c.conn.Done()
	c.conn.Close()
	b.mu.Lock()
	delete(b.clients, c.id)
	for _, cancel := range c.calls {
		cancel()
	}
	b.armLocked()
	b.mu.Unlock()
	b.sendStatus()
}

// sendStatus tells every session how many sessions use the server.
func (b *broker) sendStatus() {
	b.mu.Lock()
	n := len(b.clients)
	b.mu.Unlock()
	for _, c := range b.readyClients() {
		_ = c.conn.Notify(shareNotify, map[string]any{"sessions": n, "pid": b.pid()})
	}
}

// fromClient handles one message of a session. It runs on the session
// connection's reader goroutine: requests to the server run on their own.
func (b *broker) fromClient(c *shareClient, conn *extproc.Conn, m extproc.Message) {
	switch m.Method {
	case "initialize": // the server was initialized once, by the broker
		_ = conn.Reply(m.ID, b.init, nil)
		b.mu.Lock()
		c.ready = true
		b.mu.Unlock()
		b.sendStatus()
		return
	case "ping":
		_ = conn.Reply(m.ID, map[string]any{}, nil)
		return
	case shareStop:
		// No answer: the closed connection is the answer. A reply sent
		// right before shutdown closes the connection could be lost in
		// the write queue, and the session would see an error.
		go b.shutdown()
		return
	case "notifications/cancelled":
		var p struct {
			RequestID json.RawMessage `json:"requestId"`
		}
		_ = json.Unmarshal(m.Params, &p)
		b.mu.Lock()
		cancel := c.calls[string(p.RequestID)]
		b.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		return
	}
	if !m.IsRequest() {
		return // notifications/initialized and friends: the broker's own were sent
	}
	ctx, cancel := context.WithCancel(context.Background())
	key := string(m.ID)
	b.mu.Lock()
	c.calls[key] = cancel
	b.mu.Unlock()
	go func() {
		defer func() {
			b.mu.Lock()
			delete(c.calls, key)
			b.mu.Unlock()
			cancel()
		}()
		raw, err := b.srv.Call(ctx, m.Method, rewriteProgress(m.Params, c.id))
		if ctx.Err() != nil {
			return // cancelled: the session does not wait for it
		}
		if err != nil {
			var rerr *extproc.Error
			if !errors.As(err, &rerr) {
				rerr = &extproc.Error{Code: extproc.CodeInternal, Message: err.Error()}
			}
			_ = conn.Reply(m.ID, nil, rerr)
			return
		}
		_ = conn.Reply(m.ID, raw, nil)
	}()
}

// fromServer handles the server's requests and notifications.
func (b *broker) fromServer(conn *extproc.Conn, m extproc.Message) {
	switch m.Method {
	case "ping":
		_ = conn.Reply(m.ID, map[string]any{}, nil)
	case "roots/list":
		roots := []map[string]any{}
		for _, r := range b.cfg.Roots {
			u := url.URL{Scheme: "file", Path: filepath.ToSlash(r)}
			roots = append(roots, map[string]any{"uri": u.String(), "name": filepath.Base(r)})
		}
		_ = conn.Reply(m.ID, map[string]any{"roots": roots}, nil)
	case "notifications/progress":
		id, params, ok := routeProgress(m.Params)
		if !ok {
			return
		}
		b.mu.Lock()
		c := b.clients[id]
		b.mu.Unlock()
		if c != nil {
			_ = c.conn.Notify(m.Method, params)
		}
	default:
		if m.IsRequest() {
			// sampling/createMessage and elicitation/create are refused,
			// as in a session's own Client.
			_ = conn.Reply(m.ID, nil, &extproc.Error{Code: extproc.CodeMethodNotFound, Message: "termocode does not support " + m.Method})
			return
		}
		for _, c := range b.readyClients() { // tools/list_changed, message …
			_ = c.conn.Notify(m.Method, m.Params)
		}
	}
}

// rewriteProgress makes a call's progress token unique across sessions:
// "c<session>:<the session's token as JSON>".
func rewriteProgress(params json.RawMessage, client uint64) json.RawMessage {
	if len(params) == 0 || !strings.Contains(string(params), "progressToken") {
		return params
	}
	var p map[string]json.RawMessage
	if json.Unmarshal(params, &p) != nil {
		return params
	}
	var meta map[string]json.RawMessage
	if json.Unmarshal(p["_meta"], &meta) != nil || meta["progressToken"] == nil {
		return params
	}
	tok, _ := json.Marshal(fmt.Sprintf("c%d:%s", client, meta["progressToken"]))
	meta["progressToken"] = tok
	p["_meta"], _ = json.Marshal(meta)
	out, err := json.Marshal(p)
	if err != nil {
		return params
	}
	return out
}

// routeProgress undoes rewriteProgress: the session and its params.
func routeProgress(params json.RawMessage) (uint64, json.RawMessage, bool) {
	var p map[string]json.RawMessage
	if json.Unmarshal(params, &p) != nil {
		return 0, nil, false
	}
	var tok string
	if json.Unmarshal(p["progressToken"], &tok) != nil {
		return 0, nil, false
	}
	head, orig, ok := strings.Cut(tok, ":")
	if !ok || !strings.HasPrefix(head, "c") {
		return 0, nil, false
	}
	id, err := strconv.ParseUint(head[1:], 10, 64)
	if err != nil || !json.Valid([]byte(orig)) {
		return 0, nil, false
	}
	p["progressToken"] = json.RawMessage(orig)
	out, err := json.Marshal(p)
	if err != nil {
		return 0, nil, false
	}
	return id, out, true
}
