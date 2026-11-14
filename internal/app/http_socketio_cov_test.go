package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// covSIOServer is a small Socket.IO server. "join" (with an ack id)
// gets a binary event, a big one and a flood of 1100 events before its
// ack; "bye" closes the session before any ack; "hi" is echoed as
// "hello".
func covSIOServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.CloseNow() }()
		ctx := r.Context()
		write := func(s string) { _ = ws.Write(ctx, websocket.MessageText, []byte(s)) }
		write(`0{"sid":"e1","pingInterval":25000,"pingTimeout":20000}`)
		for {
			_, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			switch msg := string(b); {
			case strings.HasPrefix(msg, "40"):
				write(`40{"sid":"s1"}`)
			case strings.HasPrefix(msg, `42`) && strings.Contains(msg, `["join"`):
				id := strings.TrimSuffix(strings.TrimPrefix(msg, "42"), msg[strings.Index(msg, "["):])
				write(`451-["file",{"_placeholder":true,"num":0}]`)
				write(`42["big","` + strings.Repeat("x", httpSIOArgsMax+10) + `"]`)
				for i := 0; i < 1100; i++ {
					write(fmt.Sprintf(`42["tick",%d]`, i))
				}
				write(`43` + id + `["joined"]`)
			case strings.Contains(msg, `["bye"`):
				write("1")
				return
			case strings.Contains(msg, `["hi"`):
				write(`42["hello","sekret-sio-tok"]`)
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A session with acks: the flood beyond the queue is counted as
// dropped, binary events are marked, big arguments cut; a session the
// server closes says so and the waiting ack fails.
func TestCovHTTPSIOSessionEdges(t *testing.T) {
	srv := covSIOServer(t)
	p, err := httpSIOPlanFor(httpSIOResolved(srv.URL, "join\n===\nbye", map[string]string{"ack": "", "listen": "30s"}))
	if err != nil {
		t.Fatal(err)
	}
	text, err := httpSIOSession(context.Background(), p, false, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ack\n    \"joined\"", "← file (binary attachments not shown)", fmt.Sprintf("… (%d bytes)", httpSIOArgsMax+12),
		"ack failed", "connection ended", "more event(s) came too fast and were dropped."} {
		if !strings.Contains(text, want) {
			t.Errorf("transcript lacks %q", want)
		}
	}
	if strings.Contains(text, "30s.") && !strings.Contains(text, "event(s) received in 30s.") {
		t.Error("no count line")
	}
	// A cancelled context ends the listening.
	ctx, cancel := context.WithCancel(context.Background())
	p, _ = httpSIOPlanFor(httpSIOResolved(srv.URL, "", map[string]string{"listen": "60s"}))
	done := make(chan error, 1)
	go func() { _, err := httpSIOSession(ctx, p, false, time.Now); done <- err }()
	time.AfterFunc(200*time.Millisecond, cancel)
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("cancel did not end the session")
	}
	// No server.
	p, _ = httpSIOPlanFor(httpSIOResolved("http://127.0.0.1:1", "", nil))
	if _, err := httpSIOSession(context.Background(), p, false, time.Now); err == nil {
		t.Error("no server: no error")
	}
	if httpSIOArgs(nil) != "" || clipSIO(strings.Repeat("a", 70)) != strings.Repeat("a", 60)+"…" {
		t.Error("args / clip")
	}
	if _, err := httpSIOPlanFor(httpSIOResolved("http://h", "[1]", nil)); err == nil {
		t.Error("a bad emit: no error")
	}
}

// The send key on a SOCKETIO request runs one session at a time and
// opens its transcript with the secret hidden; bad requests say why.
func TestCovHTTPSIOSendFromBuffer(t *testing.T) {
	m, root, _ := covHTTP(t)
	srv := covSIOServer(t)
	if err := os.WriteFile(filepath.Join(root, "http-client.private.env.json"), []byte(`{"dev":{"tok":"sekret-sio-tok"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "http-client.env.json"), []byte(`{"dev":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := httpSetSelectedEnv(httpRootFor(root), "dev"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "s.http")
	src := "###\n# @name chat\n# @listen 300ms\nSOCKETIO " + srv.URL + "\nAuthorization: Bearer {{tok}}\n\nhi\n" + // 1-7
		"\n###\n# @name unknown\nSOCKETIO " + srv.URL + "/{{nope}}\n" + // 9-11
		"\n###\n# @name badlisten\n# @listen soon\nSOCKETIO " + srv.URL + "\n" + // 13-16
		"\n###\n# @name dead\nSOCKETIO http://127.0.0.1:1/x?t={{tok}}\n" // 18-20
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	loop := newCovHTTPLoop(t, m)
	s := m.httpS()
	for line, want := range map[int]string{11: "Socket.IO: unknown variable {{nope}}", 16: "is not a duration"} {
		covHTTPEdit(t, m, file, line)
		covHTTPReset(m)
		covHTTPDispatch(t, m, "http-send")
		if got := covHTTPToast(m); !strings.Contains(got, want) {
			t.Errorf("line %d: %q", line, got)
		}
	}
	covHTTPEdit(t, m, file, 4)
	c := covHTTPDispatch(t, m, "http-send")
	if !s.sioRunning || !strings.Contains(covHTTPToast(m), "Socket.IO: connecting to") {
		t.Fatalf("not running: %q", covHTTPToast(m))
	}
	covHTTPReset(m)
	m.httpSendBufferLine(0, 4)
	if !strings.Contains(covHTTPToast(m), "still running") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	loop.run(c, func() bool { return !s.sioRunning })
	var name, text string
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0)`, &name)
	_ = m.nvim.ExecLuaResult(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`, &text)
	if !strings.HasSuffix(name, "Socket.IO: chat") || !strings.Contains(text, "← hello") || strings.Contains(text, "sekret-sio-tok") {
		t.Errorf("page %q:\n%s", name, text)
	}
	// A session that could not start; a secret in its error is hidden.
	covHTTPEdit(t, m, file, 20)
	loop.run(covHTTPDispatch(t, m, "http-send"), func() bool { return !s.sioRunning })
	if got := covHTTPToast(m); !strings.Contains(got, "Socket.IO: could not connect") || strings.Contains(got, "sekret-sio-tok") {
		t.Errorf("toast %q", got)
	}
	covHTTPReset(m)
	s.sioRunning = true
	m.handleHTTPMsg(httpSIODoneMsg{label: "x", err: errors.New("refused")})
	if s.sioRunning || !strings.Contains(covHTTPToast(m), "Socket.IO: could not connect") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
}

// The documented `# @auth {…}` of a SOCKETIO request is the namespace
// CONNECT payload, not the HTTP auth helper: the send key must not
// refuse it as an unknown variable, and the server gets the payload
// with its variables filled in.
func TestCovHTTPSIOAuthPayloadFromBuffer(t *testing.T) {
	m, root, _ := covHTTP(t)
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.CloseNow() }()
		ctx := r.Context()
		_ = ws.Write(ctx, websocket.MessageText, []byte(`0{"sid":"e1","pingInterval":25000,"pingTimeout":20000}`))
		for {
			_, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if msg := string(b); strings.HasPrefix(msg, "40") {
				select {
				case got <- strings.TrimPrefix(msg, "40"):
				default:
				}
				_ = ws.Write(ctx, websocket.MessageText, []byte(`40{"sid":"s1"}`))
			}
		}
	}))
	t.Cleanup(srv.Close)
	file := filepath.Join(root, "auth.http")
	src := "@chatToken = abc123\n\n###\n# @name chat\n# @auth {\"token\": \"{{chatToken}}\"}\n# @listen 100ms\nSOCKETIO " + srv.URL + "\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	covHTTPEdit(t, m, file, 7)
	c := covHTTPDispatch(t, m, "http-send")
	if !m.httpS().sioRunning {
		t.Fatalf("not sent: %q", covHTTPToast(m))
	}
	newCovHTTPLoop(t, m).run(c, func() bool { return !m.httpS().sioRunning })
	select {
	case payload := <-got:
		if payload != `{"token": "abc123"}` {
			t.Errorf("CONNECT payload %q", payload)
		}
	default:
		t.Fatalf("the server got no CONNECT: %q", covHTTPToast(m))
	}
}
