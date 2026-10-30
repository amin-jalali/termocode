//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
)

// fakeWebHub answers control requests on ws's hub socket with reply
// (after one event line meant for a gateway) and records them.
func fakeWebHub(t *testing.T, ws, reply string) chan []string {
	t.Helper()
	p, err := session.PathsFor(ws)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", p.WebSocket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan []string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		first, _ := br.ReadString('\n')
		req, _ := br.ReadString('\n')
		got <- []string{strings.TrimSpace(first), strings.TrimSpace(req)}
		_, _ = io.WriteString(c, `{"t":"ev","kind":"link","data":{"sha256":"00"}}`+"\n"+reply+"\n")
	}()
	return got
}

func runWebT(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	done := make(chan int, 1)
	go func() { done <- runWebIn(args, &out, &errb, "") }()
	select {
	case c := <-done:
		return c, out.String(), errb.String()
	case <-time.After(10 * time.Second):
		t.Fatal("termocode web did not return")
	}
	return 0, "", ""
}

// Spec (T52, WEB-9): `termocode web start` only asks the UI over the hub
// socket (web.open on a control connection); --yes is passed on.
func TestWebStartAsksTheUI(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	got := fakeWebHub(t, ws, `{"t":"res","id":1,"data":{"port":41237}}`)
	code, out, errs := runWebT(t, "start", "--yes", "--dir", ws)
	if code != 0 || !strings.Contains(out, "http://127.0.0.1:41237/") {
		t.Fatalf("exit %d out %q err %q", code, out, errs)
	}
	lines := <-got
	if lines[0] != `{"t":"ctl"}` {
		t.Fatalf("first line %q", lines[0])
	}
	var req struct {
		T, Op string
		ID    int
		Args  map[string]bool
	}
	if err := json.Unmarshal([]byte(lines[1]), &req); err != nil || req.T != "req" || req.Op != "web.open" || !req.Args["yes"] {
		t.Fatalf("request %q", lines[1])
	}
	if strings.Contains(out, "#s=") {
		t.Fatal("a secret link was printed")
	}
}

// Spec: a no on the laptop is exit 1 with the UI's text; stop sends
// web.stop.
func TestWebStartRefusedAndStop(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	fakeWebHub(t, ws, `{"t":"err","id":1,"code":"forbidden","msg":"Not opened: the laptop said no."}`)
	if code, _, errs := runWebT(t, "--dir", ws); code != 1 || !strings.Contains(errs, "the laptop said no") {
		t.Fatalf("exit %d err %q", code, errs)
	}
	ws2 := shortTemp(t)
	got := fakeWebHub(t, ws2, `{"t":"res","id":1}`)
	if code, out, _ := runWebT(t, "stop", "--dir", ws2); code != 0 || !strings.Contains(out, "stopped") {
		t.Fatalf("stop: exit %d out %q", code, out)
	}
	if l := <-got; !strings.Contains(l[1], `"op":"web.stop"`) {
		t.Fatalf("request %q", l[1])
	}
}

// Spec (E11): with no termocode (or the panel off) here: exit 2 and the
// config hint; status says off.
func TestWebSubcommandOffExits2(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	code, _, errs := runWebT(t, "start", "--dir", ws)
	if code != 2 || !strings.Contains(errs, `"web_panel": "on"`) {
		t.Fatalf("exit %d err %q", code, errs)
	}
	if code, out, _ := runWebT(t, "status", "--dir", ws); code != 1 || !strings.Contains(out, "off") {
		t.Fatalf("status: exit %d out %q", code, out)
	}
	if code, _, _ := runWebT(t, "--bogus"); code != 2 {
		t.Fatalf("bad arg exit %d", code)
	}
}

// Spec: status reads the gateway's status file (no secret in it).
func TestWebStatusRunning(t *testing.T) {
	sessionEnv(t)
	ws := shortTemp(t)
	p, _ := session.PathsFor(ws)
	st := `{"pid":` + itoa(os.Getpid()) + `,"port":41237,"url":"http://127.0.0.1:41237/","started":"2026-10-30T10:00:00Z","idle_s":900,"max_s":43200}`
	if err := os.WriteFile(p.WebStatus, []byte(st), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ := runWebT(t, "status", "--dir", ws)
	if code != 0 || !strings.Contains(out, "http://127.0.0.1:41237/") || !strings.Contains(out, "15 minutes") {
		t.Fatalf("exit %d out %q", code, out)
	}
}

// Spec: the gateway process refuses to run when not started by
// termocode (no folder, no audit path).
func TestWebGatewayNeedsItsArgs(t *testing.T) {
	var errb bytes.Buffer
	if code := runWebGatewayIn(nil, nil, &errb); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestUsageListsWeb(t *testing.T) {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	printUsage()
	os.Stdout = old
	_ = w.Close()
	b, _ := io.ReadAll(r)
	if !strings.Contains(string(b), "termocode web [start|stop|status]") {
		t.Fatalf("usage has no web line:\n%s", b)
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}
