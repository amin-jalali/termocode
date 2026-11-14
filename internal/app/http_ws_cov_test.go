package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// covHTTPLoop is a small Bubble Tea loop: each command runs on its own
// goroutine and every answer goes through handleHTTPMsg. Commands left
// waiting (a WebSocket read) answer into the same loop later.
type covHTTPLoop struct {
	t       *testing.T
	m       *Model
	results chan tea.Msg
}

func newCovHTTPLoop(t *testing.T, m *Model) *covHTTPLoop {
	return &covHTTPLoop{t: t, m: m, results: make(chan tea.Msg, 256)}
}

func (l *covHTTPLoop) start(c tea.Cmd) {
	if c != nil {
		go func() { l.results <- c() }()
	}
}

// run starts cmd and handles answers until done says so.
func (l *covHTTPLoop) run(cmd tea.Cmd, done func() bool) {
	l.t.Helper()
	l.start(cmd)
	deadline := time.After(15 * time.Second)
	for !done() {
		select {
		case msg := <-l.results:
			if b, ok := msg.(tea.BatchMsg); ok {
				for _, c := range b {
					l.start(c)
				}
				continue
			}
			if msg == nil {
				continue
			}
			if next, ok := l.m.handleHTTPMsg(msg); ok {
				l.start(next)
			}
		case <-deadline:
			l.t.Fatal("loop: condition not reached")
		}
	}
}

// covWSServer echoes every message as "echo:<text>" after a "welcome";
// /bye closes the socket with an error code right after the welcome.
func covWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = c.CloseNow() }()
		ctx := r.Context()
		_ = c.Write(ctx, websocket.MessageText, []byte("welcome"))
		if r.URL.Path == "/bye" {
			_ = c.Close(websocket.StatusInternalError, "server going down")
			return
		}
		for {
			typ, b, err := c.Read(ctx)
			if err != nil {
				return
			}
			_ = c.Write(ctx, typ, append([]byte("echo:"), b...))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func covWSLogHas(m *Model, text string) func() bool {
	return func() bool {
		ws := m.httpS().ws
		if ws == nil {
			return false
		}
		for _, l := range ws.log {
			if l.Text == text {
				return true
			}
		}
		return false
	}
}

// A WEBSOCKET request opens a session on the page: saved messages and
// typed ones go out and their echoes come back; Esc disconnects, r
// reconnects, q closes the page.
func TestCovHTTPWSSession(t *testing.T) {
	m, root, _ := covHTTP(t)
	srv := covWSServer(t)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	file := filepath.Join(root, "ws.http")
	long := strings.Repeat("y", 90)
	src := "### chat\n# @name chat\nWEBSOCKET " + wsURL + "/chat\n\nhello\n===\n" + long + "\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	loop := newCovHTTPLoop(t, m)
	f, req := chainDepReq(t, m, file, "chat")
	cmd := m.httpSendRequest(file, f.Vars, req, "")
	s := m.httpS()
	if s.ws == nil || s.ws.state != httpWSConnecting || !s.wsShown {
		t.Fatalf("no session: %+v", s.ws)
	}
	if !strings.Contains(covSvcHTTPPage(t, m), "connecting") {
		t.Errorf("page:\n%s", covSvcHTTPPage(t, m))
	}
	loop.run(cmd, covWSLogHas(m, "welcome"))
	if s.ws.state != httpWSOpen || !strings.Contains(covSvcHTTPPage(t, m), "connected · 0 sent · 1 received") {
		t.Fatalf("state %v page:\n%s", s.ws.state, covSvcHTTPPage(t, m))
	}
	// m lists the saved messages (long ones cut); one is sent.
	m.httpPageKey("m")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPWS {
		t.Fatal("no saved picker")
	}
	if got := covSvcPickerText(m); !strings.Contains(got, "Type a message") || !strings.Contains(got, "hello") || strings.Contains(got, long) {
		t.Errorf("picker:\n%s", got)
	}
	c, _ := m.handleHTTPPickerSelect("0")
	loop.run(c, covWSLogHas(m, "echo:hello"))
	// Type a message: from the picker and from the i key.
	m.handleHTTPPickerSelect("__type")
	if !m.promptOpen || m.promptKind != promptKindHTTPWSType {
		t.Fatal("no type prompt")
	}
	m.promptOpen = false
	m.httpPageKey("i")
	if !m.promptOpen || m.promptKind != promptKindHTTPWSType {
		t.Fatal("i: no type prompt")
	}
	c, _ = m.handleHTTPPromptSubmit("typed")
	loop.run(c, covWSLogHas(m, "echo:typed"))
	if got := covSvcHTTPPage(t, m); !strings.Contains(got, "→ typed") || !strings.Contains(got, "← echo:typed") {
		t.Errorf("page:\n%s", got)
	}
	// Bad picks, empty text and other keys do nothing.
	for _, id := range []string{"x", "-1", "9"} {
		if m.httpWSPick(id) != nil {
			t.Errorf("pick %q sent", id)
		}
	}
	if m.httpWSSend("") != nil {
		t.Error("empty text sent")
	}
	if c, ok := m.httpWSKey("tab"); !ok || c != nil {
		t.Error("tab is not swallowed")
	}
	// Esc disconnects; the page says closed and offers r.
	c, _ = m.httpWSKey("esc")
	loop.run(c, func() bool { return s.ws.state == httpWSClosed })
	page := covSvcHTTPPage(t, m)
	if !strings.Contains(page, "closed · 2 sent · 3 received") {
		t.Errorf("page:\n%s", page)
	}
	covHTTPReset(m)
	m.httpWSKey("i")
	if !strings.Contains(covHTTPToast(m), "Not connected") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	covHTTPReset(m)
	m.httpWSKey("m")
	if m.pickerOpen || !strings.Contains(covHTTPToast(m), "No WebSocket session") {
		t.Errorf("m when closed: %q", covHTTPToast(m))
	}
	if c := m.httpWSSend("x"); c == nil || !strings.Contains(covHTTPToast(m), "No WebSocket session") {
		t.Error("send when closed")
	}
	// r reconnects (a new session, a new generation).
	old := s.ws.gen
	c, _ = m.httpWSKey("r")
	loop.run(c, func() bool { return s.ws.gen != old && s.ws.state == httpWSOpen })
	if c, ok := m.httpWSKey("r"); !ok || c != nil {
		t.Error("r while open did something")
	}
	// The palette disconnects too; a second time there is none.
	covHTTPReset(m)
	c = covHTTPDispatch(t, m, "http-ws-close")
	if s.ws.state != httpWSClosed || !strings.Contains(covSvcToast(m), "WebSocket disconnected") {
		t.Errorf("close: %v %q", s.ws.state, covSvcToast(m))
	}
	loop.run(c, func() bool { return true })
	covHTTPReset(m)
	covHTTPDispatch(t, m, "http-ws-close")
	if !strings.Contains(covHTTPToast(m), "No WebSocket session") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// Esc when closed closes the page; q closes it and hands the keys
	// back to the HTTP page.
	m.httpWSKey("esc")
	if covSvcHTTPPage(t, m) != "" {
		t.Error("page still open")
	}
	s.wsShown = true
	m.httpPageKey("q")
	if s.wsShown {
		t.Error("q kept the session page")
	}
	if _, ok := m.httpWSKey("m"); ok {
		t.Error("keys taken without the session page")
	}
}

// A server that closes with an error, a dial that fails and stale
// events: the page says why; old generations are dropped.
func TestCovHTTPWSErrors(t *testing.T) {
	m, _, _ := covHTTP(t)
	srv := covWSServer(t)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	s := m.httpS()
	loop := newCovHTTPLoop(t, m)
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "WEBSOCKET", URL: wsURL + "/bye"}}, label: "bye"}
	c := m.startHTTPRun(run, httpclient.Hooks{})
	loop.run(c, func() bool { return s.ws.state == httpWSClosed })
	if s.ws.err == "" || !strings.Contains(covSvcHTTPPage(t, m), "closed · ") {
		t.Errorf("err %q page:\n%s", s.ws.err, covSvcHTTPPage(t, m))
	}
	// A port nobody listens on.
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	run = &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: "ws://" + addr + "/x"}}, label: "x"}
	c = m.startHTTPRun(run, httpclient.Hooks{})
	loop.run(c, func() bool { return s.ws.state == httpWSClosed })
	if s.ws.err == "" || s.ws.conn != nil {
		t.Errorf("dial error: %+v", s.ws)
	}
	// Stale generations and no session.
	if m.httpWSHandle(httpWSEventMsg{gen: s.ws.gen + 5}) != nil {
		t.Error("stale event handled")
	}
	if m.httpWSSent(httpWSSentMsg{gen: s.ws.gen + 5}) != nil {
		t.Error("stale sent handled")
	}
	covHTTPReset(m)
	m.httpWSSent(httpWSSentMsg{gen: s.ws.gen, err: context.DeadlineExceeded})
	if !strings.Contains(covHTTPToast(m), "WebSocket send failed") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	s.ws = nil
	if m.httpWSPick("0") != nil || m.httpWSHandle(httpWSEventMsg{}) != nil || m.httpWSSent(httpWSSentMsg{}) != nil {
		t.Error("no session did something")
	}
	m.httpCloseWS()
	nv := m.nvim
	m.nvim = nil
	s.ws = &httpWSState{run: run}
	if m.httpShowWS() != nil {
		t.Error("shown without an editor")
	}
	m.nvim = nv
	// waitHTTPWS on a closed channel ends quietly.
	ch := make(chan httpWSEvent)
	close(ch)
	if msg := waitHTTPWS(1, ch)(); msg != nil {
		t.Errorf("closed channel: %#v", msg)
	}
}

// The page text: connecting, connected with and without saved
// messages, long messages cut, and only the newest messages kept.
func TestCovHTTPWSPageStates(t *testing.T) {
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "WEBSOCKET", URL: "ws://x/"}}, label: "x"}
	ws := &httpWSState{run: run, state: httpWSConnecting}
	if got := strings.Join(buildHTTPWSPage(ws).lines, "\n"); !strings.Contains(got, "connecting") {
		t.Errorf("connecting:\n%s", got)
	}
	ws.state = httpWSOpen
	if got := strings.Join(buildHTTPWSPage(ws).lines, "\n"); !strings.Contains(got, "i types a message. Saved messages go in the body") {
		t.Errorf("open, none saved:\n%s", got)
	}
	ws.saved = []string{"a", "b"}
	if got := strings.Join(buildHTTPWSPage(ws).lines, "\n"); !strings.Contains(got, "one of the 2 saved messages") {
		t.Errorf("open, saved:\n%s", got)
	}
	ws.log = []httpclient.WSMessage{{Dir: httpclient.WSIn, Text: strings.Repeat("z", httpWSClip+10)}}
	if got := strings.Join(buildHTTPWSPage(ws).lines, "\n"); !strings.Contains(got, strings.Repeat("z", httpWSClip)+"…") || strings.Contains(got, strings.Repeat("z", httpWSClip+1)) {
		t.Error("long message not cut")
	}
	log := make([]httpclient.WSMessage, httpWSShow+5)
	log[5].Text = "first kept"
	if got := httpWSTail(log); len(got) != httpWSShow || got[0].Text != "first kept" {
		t.Errorf("tail %d %q", len(got), got[0].Text)
	}
}
