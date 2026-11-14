//go:build unix

package assets

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/evanw/esbuild/pkg/api"

	"github.com/amin-jalali/termocode/internal/relaytun"
)

// nodeWire is the gateway's MsgConn to the headless page in node: "m
// <base64>" lines on its stdin and stdout.
type nodeWire struct {
	in   chan []byte
	w    io.Writer
	wmu  sync.Mutex
	done chan struct{}
	once sync.Once
}

func (n *nodeWire) ReadMsg(ctx context.Context) ([]byte, error) {
	select {
	case b := <-n.in:
		return b, nil
	case <-n.done:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (n *nodeWire) WriteMsg(_ context.Context, b []byte) error {
	n.wmu.Lock()
	defer n.wmu.Unlock()
	_, err := io.WriteString(n.w, "m "+base64.StdEncoding.EncodeToString(b)+"\n")
	return err
}

func (n *nodeWire) Close() error {
	n.once.Do(func() { close(n.done) })
	return nil
}

// Spec (ADR 0022, WEB-53): the page's own relay code (src/relay: Noise IK
// on WebCrypto, the tunnel's frames and windows, fetch and WebSocket over
// a stream) talks to the gateway's Go side: a JSON post, a 1 MB chunked
// answer, and a WebSocket with its subprotocol. Needs node (CI has none:
// skipped there; the Go side has its own end-to-end test).
func TestRelayPageCodeTalksToTheGateway(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node here")
	}
	abs, _ := filepath.Abs(".")
	res := api.Build(api.BuildOptions{EntryPoints: []string{filepath.Join(abs, "testdata", "relayharness.ts")}, Bundle: true, Write: false,
		Format: api.FormatIIFE, Platform: api.PlatformNode, Target: api.ES2020, LogLevel: api.LogLevelSilent})
	if len(res.Errors) > 0 || len(res.OutputFiles) != 1 {
		t.Fatalf("bundle: %v", res.Errors)
	}
	script := filepath.Join(t.TempDir(), "harness.js")
	if err := os.WriteFile(script, res.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}
	gw, err := relaytun.GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, script, gw.Public.String())
	stdin, _ := cmd.StdinPipe()
	stdout, _ := cmd.StdoutPipe()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	wire := &nodeWire{in: make(chan []byte, 64), w: stdin, done: make(chan struct{})}
	outs := make(chan string, 16)
	pub := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<22)
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "m "):
				b, err := base64.StdEncoding.DecodeString(line[2:])
				if err == nil {
					wire.in <- b
				}
			case strings.HasPrefix(line, "pub "):
				pub <- line[4:]
			case strings.HasPrefix(line, "out "):
				outs <- line[4:]
			}
		}
		_ = wire.Close()
	}()

	var pagePub string
	select {
	case pagePub = <-pub:
	case <-ctx.Done():
		t.Fatalf("no page key: %s", stderr.String())
	}
	sess, err := relaytun.Accept(ctx, wire, gw, func(p relaytun.PeerKey, h relaytun.Hello) error {
		if p.String() != pagePub || h.Pair != "p1" {
			return relaytun.ErrNotPaired
		}
		return nil
	})
	if err != nil {
		t.Fatalf("handshake with the page's code: %v (%s)", err, stderr.String())
	}
	defer func() { _ = sess.Close() }()
	ln := relaytun.NewListener()
	defer func() { _ = ln.Close() }()
	go ln.Serve(sess)
	mux := http.NewServeMux()
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_, _ = io.WriteString(w, r.Method+" "+r.URL.RawQuery+" "+r.Header.Get("Content-Type")+" "+string(b))
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 64<<10)
		for range 16 { // 1 MiB, chunked: more than one window
			_, _ = w.Write(chunk)
			w.(http.Flusher).Flush()
		}
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"termocode.v2"}})
		if err != nil {
			return
		}
		typ, b, err := c.Read(r.Context())
		if err == nil {
			_ = c.Write(r.Context(), typ, append([]byte("echo:"), b...))
		}
		_, _, _ = c.Read(r.Context()) // until the page closes
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	want := []string{
		`fetch 200 POST x=1 application/json {"a":1}`,
		"big 1048576",
		"ws echo:ping-from-page termocode.v2",
		"closed 1000",
	}
	for _, w := range want {
		select {
		case got := <-outs:
			if got != w {
				t.Fatalf("page said %q, want %q (stderr %s)", got, w, stderr.String())
			}
		case <-ctx.Done():
			t.Fatalf("page said nothing; want %q (stderr %s)", w, stderr.String())
		}
	}
}

// Spec (NEW-10): the relay page keeps what the relay sign-in gave: a
// token for the panel, or, when the laptop wants the one-time code, the
// ticket for the panel's code step (termocode.totp), never both; a
// refused sign-in keeps nothing. The panel opens its code step with
// that ticket (SignInScreen), also with no passkey here.
func TestRelaySignInHandsTheCodeStepToThePanel(t *testing.T) {
	out := runJS(t, `
import { keepRelaySignIn } from "../relay/signin";
function eq(a: unknown, b: unknown, what: string) {
  const x = JSON.stringify(a), y = JSON.stringify(b);
  if (x !== y) throw new Error(what + ": got " + x + ", want " + y);
}
const store = () => {
  const m: Record<string, string> = {};
  return { m, setItem: (k: string, v: string) => void (m[k] = v) };
};
let s = store();
keepRelaySignIn(true, { token: "tok" }, s);
eq(s.m, { "termocode.session": "tok" }, "token");
s = store();
keepRelaySignIn(true, { totp: true, ticket: "abc" }, s);
eq(s.m, { "termocode.totp": "abc" }, "ticket");
s = store();
keepRelaySignIn(false, { error: "removed" }, s);
eq(s.m, {}, "refused");
s = store();
keepRelaySignIn(true, { totp: true }, s);
eq(s.m, {}, "no ticket");
console.log("ok");
`, nil)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
	main, sign, panel := readSrc(t, "src/relay/main.ts"), readSrc(t, "src/ui/SignInScreen.tsx"), readSrc(t, "src/main.tsx")
	if !strings.Contains(main, "keepRelaySignIn(") {
		t.Error("relay/main.ts does not keep the relay sign-in's ticket")
	}
	if !strings.Contains(sign, `"termocode.totp"`) || !strings.Contains(sign, `state.value = "code"`) {
		t.Error("SignInScreen does not open the code step with the relay's ticket")
	}
	if !strings.Contains(panel, "relayCode") {
		t.Error("main.tsx does not show the sign-in screen for the relay's code step")
	}
}
