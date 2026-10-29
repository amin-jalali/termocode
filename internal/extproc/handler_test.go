package extproc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ext"
)

// handlerPair wires the host-side handler of one fake run to an
// extension-side Conn over pipes, without a process.
func handlerPair(t *testing.T, root string) (*Host, *Conn) {
	h, c, _ := handlerPair3(t, root)
	return h, c
}

func handlerPair3(t *testing.T, root string) (*Host, *Conn, *Conn) {
	t.Helper()
	h := NewHost(Config{Root: root})
	p := &proc{h: h, x: ext.Extension{Name: "t"}}
	stop := make(chan struct{})
	extR, hostW := io.Pipe()
	hostR, extW := io.Pipe()
	host := NewConn(hostR, hostW, p.handler(7, stop))
	extSide := NewConn(extR, extW, nil)
	t.Cleanup(func() { extSide.Close(); host.Close(); close(stop) })
	return h, extSide, host
}

func call(t *testing.T, c *Conn, method string, params any) (json.RawMessage, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return c.Call(ctx, method, params)
}

func nextEvent(t *testing.T, h *Host) Event {
	t.Helper()
	select {
	case ev := <-h.Events():
		return ev
	case <-time.After(10 * time.Second):
		t.Fatal("no event")
	}
	return Event{}
}

func noEvent(t *testing.T, h *Host) {
	t.Helper()
	select {
	case ev := <-h.Events():
		t.Fatalf("unexpected event %+v", ev)
	default:
	}
}

func logHas(h *Host, s string) bool {
	for _, l := range h.Log() {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

func waitLog(t *testing.T, h *Host, s string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !logHas(h, s) {
		if time.Now().After(deadline) {
			t.Fatalf("log never had %q: %q", s, h.Log())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestHandlerLogAndUnknownMethods(t *testing.T) {
	h, c := handlerPair(t, t.TempDir())
	if _, err := call(t, c, "log", map[string]string{"message": "hello log"}); err != nil {
		t.Fatal(err)
	}
	if !logHas(h, "t: hello log") {
		t.Fatalf("log %q", h.Log())
	}
	_ = c.Notify("log", map[string]string{"message": "quiet"})
	waitLog(t, h, "t: quiet")

	_, err := call(t, c, "bogus/method", nil)
	var rerr *Error
	if !errors.As(err, &rerr) || rerr.Code != CodeMethodNotFound {
		t.Fatalf("err = %v", err)
	}
	_ = c.Notify("other/bogus", nil)
	waitLog(t, h, `unknown method "other/bogus"`)
	noEvent(t, h)
}

func TestHandlerReadFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(root, "big.bin")
	if err := os.WriteFile(big, make([]byte, MaxReadFile+1), 0o644); err != nil {
		t.Fatal(err)
	}
	h, c := handlerPair(t, root)
	res, err := call(t, c, "readFile", map[string]string{"path": "a.txt"})
	if err != nil || string(res) != `{"text":"alpha"}` {
		t.Fatalf("res %s err %v", res, err)
	}
	for _, bad := range []struct {
		params any
		want   string
	}{
		{nil, "needs a path"},
		{map[string]string{"path": ""}, "needs a path"},
		{map[string]string{"path": "../outside.txt"}, "outside the workspace"},
		{map[string]string{"path": "missing.txt"}, "no such file"},
		{map[string]string{"path": "big.bin"}, "bigger than"},
		{map[string]string{"path": "."}, "is a directory"},
	} {
		_, err := call(t, c, "readFile", bad.params)
		if err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("readFile %v: err = %v, want %q", bad.params, err, bad.want)
		}
	}
	// A readFile notification has nobody to answer to: ignored.
	_ = c.Notify("readFile", map[string]string{"path": "a.txt"})
	if _, err := call(t, c, "log", map[string]string{"message": "sync"}); err != nil {
		t.Fatal(err)
	}
	noEvent(t, h)
}

func TestHandlerOpenFileStaysInWorkspace(t *testing.T) {
	root := t.TempDir()
	h, c := handlerPair(t, root)
	if _, err := call(t, c, "openFile", map[string]string{"path": "../../etc/passwd"}); err == nil ||
		!strings.Contains(err.Error(), "outside the workspace") {
		t.Fatalf("err = %v", err)
	}
	_ = c.Notify("openFile", map[string]string{"path": "/etc/passwd"})
	waitLog(t, h, `openFile "/etc/passwd" refused`)
	noEvent(t, h)

	if _, err := call(t, c, "openFile", map[string]any{"path": "sub/x.go", "line": 3}); err != nil {
		t.Fatal(err)
	}
	ev := nextEvent(t, h)
	var a map[string]any
	_ = json.Unmarshal(ev.Msg.Params, &a)
	if ev.Kind != EvMessage || ev.Gen != 7 || ev.Ext != "t" || a["path"] != filepath.Join(root, "sub", "x.go") || a["line"] != float64(3) {
		t.Fatalf("event %+v params %v", ev, a)
	}
}

func TestHandlerUIRequestsAnsweredOrPassedOn(t *testing.T) {
	h, c, host := handlerPair3(t, t.TempDir())
	// notify is answered at once and passed to the UI.
	if res, err := call(t, c, "notify", map[string]string{"message": "hi"}); err != nil || string(res) != "null" {
		t.Fatalf("res %s err %v", res, err)
	}
	if ev := nextEvent(t, h); ev.Msg.Method != "notify" {
		t.Fatalf("event %+v", ev)
	}
	// prompt waits for the UI's reply.
	done := make(chan json.RawMessage, 1)
	go func() {
		res, _ := c.Call(context.Background(), "prompt", map[string]string{"title": "Name"})
		done <- res
	}()
	ev := nextEvent(t, h)
	if ev.Msg.Method != "prompt" || !ev.Msg.IsRequest() {
		t.Fatalf("event %+v", ev)
	}
	select {
	case <-done:
		t.Fatal("prompt answered before the UI did")
	default:
	}
	// The UI answers (Host.Reply does this through the same conn).
	if err := host.Reply(ev.Msg.ID, "Ann", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case res := <-done:
		if string(res) != `"Ann"` {
			t.Fatalf("prompt answer %s", res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("prompt never answered")
	}
}

func TestConfigDefaultsAndNames(t *testing.T) {
	h := NewHost(Config{})
	c := h.cfg
	if c.MaxRestarts != 3 || c.InitTimeout != 10*time.Second || c.CallTimeout != 30*time.Second || c.StopTimeout != 2*time.Second {
		t.Fatalf("defaults %+v", c)
	}
	for n, want := range map[int]time.Duration{1: 500 * time.Millisecond, 2: time.Second, 3: 2 * time.Second, 9: 10 * time.Second} {
		if got := c.Backoff(n); got != want {
			t.Errorf("Backoff(%d) = %v, want %v", n, got, want)
		}
	}
	if StateFailed.String() != "failed" || StateIdle.String() != "idle" {
		t.Fatal("state names")
	}
	for k, want := range map[EventKind]string{EvStarted: "started", EvMessage: "message", EvExited: "exited", EvFailed: "failed", 0: "?"} {
		if k.String() != want {
			t.Errorf("%d = %q", k, k.String())
		}
	}
	select {
	case <-h.Done():
		t.Fatal("done before Close")
	default:
	}
	h.Close()
	h.Close() // twice is fine
	<-h.Done()
}

func TestLogKeepsLastLines(t *testing.T) {
	h := NewHost(Config{})
	for i := 0; i < logMax+20; i++ {
		h.logf("x", "line %d", i)
	}
	l := h.Log()
	if len(l) != logMax || !strings.HasSuffix(l[0], "line 20") || !strings.HasSuffix(l[len(l)-1], "line 519") {
		t.Fatalf("len %d first %q last %q", len(l), l[0], l[len(l)-1])
	}
}
