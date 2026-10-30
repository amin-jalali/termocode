package app

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/debounce"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// fuzzSecret is registered like a key the IDE knows (split for scanners).
var fuzzSecret = "fz9" + "-secret-" + "hub-55"

var fuzzTopics = []string{"status", "toasts", "chats", "chat", "cards", "tree", "bufs", "terms", "git",
	"tasks", "runs", "buf:1", "term:2", "buf:x", "db", "nope", "term:-1"}

// fuzzReader hands out the fuzz input byte by byte (0 when it ran out).
type fuzzReader struct {
	b []byte
	i int
}

func (r *fuzzReader) next() int {
	if r.i >= len(r.b) {
		return 0
	}
	r.i++
	return int(r.b[r.i-1])
}

func (r *fuzzReader) done() bool { return r.i >= len(r.b) }

// fuzzHubOps are handlers for every op that check the role, and answer
// at once, with a secret in the data or the error, later from a
// goroutine, or by panicking.
func fuzzHubOps(wg *sync.WaitGroup, wrongRole *atomic.Value) map[string]webOpFunc {
	ops := map[string]webOpFunc{}
	for i, p := range webproto.Ops() {
		mode := i % 4
		ops[p.Op] = func(m *Model, call *webCall) tea.Cmd {
			if !webproto.Allowed(call.conn.role, call.op) {
				wrongRole.Store(call.op + " ran for " + string(call.conn.role))
			}
			switch mode {
			case 0:
				call.reply(map[string]string{"echo": "x " + fuzzSecret})
			case 1:
				call.fail(webproto.CodeConflict, "no: "+fuzzSecret)
			case 2:
				call.later()
				wg.Add(1)
				go func() {
					defer wg.Done()
					call.reply(nil)
				}()
			default:
				panic("a handler bug")
			}
			return nil
		}
	}
	return ops
}

// fuzzLine builds one browser line from the input; id is the request id
// it used (0: not a request, or a bad one).
func fuzzLine(r *fuzzReader, lastID uint64, golden map[string]string) (line string, id uint64) {
	ops := webproto.Ops()
	topics := func() string {
		n := 1 + r.next()%3
		var ts []string
		for i := 0; i < n; i++ {
			ts = append(ts, strconv.Quote(fuzzTopics[r.next()%len(fuzzTopics)]))
		}
		return "[" + strings.Join(ts, ",") + "]"
	}
	switch r.next() % 8 {
	case 0:
		v := webproto.Version
		if r.next()%4 == 0 {
			v++
		}
		return `{"t":"hello","v":` + strconv.Itoa(v) + `,"token":"x"}`, 0
	case 1:
		return `{"t":"sub","topics":` + topics() + `}`, 0
	case 2:
		return `{"t":"unsub","topics":` + topics() + `}`, 0
	case 3, 7:
		id = lastID + 1
		if r.next()%8 == 0 {
			id = lastID // a reused id ends the connection
		}
		op := "no.such"
		if k := r.next(); k < 250 {
			op = ops[k%len(ops)].Op
		}
		args := golden[op]
		switch r.next() % 6 {
		case 0:
			args = `{"x":1}`
		case 1:
			args = `{"text":"` + fuzzSecret + `"}`
		case 2:
			if args == "" {
				args = "{}"
			}
		}
		if args == "" {
			args = "{}"
		}
		return `{"t":"req","id":` + strconv.FormatUint(id, 10) + `,"op":"` + op + `","args":` + args + `}`, id
	case 4:
		return `{"t":"ping","n":"ab"}`, 0
	case 5:
		n := r.next() % 24
		var b []byte
		for i := 0; i < n && !r.done(); i++ {
			b = append(b, byte(r.next()))
		}
		return string(b), 0
	default:
		return `{"t":"bye"}`, 0
	}
}

// FuzzHubSequence (web design 3.5): random sequences of hello, sub,
// unsub, req, ping, bye and garbage from one browser against the hub,
// with handlers that leak a secret, defer or panic. Invariants: no
// panic; no op runs for a role that may not call it; no frame holds the
// registered secret; every request the hub took gets exactly one answer
// and none are left in flight; subscriptions stay under the cap; the
// connection is forgotten when it ends.
func FuzzHubSequence(f *testing.F) {
	ai.RegisterSecret(fuzzSecret)
	f.Setenv("XDG_CONFIG_HOME", f.TempDir())
	f.Setenv("XDG_STATE_HOME", f.TempDir())
	golden := map[string]string{}
	for _, p := range webproto.Ops() {
		b, err := os.ReadFile(filepath.Join("..", "webproto", "testdata", "golden", "ops", p.Op+".json"))
		if err != nil {
			f.Fatal(err)
		}
		var req struct {
			Args json.RawMessage `json:"args"`
		}
		_ = json.Unmarshal(b, &req)
		golden[p.Op] = string(req.Args)
	}
	root, _ := filepath.EvalSymlinks(f.TempDir())
	_ = os.WriteFile(filepath.Join(root, "a-"+fuzzSecret+".txt"), []byte("x"), 0o644)
	m := &Model{w: 120, h: 40, debounce: debounce.New(), ai: newAIState(), actionsWidth: defaultActionsWidth,
		ws: workspace.Workspace{ID: workspace.LocalID(root), Kind: workspace.KindLocal, Root: root}}
	m.ai.conv = ai.Conversation{ID: "c-1", Title: "t " + fuzzSecret,
		Messages: []ai.StoredMessage{{Role: ai.StoredUser, Text: "key " + fuzzSecret}}}

	f.Add([]byte{0, 1, 0, 3, 0, 5, 3, 3, 1, 3, 4, 1, 1, 2, 3, 2, 0})
	f.Add([]byte{0, 1, 3, 0, 20, 3, 1, 21, 1, 4, 6})
	f.Add([]byte{1, 3, 0, 0, 0})
	f.Add([]byte{0, 0, 5, 10, '{', '"', 't', '"', ':', '1', '}'})
	f.Add([]byte{0, 1, 0, 7, 2, 9, 1, 7, 3, 10, 2, 3, 0, 255, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		r := &fuzzReader{b: data}
		role := webproto.RoleController
		if r.next()%2 == 1 {
			role = webproto.RoleViewer
		}
		h := newWebHub()
		defer h.stop()
		var wg sync.WaitGroup
		var wrongRole atomic.Value
		h.ops = fuzzHubOps(&wg, &wrongRole)
		m.web = h
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		c := &webConn{hub: h, nc: a, id: "c-fz", role: role, device: "phone", out: newWebQueue(), subs: map[string]*webSub{}}
		c.ctx, c.cancel = context.WithCancel(h.ctx)
		defer c.cancel()

		var frames []string
		drain := func() {
			c.out.mu.Lock()
			empty := len(c.out.items) == 0 && len(c.out.owed) == 0
			c.out.mu.Unlock()
			if !empty {
				fs, _ := c.out.take()
				for _, f := range fs {
					frames = append(frames, string(f))
				}
			}
		}
		run := func() {
			for {
				select {
				case msg := <-h.ch:
					m.handleWebMsg(msg)
					m.webAfterUpdate(msg)
				default:
					return
				}
			}
		}
		expect := map[uint64]bool{}
		var lastID uint64
		for step := 0; step < 64 && !r.done(); step++ {
			line, id := fuzzLine(r, lastID, golden)
			if len(line) > webproto.MaxFrame {
				continue
			}
			alive := c.dispatch([]byte(line))
			if id > lastID {
				lastID = id
			}
			run()
			drain()
			if !alive {
				break
			}
			if id != 0 && id == lastID && c.hello {
				expect[id] = true
			}
			if len(c.subs) > webproto.MaxSubscriptions {
				t.Fatalf("%d subscriptions", len(c.subs))
			}
		}
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("deferred answers never came")
		}
		run()
		drain()
		if v := wrongRole.Load(); v != nil {
			t.Fatal(v)
		}
		answers := map[uint64]int{}
		for _, fr := range frames {
			if strings.Contains(fr, fuzzSecret) {
				t.Fatalf("a frame leaks the secret: %s", fr)
			}
			var msg struct {
				T  string `json:"t"`
				ID uint64 `json:"id"`
			}
			if json.Unmarshal([]byte(fr), &msg) == nil && (msg.T == "res" || msg.T == "err") && msg.ID != 0 {
				answers[msg.ID]++
			}
		}
		for id, n := range answers {
			if n != 1 {
				t.Fatalf("request %d got %d answers", id, n)
			}
		}
		for id := range expect {
			if answers[id] != 1 {
				t.Fatalf("request %d got no answer; frames %v", id, frames)
			}
		}
		if n := c.inflight.Load(); n != 0 {
			t.Fatalf("%d requests still in flight", n)
		}
		m.onWebGone(webGoneMsg{hub: h, conn: c})
		if h.live[c] {
			t.Fatal("the hub still serves a connection that ended")
		}
	})
}
