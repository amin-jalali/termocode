//go:build unix

package app

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webLeakRig is a real hub and a real gateway with a browser on the
// WebSocket, a Model with nvim, a git workspace and a chat; run plays
// the program's loop (web messages, commands, applyMsg, nvim
// notifications, the after-update publishers) until a condition holds.
type webLeakRig struct {
	*webPanelRig
	ws     *websocket.Conn
	msgs   chan tea.Msg
	events chan nvim.NotifyMsg

	mu     sync.Mutex
	frames []string
	id     int
}

func newWebLeakRig(t *testing.T) *webLeakRig {
	r := &webLeakRig{webPanelRig: newWebPanelRig(t), msgs: make(chan tea.Msg, 256)}
	r.ws = r.browser(r.started())
	go func() {
		for {
			_, b, err := r.ws.Read(context.Background())
			if err != nil {
				return
			}
			r.mu.Lock()
			r.frames = append(r.frames, string(b))
			r.mu.Unlock()
		}
	}()
	r.cmd(waitWeb(r.hub))
	return r
}

// withNvim starts nvim and forwards its notifications.
func (r *webLeakRig) withNvim() {
	c := aiTestNvim(r.t, r.m)
	if err := c.ExecLua(fileReloadLua); err != nil {
		r.t.Fatal(err)
	}
	r.m.subscribeNotify()
	r.events = make(chan nvim.NotifyMsg, 4096)
	go func() {
		for {
			switch v := c.Next()().(type) {
			case nil, nvim.ExitedMsg:
				return
			case nvim.NotifyMsg:
				select {
				case r.events <- v:
				default:
				}
			}
		}
	}()
}

// cmd runs a command in the background; its message joins the loop.
func (r *webLeakRig) cmd(c tea.Cmd) {
	if c == nil {
		return
	}
	go func() {
		if msg := c(); msg != nil {
			select {
			case r.msgs <- msg:
			case <-time.After(30 * time.Second):
			}
		}
	}()
}

// run plays the loop until cond holds.
func (r *webLeakRig) run(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.After(30 * time.Second)
	for !cond() {
		var msg tea.Msg
		select {
		case msg = <-r.msgs:
		case ev := <-r.events:
			r.m.dispatchNotify(ev)
		case <-deadline:
			r.t.Fatalf("timed out waiting for %s", what)
		case <-time.After(10 * time.Millisecond):
		}
		switch v := msg.(type) {
		case nil:
		case tea.BatchMsg:
			for _, c := range v {
				r.cmd(c)
			}
		case applyMsg:
			r.cmd(v(r.m))
		default:
			if c, ok := r.m.handleWebMsg(msg); ok {
				r.cmd(c)
			}
		}
		r.cmd(r.m.webAfterUpdate(msg))
	}
}

// send writes one message from the browser.
func (r *webLeakRig) send(s string) {
	r.t.Helper()
	if err := r.ws.Write(context.Background(), websocket.MessageText, []byte(s)); err != nil {
		r.t.Fatal(err)
	}
}

// req sends a request and waits for its answer frame.
func (r *webLeakRig) req(op, args string) map[string]any {
	r.t.Helper()
	r.id++
	id := r.id
	r.send(`{"t":"req","id":` + strconv.Itoa(id) + `,"op":"` + op + `","args":` + args + `}`)
	var ans map[string]any
	r.run(op+"'s answer", func() bool {
		for _, f := range r.all() {
			var m map[string]any
			if json.Unmarshal([]byte(f), &m) == nil && m["id"] == float64(id) && (m["t"] == "res" || m["t"] == "err") {
				ans = m
				return true
			}
		}
		return false
	})
	return ans
}

func (r *webLeakRig) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.frames...)
}

// saw reports whether some frame holds every one of parts.
func (r *webLeakRig) saw(parts ...string) bool {
	for _, f := range r.all() {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(f, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// Spec (T43, web design 3.1): with secrets registered (a key, a database
// password, an env value), every view the browser can reach (chat, tree,
// a file, git, search, a terminal, tasks, status) sends them redacted:
// no frame on the WebSocket, through the real hub and gateway, holds a
// secret, and neither does the gateway's audit log.
func TestNoSecretOverWS(t *testing.T) {
	key := "lk7" + "-secret-" + "token-77"
	dbPass := "pg" + "-pass-" + "wordQ9"
	envVal := "env" + "-secret-" + "value-31"
	for _, s := range []string{key, dbPass, envVal} {
		ai.RegisterSecret(s)
	}
	t.Setenv("WEBLEAK_PW", envVal)
	r := newWebLeakRig(t)
	m := r.m
	dir, _ := webTestRepo(t, m)
	webTestWrite(t, dir, "a.txt", "one\nkey="+key+"\n")
	webTestWrite(t, dir, "db.txt", "DATABASE_URL=postgres://app:"+dbPass+"@db/app\n")
	webTestWrite(t, dir, "note-"+key+".md", "x\n")
	webTestWrite(t, dir, ".termocode/tasks.json", `{"tasks":[{"label":"push `+key+`","command":"echo `+key+`"}]}`)
	webTestGitLoad(m)
	m.ai.conv = ai.Conversation{ID: "c-1", Title: "Use " + key, Messages: []ai.StoredMessage{
		{Role: ai.StoredUser, Text: "my key is " + key},
		{Role: ai.StoredAssistant, Text: "The URL is postgres://app:" + dbPass + "@db/app"},
		{Role: ai.StoredTool, Text: "⚙ env → WEBLEAK_PW=" + envVal},
	}}
	r.withNvim()
	var term int
	if err := m.nvim.ExecLuaResult(`local dir = ...
local b = vim.api.nvim_create_buf(false, true)
vim.api.nvim_buf_call(b, function()
  vim.fn.jobstart({ 'sh' }, { term = true, cwd = dir, env = { PS1 = '$ ', ENV = '' } })
end)
return b`, &term, dir); err != nil {
		t.Fatal(err)
	}
	m.terminalTabs = append(m.terminalTabs, terminalTab{BufID: term, Name: "sh " + key, Cwd: dir, LastExit: -1})
	m.termOpen = true

	var topics []string
	for _, d := range webproto.Topics() {
		if d.Phase == webproto.PhaseV1 && !d.Keyed {
			topics = append(topics, `"`+d.Name+`"`)
		}
	}
	r.send(`{"t":"sub","topics":[` + strings.Join(topics, ",") + `,"term:` + strconv.Itoa(term) + `"]}`)
	r.run("the chat snapshot", func() bool { return r.saw(`"topic":"chat"`, "my key is") })

	if a := r.req("tree.list", `{"path":""}`); a["t"] != "res" {
		t.Fatalf("tree.list %v", a)
	}
	open := r.req("buf.open", `{"path":"db.txt"}`)
	var ref webproto.BufOpenRes
	webResData(t, open, &ref)
	r.send(`{"t":"sub","topics":["buf:` + strconv.Itoa(ref.Buf) + `"]}`)
	r.run("the file", func() bool { return r.saw("DATABASE_URL=postgres://app:") })
	if a := r.req("git.diff", `{"path":"a.txt"}`); a["t"] != "res" {
		t.Fatalf("git.diff %v", a)
	}
	if a := r.req("search.run", `{"query":"`+key+`"}`); a["t"] != "res" {
		t.Fatalf("search.run %v", a)
	}
	r.run("search hits", func() bool { return r.saw(`"t":"ev"`, "key=") })
	if a := r.req("term.send", `{"buf":`+strconv.Itoa(term)+`,"text":"echo pw=$WEBLEAK_PW k=`+key+`\r"}`); a["t"] != "res" {
		t.Fatalf("term.send %v", a)
	}
	r.run("the terminal output", func() bool { return r.saw("pw=[REDACTED]") })

	for _, want := range []string{"my key is [REDACTED]", "app:[REDACTED]@db", "key=[REDACTED]", "WEBLEAK_PW=[REDACTED]"} {
		if !r.saw(want) {
			t.Errorf("no frame shows %q: a view was not reached", want)
		}
	}
	for i, f := range r.all() {
		for _, s := range []string{key, dbPass, envVal} {
			if strings.Contains(f, s) {
				t.Fatalf("frame %d leaks a secret: %s", i, f)
			}
		}
	}
	b, _ := os.ReadFile(r.audit)
	for _, s := range []string{key, dbPass, envVal} {
		if strings.Contains(string(b), s) {
			t.Fatalf("the audit log leaks a secret:\n%s", b)
		}
	}
}
