//go:build unix

package webgw

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// upRig is a UnifiedPush with a test distributor (a TLS server) and a
// clock that only moves when the test says so.
type upRig struct {
	u    *UnifiedPush
	srv  *httptest.Server
	mu   sync.Mutex
	got  []upHit
	code int
	now  time.Time
	live map[string]bool
	ons  []bool
}

type upHit struct {
	path, body, ctype string
	auth              bool
}

func newUPRig(t *testing.T) *upRig {
	t.Helper()
	r := &upRig{code: http.StatusCreated, now: time.Unix(1_700_000_000, 0), live: map[string]bool{}}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		b, _ := io.ReadAll(q.Body)
		r.mu.Lock()
		r.got = append(r.got, upHit{path: q.URL.Path, body: string(b), ctype: q.Header.Get("Content-Type"), auth: q.Header.Get("Authorization") != ""})
		code := r.code
		r.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(r.srv.Close)
	r.u = NewUnifiedPush(UnifiedPushConfig{
		Client: r.srv.Client(),
		Now:    func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now },
		Live:   func(d string) bool { r.mu.Lock(); defer r.mu.Unlock(); return r.live[d] },
		Watch: func(_ context.Context, on bool) error {
			r.mu.Lock()
			r.ons = append(r.ons, on)
			r.mu.Unlock()
			return nil
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { r.u.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done; r.u.Wait() })
	return r
}

func (r *upRig) hits() []upHit {
	r.u.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]upHit(nil), r.got...)
}

func (r *upRig) advance(d time.Duration) {
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
}

// waitOns waits until the watch loop told the hub n times.
func (r *upRig) waitOns(t *testing.T, n int) []bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		got := append([]bool(nil), r.ons...)
		r.mu.Unlock()
		if len(got) >= n {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("watch told %d times, want %d", len(r.ons), n)
	return nil
}

// Spec (MOB-32): a wake is a POST of the fixed body "wake" to the
// phone's own endpoint: no count, no title, no token, nothing about what
// waits. A device with a live link gets none (it sees the card already),
// and one device is woken at most once per WakeGap.
func TestUnifiedPushWakeIsContentFree(t *testing.T) {
	r := newUPRig(t)
	if err := r.u.Subscribe("dev-a", r.srv.URL+"/up/a"); err != nil {
		t.Fatal(err)
	}
	if err := r.u.Subscribe("dev-b", r.srv.URL+"/up/b"); err != nil {
		t.Fatal(err)
	}
	r.live["dev-b"] = true
	if n := r.u.Wake(context.Background()); n != 1 {
		t.Fatalf("wakes started %d, want 1", n)
	}
	h := r.hits()
	if len(h) != 1 || h[0].path != "/up/a" || h[0].body != WakeBody || h[0].auth || h[0].ctype != "text/plain" {
		t.Fatalf("hits %+v", h)
	}
	if n := r.u.Wake(context.Background()); n != 0 {
		t.Fatalf("a second wake inside the gap: %d", n)
	}
	r.advance(WakeGap)
	if n := r.u.Wake(context.Background()); n != 1 {
		t.Fatalf("after the gap: %d", n)
	}
	if len(r.hits()) != 2 {
		t.Fatalf("hits %+v", r.hits())
	}
}

// Spec (MOB-32): an endpoint the distributor says is gone (404 or 410)
// is dropped; the hub hears "on" with the first subscription and "off"
// when the last one goes.
func TestUnifiedPushGoneEndpointAndWatch(t *testing.T) {
	r := newUPRig(t)
	if err := r.u.Subscribe("dev-a", r.srv.URL+"/up/a"); err != nil {
		t.Fatal(err)
	}
	if got := r.waitOns(t, 1); !got[0] {
		t.Fatalf("watch %v", got)
	}
	r.mu.Lock()
	r.code = http.StatusGone
	r.mu.Unlock()
	r.u.Wake(context.Background())
	r.u.Wait()
	if r.u.Len() != 0 {
		t.Fatal("a gone endpoint is kept")
	}
	if got := r.waitOns(t, 2); got[1] {
		t.Fatalf("watch %v", got)
	}
}

// Spec (MOB-32): a phone's new endpoint replaces its old one; Forget (a
// removed device) and Unsubscribe drop it; the list is capped.
func TestUnifiedPushSubscribeRules(t *testing.T) {
	r := newUPRig(t)
	if err := r.u.Subscribe("dev-a", r.srv.URL+"/up/1"); err != nil {
		t.Fatal(err)
	}
	if err := r.u.Subscribe("dev-a", r.srv.URL+"/up/2"); err != nil {
		t.Fatal(err)
	}
	if r.u.Len() != 1 {
		t.Fatalf("len %d", r.u.Len())
	}
	r.u.Wake(context.Background())
	if h := r.hits(); len(h) != 1 || h[0].path != "/up/2" {
		t.Fatalf("hits %+v", h)
	}
	if !r.u.Unsubscribe("dev-a") || r.u.Unsubscribe("dev-a") {
		t.Fatal("unsubscribe")
	}
	for i := 0; i < MaxPushSubs; i++ {
		if err := r.u.Subscribe("dev-"+string(rune('a'+i%26))+string(rune('a'+i/26)), r.srv.URL+"/up/x"); err != nil {
			t.Fatalf("sub %d: %v", i, err)
		}
	}
	if err := r.u.Subscribe("dev-over", r.srv.URL+"/up/x"); err != ErrPushFull {
		t.Fatalf("over the cap: %v", err)
	}
	r.u.Forget("dev-aa")
	if r.u.Len() != MaxPushSubs-1 {
		t.Fatalf("forget: len %d", r.u.Len())
	}
	if err := r.u.Subscribe("", r.srv.URL); err == nil {
		t.Fatal("no device taken")
	}
}

// Spec (MOB-32): the default client never reaches the gateway's own
// machine or a cloud metadata address, whatever the name resolves to
// (checked when it dials, so a name that changes its address later is
// caught too); a home or tailnet address is fine (a self-hosted
// distributor).
func TestUnifiedPushDialGuard(t *testing.T) {
	for _, s := range []string{"127.0.0.1", "::1", "0.0.0.0", "169.254.169.254", "fe80::1", "224.0.0.1", "::"} {
		if pushAddrOK(netip.MustParseAddr(s)) {
			t.Errorf("%s allowed", s)
		}
	}
	for _, s := range []string{"192.168.1.20", "10.0.0.5", "100.64.0.7", "93.184.216.34", "2001:db8::1"} {
		if !pushAddrOK(netip.MustParseAddr(s)) {
			t.Errorf("%s refused", s)
		}
	}
	c := pushClient()
	if c.CheckRedirect == nil || c.Timeout <= 0 {
		t.Fatal("the default client follows redirects or has no timeout")
	}
	req, _ := http.NewRequest(http.MethodPost, "https://127.0.0.1:1/up", nil)
	if _, err := c.Do(req); err == nil {
		t.Fatal("dialed loopback")
	}
}
