package relaytun

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// pipeMsgs is an in-memory message pipe; every message that crosses it
// is recorded (what a relay would see).
type pipeMsgs struct {
	in     chan []byte
	out    chan []byte
	closed chan struct{}
	once   *sync.Once
	rec    *recorder
}

type recorder struct {
	mu  sync.Mutex
	all [][]byte
}

func (r *recorder) add(b []byte) {
	r.mu.Lock()
	r.all = append(r.all, append([]byte(nil), b...))
	r.mu.Unlock()
}

func (r *recorder) joined() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return bytes.Join(r.all, nil)
}

func msgPipe() (a, b *pipeMsgs, rec *recorder) {
	ab, ba := make(chan []byte, 64), make(chan []byte, 64)
	closed := make(chan struct{})
	once := &sync.Once{}
	rec = &recorder{}
	return &pipeMsgs{in: ba, out: ab, closed: closed, once: once, rec: rec},
		&pipeMsgs{in: ab, out: ba, closed: closed, once: once, rec: rec}, rec
}

func (p *pipeMsgs) ReadMsg(ctx context.Context) ([]byte, error) {
	select {
	case b := <-p.in:
		return b, nil
	case <-p.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *pipeMsgs) WriteMsg(ctx context.Context, b []byte) error {
	p.rec.add(b)
	select {
	case p.out <- append([]byte(nil), b...):
		return nil
	case <-p.closed:
		return io.EOF
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *pipeMsgs) Close() error {
	p.once.Do(func() { close(p.closed) })
	return nil
}

func keys(t *testing.T) (gw, page Keypair) {
	t.Helper()
	var err error
	if gw, err = GenerateKeypair(); err != nil {
		t.Fatal(err)
	}
	if page, err = GenerateKeypair(); err != nil {
		t.Fatal(err)
	}
	return gw, page
}

// connect runs both handshakes over one pipe.
func connect(t *testing.T, gwKey, pageKey Keypair, gwPub [32]byte, hello Hello, allow Allow) (gw, page *Session, rec *recorder, gwErr, pageErr error) {
	t.Helper()
	a, b, rec := msgPipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		gw, gwErr = Accept(ctx, a, gwKey, allow)
	}()
	page, pageErr = Dial(ctx, b, pageKey, gwPub, hello)
	wg.Wait()
	t.Cleanup(func() {
		if gw != nil {
			_ = gw.Close()
		}
		if page != nil {
			_ = page.Close()
		}
	})
	return gw, page, rec, gwErr, pageErr
}

func allowAll(PeerKey, Hello) error { return nil }

func TestHandshakeGivesBothEndsTheKeysAndTheHello(t *testing.T) {
	gwKey, pageKey := keys(t)
	var gotPeer PeerKey
	var gotHello Hello
	gw, page, _, gerr, perr := connect(t, gwKey, pageKey, gwKey.Public, Hello{Pair: "pair-secret"}, func(p PeerKey, h Hello) error {
		gotPeer, gotHello = p, h
		return nil
	})
	if gerr != nil || perr != nil {
		t.Fatalf("handshake: gw %v page %v", gerr, perr)
	}
	if gotPeer != pageKey.Public || gw.Peer() != pageKey.Public {
		t.Fatal("the gateway did not learn the page's static key")
	}
	if gotHello.Pair != "pair-secret" {
		t.Fatalf("hello = %+v", gotHello)
	}
	if page.Peer() != gwKey.Public {
		t.Fatal("the page's peer is not the gateway key")
	}
}

func TestAPageWithTheWrongGatewayKeyFails(t *testing.T) {
	gwKey, pageKey := keys(t)
	other, _ := GenerateKeypair()
	_, _, _, gerr, perr := connect(t, gwKey, pageKey, other.Public, Hello{}, allowAll)
	if gerr == nil || perr == nil {
		t.Fatalf("handshake with a wrong gateway key: gw %v page %v, want both to fail", gerr, perr)
	}
}

func TestARefusedPageLearnsWhyAndGetsNoSession(t *testing.T) {
	gwKey, pageKey := keys(t)
	_, page, _, gerr, perr := connect(t, gwKey, pageKey, gwKey.Public, Hello{}, func(PeerKey, Hello) error {
		return ErrNotPaired
	})
	if !errors.Is(gerr, ErrNotPaired) || page != nil {
		t.Fatalf("gw err %v page %v", gerr, page)
	}
	var re *RefusedError
	if !errors.As(perr, &re) || re.Why != "not_paired" {
		t.Fatalf("page err = %v, want a refusal saying not_paired", perr)
	}
}

func TestStreamsCarryBytesBothWaysAndTheWireIsCiphertext(t *testing.T) {
	gwKey, pageKey := keys(t)
	gw, page, rec, gerr, perr := connect(t, gwKey, pageKey, gwKey.Public, Hello{}, allowAll)
	if gerr != nil || perr != nil {
		t.Fatal(gerr, perr)
	}
	marker := []byte("PLAINTEXT-MARKER-chat-line")
	big := bytes.Repeat([]byte("TERMINAL-LINE-"), 100_000) // 1.4 MB: more than a window
	done := make(chan error, 1)
	go func() {
		c, err := gw.AcceptStream()
		if err != nil {
			done <- err
			return
		}
		got, err := io.ReadAll(io.LimitReader(c, int64(len(marker))))
		if err != nil || !bytes.Equal(got, marker) {
			done <- errors.New("gw got " + string(got))
			return
		}
		if _, err := c.Write(big); err != nil {
			done <- err
			return
		}
		done <- c.Close()
	}()
	c, err := page.OpenStream()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write(marker); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(c)
	if err != nil || !bytes.Equal(got, big) {
		t.Fatalf("page read %d bytes, err %v; want %d", len(got), err, len(big))
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	wire := rec.joined()
	for _, m := range [][]byte{marker, []byte("TERMINAL-LINE-")} {
		if bytes.Contains(wire, m) {
			t.Fatalf("the wire holds the plaintext %q", m)
		}
	}
}

func TestManyStreamsAtOnce(t *testing.T) {
	gwKey, pageKey := keys(t)
	gw, page, _, gerr, perr := connect(t, gwKey, pageKey, gwKey.Public, Hello{}, allowAll)
	if gerr != nil || perr != nil {
		t.Fatal(gerr, perr)
	}
	go func() {
		for {
			c, err := gw.AcceptStream()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(c, c); _ = c.Close() }() // echo until the page closes its side
		}
	}()
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := page.OpenStream()
			if err != nil {
				t.Error(err)
				return
			}
			msg := bytes.Repeat([]byte{byte(i)}, 50_000)
			go func() { _, _ = c.Write(msg); _ = c.(interface{ CloseWrite() error }).CloseWrite() }()
			got, err := io.ReadAll(c)
			if err != nil || !bytes.Equal(got, msg) {
				t.Errorf("stream %d: %d bytes, %v", i, len(got), err)
			}
		}()
	}
	wg.Wait()
}

func TestATamperedMessageEndsTheSession(t *testing.T) {
	gwKey, pageKey := keys(t)
	a, b, _ := msgPipe()
	tamper := &flipper{pipeMsgs: b}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var gw *Session
	var gerr error
	ready := make(chan struct{})
	go func() { gw, gerr = Accept(ctx, a, gwKey, allowAll); close(ready) }()
	page, err := Dial(ctx, tamper, pageKey, gwKey.Public, Hello{})
	<-ready
	if err != nil || gerr != nil {
		t.Fatal(err, gerr)
	}
	defer func() { _ = page.Close() }()
	tamper.on.Store(true)
	c, _ := page.OpenStream()
	_, _ = c.Write([]byte("x"))
	select {
	case <-gw.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("the gateway kept a session after a changed message")
	}
	if !errors.Is(gw.Err(), ErrDecrypt) {
		t.Fatalf("session end = %v, want ErrDecrypt", gw.Err())
	}
}

type flipper struct {
	*pipeMsgs
	on atomicBool
}

type atomicBool struct {
	mu sync.Mutex
	v  bool
}

func (a *atomicBool) Store(v bool) { a.mu.Lock(); a.v = v; a.mu.Unlock() }
func (a *atomicBool) Load() bool   { a.mu.Lock(); defer a.mu.Unlock(); return a.v }

func (f *flipper) WriteMsg(ctx context.Context, b []byte) error {
	if f.on.Load() && len(b) > 0 {
		b = append([]byte(nil), b...)
		b[len(b)-1] ^= 1
	}
	return f.pipeMsgs.WriteMsg(ctx, b)
}

func TestHTTPServesOverTheTunnel(t *testing.T) {
	gwKey, pageKey := keys(t)
	gw, page, rec, gerr, perr := connect(t, gwKey, pageKey, gwKey.Public, Hello{}, allowAll)
	if gerr != nil || perr != nil {
		t.Fatal(gerr, perr)
	}
	ln := NewListener()
	defer func() { _ = ln.Close() }()
	go ln.Serve(gw)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello from "+r.URL.Path+" SECRET-FILE-NAME.go")
	})}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()
	client := &http.Client{Transport: &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		return page.OpenStream()
	}}, Timeout: 5 * time.Second}
	for range 3 {
		resp, err := client.Get("http://gateway.invalid/x")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if !strings.Contains(string(b), "hello from /x") {
			t.Fatalf("body = %q", b)
		}
	}
	if bytes.Contains(rec.joined(), []byte("SECRET-FILE-NAME")) {
		t.Fatal("the wire holds plaintext")
	}
}

func TestStreamDeadlines(t *testing.T) {
	gwKey, pageKey := keys(t)
	_, page, _, gerr, perr := connect(t, gwKey, pageKey, gwKey.Public, Hello{}, allowAll)
	if gerr != nil || perr != nil {
		t.Fatal(gerr, perr)
	}
	c, _ := page.OpenStream()
	_ = c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	_, err := c.Read(make([]byte, 1))
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("read past the deadline = %v, want a timeout", err)
	}
}

func TestPeerKeyText(t *testing.T) {
	k, _ := GenerateKeypair()
	s := k.Public.String()
	back, err := ParsePeerKey(s)
	if err != nil || back != k.Public || len(s) != 43 {
		t.Fatalf("round trip %q: %v", s, err)
	}
	if _, err := ParsePeerKey("short"); err == nil {
		t.Fatal("a short key parsed")
	}
}
