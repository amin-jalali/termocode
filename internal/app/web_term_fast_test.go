//go:build unix

package app

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The web terminal's speed through our whole path, with fakes: a browser
// (a Go WebSocket client) → the real gateway (webgw.Run) → the real hub
// socket → Update (the hub's and nvim's messages, then the after-Update
// publishers, one at a time as the program runs them) → nvim's terminal
// buffer running a plain sh → the lines delta back the same way.

// webTermBudget is what a key may take from the browser's frame out to
// its echo's delta in (p95). nvim refreshes a terminal every 10 ms; the
// rest is ours.
const webTermBudget = 30 * time.Millisecond

// webTermFastRig is a panel rig with nvim, a shell and a browser that
// follows the shell's term topic.
type webTermFastRig struct {
	t     *testing.T
	ws    *websocket.Conn
	buf   int
	topic string
	state json.RawMessage // the term topic's state, replayed from snap and deltas
	id    uint64
}

func newWebTermFastRig(t *testing.T) *webTermFastRig {
	t.Helper()
	r := newWebPanelRig(t)
	link := r.started()
	ws := r.browser(link)
	c := aiTestNvim(t, r.m)
	if err := c.ExecLua(fileReloadLua); err != nil {
		t.Fatal(err)
	}
	r.m.subscribeNotify()
	root := t.TempDir()
	var buf int
	if err := c.ExecLuaResult(`local argv, dir = ...
local b = vim.api.nvim_create_buf(false, true)
vim.api.nvim_buf_call(b, function()
  vim.fn.jobstart(argv, { term = true, cwd = dir, env = { PS1 = '$ ', ENV = '' } })
end)
return b`, &buf, []string{"sh"}, root); err != nil {
		t.Fatal(err)
	}
	r.m.terminalTabs = append(r.m.terminalTabs, terminalTab{BufID: buf, Name: "sh", Cwd: root, LastExit: -1})
	r.m.termOpen = true

	// Update from here on, in its own goroutine: the hub's messages and
	// nvim's notifications, each followed by the after-Update publishers.
	events := make(chan nvim.NotifyMsg, 4096)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		for {
			var n nvim.NotifyMsg
			switch msg := c.Next()().(type) {
			case nil, nvim.ExitedMsg:
				return
			case nvim.NotifyMsg:
				n = msg
			default:
				continue
			}
			select {
			case events <- n:
			case <-stop:
				return
			}
		}
	}()
	go func() {
		defer close(done)
		for {
			var msg any
			select {
			case m := <-r.hub.ch:
				r.m.handleWebMsg(m)
				msg = m
			case n := <-events:
				r.m.dispatchNotify(n)
				msg = n
			case <-stop:
				return
			}
			r.m.webAfterUpdate(msg)
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})

	f := &webTermFastRig{t: t, ws: ws, buf: buf, topic: "term:" + strconv.Itoa(buf)}
	f.write(map[string]any{"t": "sub", "topics": []string{f.topic}})
	for f.state == nil {
		m := f.read(10 * time.Second)
		if m.T == "snap" && m.Topic == f.topic {
			f.state = m.Data
		}
	}
	f.waitText("$", 10*time.Second)
	return f
}

type webFastFrame struct {
	T     string          `json:"t"`
	Topic string          `json:"topic"`
	ID    uint64          `json:"id"`
	Data  json.RawMessage `json:"data"`
}

func (f *webTermFastRig) write(v any) {
	f.t.Helper()
	b, _ := json.Marshal(v)
	if err := f.ws.Write(context.Background(), websocket.MessageText, b); err != nil {
		f.t.Fatal(err)
	}
}

func (f *webTermFastRig) read(d time.Duration) webFastFrame {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	_, b, err := f.ws.Read(ctx)
	if err != nil {
		f.t.Fatalf("read: %v (rows %q)", err, f.text())
	}
	var m webFastFrame
	if err := json.Unmarshal(b, &m); err != nil {
		f.t.Fatal(err)
	}
	if m.T == "err" || m.T == "bye" {
		f.t.Fatalf("hub said %s", b)
	}
	return m
}

// send types text the way the page does (one term.send).
func (f *webTermFastRig) send(text string) {
	f.t.Helper()
	f.id++
	f.write(map[string]any{"t": "req", "id": f.id, "op": webproto.OpTermSend, "args": map[string]any{"buf": f.buf, "text": text}})
}

// text is the shell's rows as the browser has them, wrapped rows joined.
func (f *webTermFastRig) text() string {
	var s webproto.TermSnap
	_ = json.Unmarshal(f.state, &s)
	return strings.Join(s.Rows, "")
}

// waitText reads frames until the rows hold want; it returns how many
// term deltas that took.
func (f *webTermFastRig) waitText(want string, d time.Duration) int {
	f.t.Helper()
	n := 0
	deadline := time.Now().Add(d)
	for !strings.Contains(f.text(), want) {
		m := f.read(time.Until(deadline))
		if m.T != "delta" || m.Topic != f.topic {
			continue
		}
		next, err := webTermApply(f.state, m.Data)
		if err != nil {
			f.t.Fatal(err)
		}
		f.state, n = next, n+1
	}
	return n
}

func webP95(d []time.Duration) (p50, p95 time.Duration) {
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2], s[len(s)*95/100]
}

// Spec (web terminal speed): a key typed in the web terminal comes back
// as its echo's delta within webTermBudget (p95), every key on its own:
// nothing in the gateway, the hub or Update holds a lone key or its echo
// back (no fixed sleep, poll or frame-sized wait).
func TestWebTermKeyEchoWithinBudget(t *testing.T) {
	f := newWebTermFastRig(t)
	typed := ""
	var took []time.Duration
	for i := 0; i < 40; i++ {
		ch := string(rune('a' + i%26))
		typed += ch
		start := time.Now()
		f.send(ch)
		f.waitText("$ "+typed, 5*time.Second)
		took = append(took, time.Since(start))
		time.Sleep(time.Duration(i%5) * time.Millisecond) // keys come at a typist's pace, not in step with nvim
	}
	p50, p95 := webP95(took)
	t.Logf("key -> echo delta through gateway, hub, Update and nvim: p50 %v, p95 %v (budget %v)", p50.Round(100*time.Microsecond), p95.Round(100*time.Microsecond), webTermBudget)
	if p95 > webTermBudget {
		t.Errorf("a key's echo took %v (p95), budget %v", p95, webTermBudget)
	}
}

// Spec (web terminal speed): a command's output burst reaches the browser
// as it comes, with no frame-sized wait in between: all 300 rows it
// prints within 4 x webTermBudget of the Enter (nvim refreshes a
// terminal every 10 ms; the hub queue merges what the browser has not
// taken yet).
func TestWebTermOutputBurstComesAtOnce(t *testing.T) {
	f := newWebTermFastRig(t)
	cmd := "i=0; while [ $i -lt 300 ]; do i=$((i+1)); echo row-$i; done"
	start := time.Now()
	f.send(cmd + "\r")
	n := f.waitText("done", 5*time.Second)
	echo := time.Since(start)
	n += f.waitText("row-300", 10*time.Second)
	all := time.Since(start)
	t.Logf("output burst: the command's echo after %v, all 300 rows after %v, in %d term deltas", echo.Round(100*time.Microsecond), all.Round(100*time.Microsecond), n)
	if all > 4*webTermBudget {
		t.Errorf("300 rows took %v to reach the browser, want under %v", all, 4*webTermBudget)
	}
	if got := strings.Count(f.text(), "row-"); got < 300 {
		t.Fatalf("the browser has %d rows of the 300", got)
	}
}
