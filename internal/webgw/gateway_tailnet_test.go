//go:build unix

package webgw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// fakeServe stands for `tailscale serve` in the foreground.
type fakeServe struct {
	mu      sync.Mutex
	targets []string
	stopped int
	done    chan struct{}
	once    sync.Once
	fail    error
}

func newFakeServe() *fakeServe { return &fakeServe{done: make(chan struct{})} }

func (f *fakeServe) start(target string) (ServeProc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	f.targets = append(f.targets, target)
	return f, nil
}

func (f *fakeServe) Stop() {
	f.mu.Lock()
	f.stopped++
	f.mu.Unlock()
	f.exit()
}
func (f *fakeServe) exit()                 { f.once.Do(func() { close(f.done) }) }
func (f *fakeServe) Done() <-chan struct{} { return f.done }
func (f *fakeServe) Reason() string        { return "serve ended" }
func (f *fakeServe) target() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.targets) == 0 {
		return ""
	}
	return f.targets[len(f.targets)-1]
}

// liveTS is a fake CLI whose serve status follows the fake Serve: ours
// once Serve started, plus whatever the test sets.
type liveTS struct {
	mu     sync.Mutex
	serve  *fakeServe
	status string
	extra  string // e.g. a Funnel flag, raw JSON members
}

func (l *liveTS) run(_ context.Context, args ...string) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch strings.Join(args, " ") {
	case "status --json":
		return []byte(l.status), nil
	case "serve status --json":
		var parts []string
		if tg := l.serve.target(); tg != "" && l.serve.stoppedN() == 0 {
			parts = append(parts, `"Web":{"box.tail1234.ts.net:443":{"Handlers":{"/":{"Proxy":"`+tg+`"}}}}`)
		}
		if l.extra != "" {
			parts = append(parts, l.extra)
		}
		return []byte("{" + strings.Join(parts, ",") + "}"), nil
	}
	return nil, errors.New("unexpected")
}

func (l *liveTS) set(extra string) {
	l.mu.Lock()
	l.extra = extra
	l.mu.Unlock()
}

func (f *fakeServe) stoppedN() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stopped
}

func tailnetRig(t *testing.T, every time.Duration) (*gwRig, *liveTS, *fakeServe, io.Reader) {
	t.Helper()
	fs := newFakeServe()
	ts := &liveTS{serve: fs, status: tsStatusRunning}
	pr, pw := io.Pipe()
	r := startGateway(t, func(c *RunConfig) {
		c.Handoff = pw
		c.Tailnet = &TailnetConfig{Run: ts.run, Start: fs.start, Every: every, Wait: 5 * time.Second}
	})
	return r, ts, fs, pr
}

// Tailnet mode serves on 127.0.0.1 behind `tailscale serve` for the
// panel's whole life: the handoff and the status file name the https
// tailnet URL, the site answers only to that name, and Serve stops with
// the gateway.
func TestRunTailnetServes(t *testing.T) {
	r, _, fs, pr := tailnetRig(t, time.Hour)
	r.link()
	var h struct {
		Port int    `json:"port"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(pr).Decode(&h); err != nil {
		t.Fatal(err)
	}
	if h.URL != "https://box.tail1234.ts.net/" || h.Port == 0 {
		t.Fatalf("handoff %+v", h)
	}
	if got, want := fs.target(), "http://127.0.0.1:"+strconv.Itoa(h.Port); got != want {
		t.Fatalf("serve target %q, want %q", got, want)
	}
	st, err := ReadStatus(r.cfg.StatusPath)
	if err != nil || st.URL != "https://box.tail1234.ts.net/" {
		t.Fatalf("status %+v %v", st, err)
	}
	base := "http://127.0.0.1:" + strconv.Itoa(h.Port)
	for _, tc := range []struct {
		host, origin string
		code         int
	}{
		{"box.tail1234.ts.net", "https://box.tail1234.ts.net", http.StatusBadRequest}, // reached /auth (empty body)
		{"127.0.0.1:" + strconv.Itoa(h.Port), "http://127.0.0.1:" + strconv.Itoa(h.Port), http.StatusMisdirectedRequest},
	} {
		req, _ := http.NewRequest(http.MethodPost, base+"/auth", strings.NewReader(`{}`))
		req.Host = tc.host
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.code {
			t.Errorf("host %s: %d, want %d", tc.host, resp.StatusCode, tc.code)
		}
	}
	r.ctl.send(t, `{"t":"bye","code":4009}`)
	if code := r.wait(); code != webproto.CloseStopped {
		t.Fatalf("code %d", code)
	}
	if fs.stoppedN() == 0 {
		t.Fatal("tailscale serve still runs after the gateway stopped")
	}
	if b, _ := os.ReadFile(r.cfg.AuditPath); !strings.Contains(string(b), "box.tail1234.ts.net") {
		t.Errorf("audit start does not name the tailnet URL:\n%s", b)
	}
}

// Funnel on the port at the start: the gateway refuses and never starts
// Serve (E9, T16).
func TestRunTailnetRefusesFunnel(t *testing.T) {
	fs := newFakeServe()
	ts := &liveTS{serve: fs, status: tsStatusRunning, extra: `"AllowFunnel":{"box.tail1234.ts.net:443":true}`}
	_, err := runOnce(t, &TailnetConfig{Run: ts.run, Start: fs.start, Every: time.Hour, Wait: time.Second})
	if !errors.Is(err, ErrFunnel) {
		t.Fatalf("err = %v, want ErrFunnel", err)
	}
	if fs.target() != "" {
		t.Fatal("serve was started with Funnel on")
	}
}

// Not on a tailnet: refused before anything listens.
func TestRunTailnetNotOnTailnet(t *testing.T) {
	fs := newFakeServe()
	ts := &liveTS{serve: fs, status: `{"BackendState":"NeedsLogin"}`}
	if _, err := runOnce(t, &TailnetConfig{Run: ts.run, Start: fs.start}); !errors.Is(err, ErrNotOnTailnet) {
		t.Fatalf("err = %v", err)
	}
}

// Serve that dies at once (no operator rights, say) is a start error
// with its own last line.
func TestRunTailnetServeFails(t *testing.T) {
	fs := newFakeServe()
	fs.fail = errors.New("Access denied: serve config denied")
	ts := &liveTS{serve: fs, status: tsStatusRunning}
	_, err := runOnce(t, &TailnetConfig{Run: ts.run, Start: fs.start, Wait: time.Second})
	if err == nil || !strings.Contains(err.Error(), "operator") {
		t.Fatalf("err = %v, want the operator hint", err)
	}
}

// Funnel turned on while the panel runs: every browser is closed and the
// gateway stops within one check (T16).
func TestRunTailnetStopsOnFunnelLater(t *testing.T) {
	r, ts, fs, pr := tailnetRig(t, 20*time.Millisecond)
	r.link()
	r.handoff(pr)
	ts.set(`"AllowFunnel":{"box.tail1234.ts.net:443":true}`)
	if code := r.wait(); code != webproto.CloseStopped {
		t.Fatalf("code %d", code)
	}
	if fs.stoppedN() == 0 {
		t.Fatal("serve not stopped")
	}
	b, _ := os.ReadFile(r.cfg.AuditPath)
	if !strings.Contains(string(b), `"ev":"funnel_stop"`) {
		t.Errorf("no funnel_stop in the audit:\n%s", b)
	}
}

// Serve gone (the user ran tailscale serve reset, or it crashed): the
// panel cannot be reached, so it stops instead of waiting for idle.
func TestRunTailnetStopsWhenServeEnds(t *testing.T) {
	r, _, fs, pr := tailnetRig(t, time.Hour)
	r.link()
	r.handoff(pr)
	fs.exit()
	if code := r.wait(); code != webproto.CloseStopped {
		t.Fatalf("code %d", code)
	}
}

// runOnce runs a tailnet gateway that is expected to fail its start.
func runOnce(t *testing.T, tc *TailnetConfig) (webproto.CloseCode, error) {
	t.Helper()
	dir := t.TempDir()
	hub := newFakeHub(t)
	cfg := RunConfig{Dial: hub.dial, AuditPath: dir + "/a.jsonl", StatusPath: dir + "/s.json", LockPath: dir + "/l", Tailnet: tc,
		LinkWait: time.Second}
	return Run(context.Background(), cfg)
}
