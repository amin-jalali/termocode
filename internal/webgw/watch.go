//go:build unix

package webgw

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The watch socket (MOB-23; webproto/data_watch.go has the protocol).
//
//   - app.watch_token (a gateway op on /ws) gives a paired device a watch
//     token: random, shown once, kept only as its SHA-256 in
//     <devices>/watch-tokens.json (0600), so the app's service still gets
//     in after the gateway restarts. A shared viewer, a launch-link tab and
//     a removed device get none. A device keeps MaxWatchTokens; the oldest
//     goes first.
//   - GET /watch with "Authorization: Bearer <token>" streams NDJSON
//     WatchFrames: the state at once, a frame when the merged inbox
//     changes (at most one per WatchEvery), a ping every WatchPing. The
//     token is never taken from the URL. The socket is closed when the
//     device is removed, its token ends or the gateway stops.
//   - The first socket tells the hub to send its inbox (web.watching
//     {on: true}); the last one to close tells it to stop.

// WatchTokensFile is the file of the watch token digests in the devices
// folder.
const WatchTokensFile = "watch-tokens.json"

// watchWrite bounds one frame write: a phone that stopped reading is
// dropped, it never holds the gateway.
const watchWrite = 10 * time.Second

// Errors of WatchTokens.Issue.
var (
	errWatchNoDevice = errors.New("watch: not a paired device")
)

type watchTok struct {
	Device string `json:"device"`
	Made   int64  `json:"made"` // Unix ms
	Exp    int64  `json:"exp"`  // Unix ms
}

// WatchTokens are the live watch tokens, by SHA-256 digest. Safe for
// concurrent use.
type WatchTokens struct {
	path string // "" keeps them in memory only

	mu     sync.Mutex
	byHash map[[sha256.Size]byte]watchTok
}

// OpenWatchTokens reads the tokens of the devices folder dir ("" keeps
// them in memory). A missing file is no tokens. A damaged one is no
// tokens too and is said once (the caller audits it); it is rewritten on
// the next Issue, so the app's service asks the page for a new token.
func OpenWatchTokens(dir string) (*WatchTokens, error) {
	t := &WatchTokens{byHash: map[[sha256.Size]byte]watchTok{}}
	if dir == "" {
		return t, nil
	}
	t.path = filepath.Join(dir, WatchTokensFile)
	b, err := os.ReadFile(t.path)
	if errors.Is(err, os.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	var disk map[string]watchTok
	if err := json.Unmarshal(b, &disk); err != nil {
		return t, err
	}
	for h, w := range disk {
		d, err := hex.DecodeString(h)
		if err != nil || len(d) != sha256.Size || !strings.HasPrefix(w.Device, "dev-") {
			continue
		}
		t.byHash[[sha256.Size]byte(d)] = w
	}
	return t, nil
}

// Issue makes a watch token of device, valid for webproto.WatchTTL from
// now. The token is returned once and not kept.
func (t *WatchTokens) Issue(device string, now time.Time) (string, time.Time, error) {
	if !strings.HasPrefix(device, "dev-") {
		return "", time.Time{}, errWatchNoDevice
	}
	tok, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	exp := now.Add(webproto.WatchTTL)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweepLocked(now)
	var mine [][sha256.Size]byte
	for h, w := range t.byHash {
		if w.Device == device {
			mine = append(mine, h)
		}
	}
	sort.Slice(mine, func(i, j int) bool { return t.byHash[mine[i]].Made < t.byHash[mine[j]].Made })
	for len(mine) >= webproto.MaxWatchTokens {
		delete(t.byHash, mine[0])
		mine = mine[1:]
	}
	h := sha256.Sum256([]byte(tok))
	t.byHash[h] = watchTok{Device: device, Made: now.UnixMilli(), Exp: exp.UnixMilli()}
	if err := t.saveLocked(); err != nil {
		delete(t.byHash, h)
		return "", time.Time{}, err
	}
	return tok, exp, nil
}

// Check returns the device of a live token.
func (t *WatchTokens) Check(tok string, now time.Time) (string, bool) {
	if tok == "" {
		return "", false
	}
	h := sha256.Sum256([]byte(tok))
	t.mu.Lock()
	defer t.mu.Unlock()
	w, ok := t.byHash[h]
	if !ok {
		return "", false
	}
	if now.UnixMilli() >= w.Exp {
		delete(t.byHash, h)
		_ = t.saveLocked()
		return "", false
	}
	return w.Device, true
}

// RevokeDevice drops every token of device and says how many.
func (t *WatchTokens) RevokeDevice(device string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for h, w := range t.byHash {
		if w.Device == device {
			delete(t.byHash, h)
			n++
		}
	}
	if n > 0 {
		_ = t.saveLocked()
	}
	return n
}

// sweepLocked drops the expired tokens. t.mu is held.
func (t *WatchTokens) sweepLocked(now time.Time) {
	for h, w := range t.byHash {
		if now.UnixMilli() >= w.Exp {
			delete(t.byHash, h)
		}
	}
}

// saveLocked writes the digests atomically, mode 0600. t.mu is held.
func (t *WatchTokens) saveLocked() error {
	if t.path == "" {
		return nil
	}
	disk := make(map[string]watchTok, len(t.byHash))
	for h, w := range t.byHash {
		disk[hex.EncodeToString(h[:])] = w
	}
	b, err := json.Marshal(disk)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(t.path), ".watch-tokens-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if fi, err := os.Lstat(t.path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return syscall.ELOOP
	}
	return os.Rename(tmp.Name(), t.path)
}

// Home is the workspace key of the mux's home hub.
func (m *InboxMux) Home() string { return m.home }

// Watch serves GET /watch.
type Watch struct {
	Tokens *WatchTokens
	Store  *webdevices.Store
	Inbox  *InboxMux
	Audit  *Audit
	Now    func() time.Time
	// Watching tells the hub the first socket opened (true) or the last
	// one closed (false); nil: nobody to tell. It runs on its own
	// goroutine, in order.
	Watching func(ctx context.Context, on bool) error
	// Every and Ping are webproto.WatchEvery and webproto.WatchPing (0:
	// those); tests make them short.
	Every, Ping time.Duration

	mu    sync.Mutex
	live  map[*watchConn]struct{}
	want  bool          // what the hub should be told (mu)
	tell  chan struct{} // wakes the teller
	tellO sync.Once
}

type watchConn struct {
	device string
	kill   chan struct{}
	once   sync.Once
}

func (c *watchConn) end() { c.once.Do(func() { close(c.kill) }) }

// CloseDevice ends the sockets of device and says how many.
func (wt *Watch) CloseDevice(device string) int {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	n := 0
	for c := range wt.live {
		if c.device == device {
			c.end()
			n++
		}
	}
	return n
}

// Live is the number of open sockets.
func (wt *Watch) Live() int {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	return len(wt.live)
}

func (wt *Watch) now() time.Time {
	if wt.Now != nil {
		return wt.Now()
	}
	return time.Now()
}

// add and drop keep the live set; the first add and the last drop tell
// the hub.
func (wt *Watch) add(c *watchConn) {
	wt.mu.Lock()
	if wt.live == nil {
		wt.live = map[*watchConn]struct{}{}
	}
	wt.live[c] = struct{}{}
	first := len(wt.live) == 1
	wt.mu.Unlock()
	if first {
		wt.say(true)
	}
}

func (wt *Watch) drop(c *watchConn) {
	wt.mu.Lock()
	delete(wt.live, c)
	last := len(wt.live) == 0
	wt.mu.Unlock()
	if last {
		wt.say(false)
	}
}

// say tells the hub the newest state; one goroutine sends it, so the
// calls keep their order and a quick close-open sends only what is true
// at the end.
func (wt *Watch) say(on bool) {
	if wt.Watching == nil {
		return
	}
	wt.mu.Lock()
	wt.want = on
	wt.mu.Unlock()
	wt.tellO.Do(func() {
		wt.tell = make(chan struct{}, 1)
		crash.Go("web gateway: tell the hub about watchers", wt.teller)
	})
	select {
	case wt.tell <- struct{}{}:
	default: // a wake is pending; it reads want then
	}
}

// teller sends web.watching when the wanted state differs from the sent
// one. It lives as long as the gateway process.
func (wt *Watch) teller() {
	sent := false
	for range wt.tell {
		wt.mu.Lock()
		on := wt.want
		wt.mu.Unlock()
		if on == sent {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := wt.Watching(ctx, on)
		cancel()
		if err != nil {
			f := false
			_ = wt.Audit.Log(Event{Kind: EvOp, Op: webproto.OpWebWatching, OK: &f, Why: "hub"})
			continue // the next open or close tries again
		}
		sent = on
	}
}

func (wt *Watch) fail(w http.ResponseWriter, code int, why, device string) {
	f := false
	_ = wt.Audit.Log(Event{Kind: EvOp, Op: "watch", Device: device, OK: &f, Why: why, Code: code})
	refuse(w, code, "watch: "+why)
}

func (wt *Watch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/watch" {
		refuse(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		refuse(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if wt.Tokens == nil || wt.Inbox == nil {
		refuse(w, http.StatusNotFound, "not found")
		return
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || r.URL.RawQuery != "" {
		wt.fail(w, http.StatusUnauthorized, "no_token", "")
		return
	}
	dev, ok := wt.Tokens.Check(tok, wt.now())
	if !ok {
		wt.fail(w, http.StatusUnauthorized, "token", "")
		return
	}
	if wt.Store != nil {
		d, ok, err := wt.Store.Get(dev)
		if err != nil || !ok || d.Guest {
			wt.Tokens.RevokeDevice(dev)
			wt.fail(w, http.StatusUnauthorized, "device", dev)
			return
		}
	}
	wake, stop := wt.Inbox.Watch()
	defer stop()
	select {
	case _, open := <-wake:
		if !open {
			wt.fail(w, http.StatusServiceUnavailable, "too_many", dev)
			return
		}
		// a wake that was pending: the first frame has the state anyway
	default:
	}
	c := &watchConn{device: dev, kill: make(chan struct{})}
	wt.add(c)
	defer wt.drop(c)
	t := true
	_ = wt.Audit.Log(Event{Kind: EvOp, Op: "watch", Device: dev, OK: &t, Summary: "open"})
	defer func() { _ = wt.Audit.Log(Event{Kind: EvOp, Op: "watch", Device: dev, OK: &t, Summary: "closed"}) }()

	rc := http.NewResponseController(w)
	_ = rc.SetReadDeadline(time.Time{}) // a long GET: the server's read timeout must not end it
	h := w.Header()
	h.Set("Content-Type", "application/x-ndjson")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	write := func(b []byte) bool {
		_ = rc.SetWriteDeadline(time.Now().Add(watchWrite))
		if _, err := w.Write(append(b, '\n')); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	send := func(f webproto.WatchFrame) bool {
		b, err := json.Marshal(f)
		return err == nil && write(b)
	}
	every, ping := wt.Every, wt.Ping
	if every <= 0 {
		every = webproto.WatchEvery
	}
	if ping <= 0 {
		ping = webproto.WatchPing
	}
	home := wt.Inbox.Home()
	prev := wt.Inbox.Snap()
	if !send(webproto.WatchFrameOf(nil, prev, wt.Inbox.Line(), home)) {
		return
	}
	last := time.Now()
	pingT := time.NewTimer(ping)
	defer pingT.Stop()
	var soonC <-chan time.Time // a coalesced frame after a wake within every
	frame := func() bool {
		if _, ok := wt.Tokens.Check(tok, wt.now()); !ok {
			return false // the token ended or was revoked
		}
		now := wt.Inbox.Snap()
		ok := send(webproto.WatchFrameOf(&prev, now, wt.Inbox.Line(), home))
		prev, last = now, time.Now()
		pingT.Reset(ping)
		return ok
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case <-c.kill:
			return
		case _, open := <-wake:
			if !open {
				return
			}
			if soonC != nil {
				continue // a frame is due already
			}
			if wait := every - time.Since(last); wait > 0 {
				soonC = time.After(wait)
				continue
			}
			if !frame() {
				return
			}
		case <-soonC:
			soonC = nil
			if !frame() {
				return
			}
		case <-pingT.C:
			if !write([]byte(`{"t":"ping"}`)) {
				return
			}
			pingT.Reset(ping)
		}
	}
}
