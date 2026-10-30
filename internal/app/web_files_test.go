package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// A rig for the file and terminal views: a Model on a temp workspace
// (the folder ws-root, so names in goldens are stable), a hub and one
// socketless browser connection. Ops run as Update would run them; their
// commands run here, in order.

type webFilesRig struct {
	t      *testing.T
	m      *Model
	h      *webHub
	c      *webConn
	root   string
	id     uint64
	kept   map[*webConn][]map[string]any // topic frames read past while looking for an answer
	events chan nvim.NotifyMsg
}

func newWebFilesRig(t *testing.T) *webFilesRig {
	t.Helper()
	mm := aiTestModel(t)
	m := &mm
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(base, "ws-root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	m.ws = workspace.Workspace{ID: workspace.LocalID(root), Kind: workspace.KindLocal, Root: root}
	h := newWebHub()
	t.Cleanup(h.stop)
	m.web = h
	r := &webFilesRig{t: t, m: m, h: h, root: root, kept: map[*webConn][]map[string]any{}}
	r.c = r.conn("c-1", webproto.RoleController)
	return r
}

// conn adds a live socketless browser connection.
func (r *webFilesRig) conn(id string, role webproto.Role) *webConn {
	c := &webConn{hub: r.h, id: id, role: role, out: newWebQueue(), subs: map[string]*webSub{}}
	c.ctx, c.cancel = context.WithCancel(r.h.ctx)
	r.h.live[c] = true
	return c
}

// write creates rel (and its folders) under the root with a fixed mtime.
func (r *webFilesRig) write(rel, body string) string {
	r.t.Helper()
	abs := filepath.Join(r.root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
	r.fixTimes()
	return abs
}

// fixTimes sets every mtime under the root to one instant (goldens).
func (r *webFilesRig) fixTimes() {
	at := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	_ = filepath.Walk(r.root, func(p string, _ os.FileInfo, err error) error {
		if err == nil {
			_ = os.Chtimes(p, at, at)
		}
		return nil
	})
}

// callOn runs op with args for connection c and returns its answer frame.
func (r *webFilesRig) callOn(c *webConn, op string, args any) map[string]any {
	r.t.Helper()
	r.id++
	c.inflight.Add(1)
	call := &webCall{ctx: c.ctx, conn: c, id: r.id, op: op, args: args}
	cmd, found := r.m.webRunCall(r.h, call)
	if !found {
		r.t.Fatalf("no handler for %s", op)
	}
	webDrain(r.t, r.m, cmd)
	var answer map[string]any
	for _, f := range r.takeOn(c) {
		if id, ok := f["id"].(float64); ok && uint64(id) == r.id && answer == nil {
			answer = f
			continue
		}
		r.keep(c, f)
	}
	if answer == nil {
		r.t.Fatalf("%s gave no answer", op)
	}
	return answer
}

func (r *webFilesRig) call(op string, args any) map[string]any {
	r.t.Helper()
	return r.callOn(r.c, op, args)
}

func (r *webFilesRig) keep(c *webConn, f map[string]any) {
	r.kept[c] = append(r.kept[c], f)
}

// takeOn returns the frames queued on c (kept ones first).
func (r *webFilesRig) takeOn(c *webConn) []map[string]any {
	r.t.Helper()
	out := r.kept[c]
	delete(r.kept, c)
	c.out.mu.Lock()
	empty := len(c.out.items) == 0 && len(c.out.owed) == 0
	c.out.mu.Unlock()
	if empty { // take would wait
		return out
	}
	return append(out, webTestTake(r.t, c)...)
}

func (r *webFilesRig) take() []map[string]any { return r.takeOn(r.c) }

// sub subscribes c to topic and returns the snap's data.
func (r *webFilesRig) subOn(c *webConn, topic string) map[string]any {
	r.t.Helper()
	r.m.onWebSub(webSubMsg{hub: r.h, conn: c, topics: []string{topic}})
	var snap map[string]any
	for _, f := range r.takeOn(c) {
		switch {
		case f["t"] == "snap" && f["topic"] == topic && snap == nil:
			snap, _ = f["data"].(map[string]any)
		case f["t"] == "err":
			r.t.Fatalf("sub %s: %v", topic, f)
		default:
			r.keep(c, f)
		}
	}
	if snap == nil {
		r.t.Fatalf("sub %s: no snap", topic)
	}
	return snap
}

func (r *webFilesRig) sub(topic string) map[string]any {
	r.t.Helper()
	return r.subOn(r.c, topic)
}

// deltas returns the data of the queued deltas of topic on c.
func (r *webFilesRig) deltasOn(c *webConn, topic string) []map[string]any {
	r.t.Helper()
	var out []map[string]any
	for _, f := range r.takeOn(c) {
		if f["t"] == "delta" && f["topic"] == topic {
			d, _ := f["data"].(map[string]any)
			out = append(out, d)
		} else {
			r.keep(c, f)
		}
	}
	return out
}

func (r *webFilesRig) deltas(topic string) []map[string]any {
	r.t.Helper()
	return r.deltasOn(r.c, topic)
}

// track runs the after-Update hook and its commands.
func (r *webFilesRig) track() {
	r.t.Helper()
	webDrain(r.t, r.m, r.m.webTrackFiles())
}

// webDrain runs cmd and what it leads to, as the program would: batches
// fan out, applyMsg runs on the Model. Other messages are dropped.
func webDrain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(20 * time.Second):
			t.Fatal("a command did not return")
		}
		switch v := msg.(type) {
		case tea.BatchMsg:
			queue = append(queue, v...)
		case applyMsg:
			queue = append(queue, v(m))
		}
	}
}

// webErrCode is the code of an err frame ("" for a res).
func webErrCode(f map[string]any) string {
	if f["t"] != "err" {
		return ""
	}
	s, _ := f["code"].(string)
	return s
}

// webResData decodes a res frame's data into v.
func webResData(t *testing.T, f map[string]any, v any) {
	t.Helper()
	if f["t"] != "res" {
		t.Fatalf("want res, got %v", f)
	}
	b, _ := json.Marshal(f["data"])
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

// withNvim starts a real nvim for the rig (closed at the end of the test)
// and forwards its notifications to pump.
func (r *webFilesRig) withNvim() *nvim.Client {
	r.t.Helper()
	c := aiTestNvim(r.t, r.m)
	if err := c.ExecLua(fileReloadLua); err != nil { // as the app does at startup
		r.t.Fatal(err)
	}
	r.m.subscribeNotify()
	ch := make(chan nvim.NotifyMsg, 4096)
	go func() {
		defer close(ch)
		for {
			msg := c.Next()()
			switch v := msg.(type) {
			case nil, nvim.ExitedMsg:
				return
			case nvim.NotifyMsg:
				select {
				case ch <- v:
				default:
				}
			}
		}
	}()
	r.events = ch
	return c
}

// pumpUntil hands nvim notifications to the Model (as Update does) until
// cond holds.
func (r *webFilesRig) pumpUntil(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.After(20 * time.Second)
	for !cond() {
		select {
		case msg, ok := <-r.events:
			if !ok {
				r.t.Fatalf("nvim ended while waiting for %s", what)
			}
			r.m.dispatchNotify(msg)
		case <-deadline:
			r.t.Fatalf("timed out waiting for %s", what)
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// lua runs a test chunk in nvim and decodes its result.
func (r *webFilesRig) lua(code string, out any, args ...any) {
	r.t.Helper()
	if out == nil {
		var discard any
		out = &discard
	}
	if err := r.m.nvim.ExecLuaResult(code, out, args...); err != nil {
		r.t.Fatalf("lua: %v", err)
	}
}
