//go:build unix

package webgw

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (MOB-23): a watch token is random, works for its device until
// WatchTTL, is kept only as a digest in a 0600 file that a new gateway
// reads again, a device keeps MaxWatchTokens (the oldest goes), and a
// removed device's tokens stop at once.
func TestWatchTokens(t *testing.T) {
	dir := t.TempDir()
	now := time.Unix(1_800_000_000, 0)
	wt, err := OpenWatchTokens(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := wt.Issue("", now); err == nil {
		t.Fatal("a token for no device")
	}
	a, exp, err := wt.Issue("dev-1", now)
	if err != nil || len(a) < 32 || !exp.Equal(now.Add(webproto.WatchTTL)) {
		t.Fatalf("issue: %q %v %v", a, exp, err)
	}
	if d, ok := wt.Check(a, now.Add(time.Hour)); !ok || d != "dev-1" {
		t.Fatalf("check: %q %v", d, ok)
	}
	if _, ok := wt.Check(a+"x", now); ok {
		t.Error("a wrong token works")
	}
	raw, _ := os.ReadFile(filepath.Join(dir, WatchTokensFile))
	if strings.Contains(string(raw), a) {
		t.Error("the token itself is on disk")
	}
	if fi, _ := os.Stat(filepath.Join(dir, WatchTokensFile)); fi == nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v", fi)
	}
	again, err := OpenWatchTokens(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := again.Check(a, now); !ok || d != "dev-1" {
		t.Error("a new gateway does not know the token")
	}
	b, _, _ := wt.Issue("dev-1", now.Add(time.Second))
	c, _, _ := wt.Issue("dev-1", now.Add(2*time.Second))
	if _, ok := wt.Check(a, now); ok {
		t.Error("a third token kept the oldest")
	}
	if _, ok := wt.Check(b, now); !ok {
		t.Error("the second token went")
	}
	other, _, _ := wt.Issue("dev-2", now)
	if n := wt.RevokeDevice("dev-1"); n != 2 {
		t.Errorf("revoked %d, want 2", n)
	}
	if _, ok := wt.Check(c, now); ok {
		t.Error("a removed device's token works")
	}
	if _, ok := wt.Check(other, now); !ok {
		t.Error("revoke dropped another device's token")
	}
	if _, ok := wt.Check(other, now.Add(webproto.WatchTTL)); ok {
		t.Error("a token works past WatchTTL")
	}
}

// Spec: a damaged file is no tokens (said once), never a crash.
func TestWatchTokensDamagedFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, WatchTokensFile), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	wt, err := OpenWatchTokens(dir)
	if err == nil || wt == nil {
		t.Fatalf("damaged file: %v %v", wt, err)
	}
	if _, _, err := wt.Issue("dev-1", time.Now()); err != nil {
		t.Errorf("issue after a damaged file: %v", err)
	}
}

type watchRig struct {
	t     *testing.T
	srv   *httptest.Server
	watch *Watch
	toks  *WatchTokens
	store *webdevices.Store
	inbox *InboxMux
	mu    sync.Mutex
	told  []bool
}

const watchHome = "0011223344556677"

func newWatchRig(t *testing.T) *watchRig {
	t.Helper()
	dir := t.TempDir()
	r := &watchRig{t: t, store: webdevices.Open(dir), inbox: NewInboxMux(watchHome)}
	var err error
	if r.toks, err = OpenWatchTokens(dir); err != nil {
		t.Fatal(err)
	}
	for _, d := range []webdevices.Device{{ID: "dev-1", Name: "Pixel", Role: webdevices.RoleControl},
		{ID: "dev-g", Name: "Guest", Role: webdevices.RoleViewer, Guest: true}} {
		if err := r.store.Add(d); err != nil {
			t.Fatal(err)
		}
	}
	r.watch = &Watch{Tokens: r.toks, Store: r.store, Inbox: r.inbox, Every: 10 * time.Millisecond, Ping: time.Hour,
		Watching: func(_ context.Context, on bool) error {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.told = append(r.told, on)
			return nil
		}}
	r.srv = httptest.NewServer(r.watch)
	t.Cleanup(r.srv.Close)
	return r
}

func (r *watchRig) get(path, auth string) *http.Response {
	r.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, r.srv.URL+path, nil)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	return res
}

func (r *watchRig) token(dev string) string {
	r.t.Helper()
	tok, _, err := r.toks.Issue(dev, time.Now())
	if err != nil {
		r.t.Fatal(err)
	}
	return tok
}

func (r *watchRig) setInbox(items ...webproto.InboxItem) {
	r.t.Helper()
	s := webproto.InboxSnap{Items: items}
	for _, it := range items {
		s.Counts.Add(it.State)
	}
	b, _ := json.Marshal(s)
	if err := r.inbox.Set("", b); err != nil {
		r.t.Fatal(err)
	}
}

func readFrame(t *testing.T, br *bufio.Reader) map[string]any {
	t.Helper()
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() { l, err := br.ReadString('\n'); ch <- res{l, err} }()
	select {
	case got := <-ch:
		if got.err != nil {
			t.Fatalf("read frame: %v", got.err)
		}
		var f map[string]any
		if err := json.Unmarshal([]byte(got.line), &f); err != nil {
			t.Fatalf("frame %q: %v", got.line, err)
		}
		return f
	case <-time.After(5 * time.Second):
		t.Fatal("no frame")
	}
	return nil
}

// Spec: /watch needs a live watch token in the Authorization header;
// none, a wrong one, one in the URL, a guest's and a POST are refused.
func TestWatchRefuses(t *testing.T) {
	r := newWatchRig(t)
	tok := r.token("dev-1")
	for _, c := range []struct {
		name, path, auth string
		want             int
	}{
		{"no token", "/watch", "", http.StatusUnauthorized},
		{"wrong token", "/watch", "Bearer nope", http.StatusUnauthorized},
		{"token in the URL", "/watch?token=" + tok, "Bearer " + tok, http.StatusUnauthorized},
		{"other path", "/watch/x", "Bearer " + tok, http.StatusNotFound},
		{"guest", "/watch", "Bearer " + r.token("dev-g"), http.StatusUnauthorized},
	} {
		res := r.get(c.path, c.auth)
		_ = res.Body.Close()
		if res.StatusCode != c.want {
			t.Errorf("%s: %d, want %d", c.name, res.StatusCode, c.want)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, r.srv.URL+"/watch", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", res.StatusCode)
	}
	if r.watch.Live() != 0 {
		t.Error("a refused request counts as a watcher")
	}
}

// Spec: the stream starts with the state (no old items as new), sends a
// frame with the newly waiting item when the inbox changes, tells the
// hub on the first open and the last close, and ends when the device is
// removed.
func TestWatchStream(t *testing.T) {
	r := newWatchRig(t)
	r.setInbox(webproto.InboxItem{ID: "card:1", Kind: webproto.InboxCard, Ref: "1", Title: "Old card", State: webproto.InboxNeedsYou, Since: 1})
	res := r.get("/watch", "Bearer "+r.token("dev-1"))
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/x-ndjson") {
		t.Fatalf("open: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	br := bufio.NewReader(res.Body)
	f := readFrame(t, br)
	if f["t"] != "watch" || f["new"] != nil || !strings.HasPrefix(f["line"].(string), "1 thing needs you") {
		t.Fatalf("first frame: %v", f)
	}
	r.setInbox(
		webproto.InboxItem{ID: "card:2", Kind: webproto.InboxCard, Ref: "2", Title: "Allow a write?", State: webproto.InboxNeedsYou, Since: 2},
		webproto.InboxItem{ID: "card:1", Kind: webproto.InboxCard, Ref: "1", Title: "Old card", State: webproto.InboxNeedsYou, Since: 1})
	f = readFrame(t, br)
	news, _ := f["new"].([]any)
	if len(news) != 1 || news[0].(map[string]any)["title"] != "Allow a write?" || news[0].(map[string]any)["link"] != "#/home?card=2" {
		t.Fatalf("change frame: %v", f)
	}
	r.mu.Lock()
	told := append([]bool(nil), r.told...)
	r.mu.Unlock()
	if len(told) == 0 || !told[0] {
		t.Errorf("the hub was not told: %v", told)
	}
	if n := r.watch.CloseDevice("dev-1"); n != 1 {
		t.Fatalf("closed %d sockets", n)
	}
	done := make(chan struct{})
	go func() { _, _ = br.ReadString('\n'); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the socket stayed open after the device was removed")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		r.mu.Lock()
		n := len(r.told)
		last := n > 0 && !r.told[n-1]
		r.mu.Unlock()
		if last {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the hub was not told the last watcher left")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Spec: an idle socket gets a ping frame with nothing else in it.
func TestWatchPing(t *testing.T) {
	r := newWatchRig(t)
	r.watch.Ping = 20 * time.Millisecond
	res := r.get("/watch", "Bearer "+r.token("dev-1"))
	defer res.Body.Close()
	br := bufio.NewReader(res.Body)
	_ = readFrame(t, br)
	f := readFrame(t, br)
	if f["t"] != "ping" || len(f) != 1 {
		t.Fatalf("ping frame: %v", f)
	}
}

// Spec: past maxInboxWatchers sockets the next one gets 503.
func TestWatchTooMany(t *testing.T) {
	r := newWatchRig(t)
	var open []*http.Response
	defer func() {
		for _, res := range open {
			_ = res.Body.Close()
		}
	}()
	tok := r.token("dev-1")
	for range maxInboxWatchers {
		res := r.get("/watch", "Bearer "+tok)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("socket %d: %d", len(open)+1, res.StatusCode)
		}
		_ = readFrame(t, bufio.NewReader(res.Body))
		open = append(open, res)
	}
	res := r.get("/watch", "Bearer "+tok)
	_ = res.Body.Close()
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("one too many: %d", res.StatusCode)
	}
}

// Spec: the hub's inbox_watch control event feeds the merged inbox; a
// removed device's watch tokens and sockets end with its sessions.
func TestWatchCtlEvents(t *testing.T) {
	r := newWatchRig(t)
	tok := r.token("dev-1")
	snap := webproto.InboxSnap{Items: []webproto.InboxItem{{ID: "card:9", Kind: webproto.InboxCard, Ref: "9", Title: "x", State: webproto.InboxNeedsYou, Since: 1}}}
	snap.Counts.Add(webproto.InboxNeedsYou)
	b, _ := json.Marshal(snap)
	targets := ctlTargets{tokens: NewTokens(time.Hour, time.Hour), watchToks: r.toks, watch: r.watch, inbox: r.inbox}
	onCtlEvent(webproto.EvInboxWatch, b, targets)
	if got := r.inbox.Snap(); got.Counts.NeedsYou != 1 || len(got.Items) != 1 {
		t.Fatalf("inbox not fed: %+v", got)
	}
	onCtlEvent(webproto.EvInboxWatch, []byte(`{"items":[],"bad":1}`), targets)
	if r.inbox.Snap().Counts.NeedsYou != 1 {
		t.Error("a bad inbox event replaced the last good one")
	}
	onCtlEvent(EvRevokeDevice, []byte(`{"device":"dev-1"}`), targets)
	if _, ok := r.toks.Check(tok, time.Now()); ok {
		t.Error("a removed device's watch token works")
	}
}
