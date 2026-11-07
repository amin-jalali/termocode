package app

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/secrets/secretstest"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webpush"
)

// webPushRig swaps the store, the device list and the sender for a
// memory store, a fixed list and a local TLS push service.
type webPushRig struct {
	store    secrets.Store
	devs     []webdevices.Device
	mu       sync.Mutex
	hits     map[string]int // path → pushes
	code     int
	endpoint string
}

func newWebPushRig(t *testing.T) *webPushRig {
	t.Helper()
	r := &webPushRig{store: secrets.NewStore(secretstest.NewMemory(), secrets.Options{Dir: t.TempDir()}), hits: map[string]int{}, code: http.StatusCreated}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		r.hits[req.URL.Path]++
		code := r.code
		r.mu.Unlock()
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	oldStore, oldDevs, oldSender := webPushStore, webPushDevices, webPushSender
	t.Cleanup(func() { webPushStore, webPushDevices, webPushSender = oldStore, oldDevs, oldSender })
	webPushStore = func() (secrets.Store, error) { return r.store, nil }
	webPushDevices = func() ([]webdevices.Device, error) { return r.devs, nil }
	webPushSender = func(k *webpush.Key) *webpush.Sender {
		return &webpush.Sender{Key: k, Subject: webPushSubject, Client: srv.Client(), Hosts: []string{u.Host}}
	}
	r.endpoint = srv.URL
	return r
}

func (r *webPushRig) subscribe(t *testing.T, dev string) {
	t.Helper()
	k, _ := ecdh.P256().GenerateKey(rand.Reader)
	b64 := base64.RawURLEncoding
	sub := webpush.Sub{Endpoint: r.endpoint + "/" + dev, P256dh: b64.EncodeToString(k.PublicKey().Bytes()), Auth: b64.EncodeToString(make([]byte, 16))}
	b, _ := json.Marshal(sub)
	if err := r.store.Set(webPushSubPrefix+dev, string(b)); err != nil {
		t.Fatal(err)
	}
}

func (r *webPushRig) pushes(dev string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.hits["/"+dev]
}

// Spec: a push goes when more things wait than at the last check, at
// most once per cooldown (a held push goes when it ends), never while
// one is being sent, and not again while the count stays or falls.
func TestWebPushDecide(t *testing.T) {
	var p webPushState
	t0 := time.Unix(1_800_000_000, 0)
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	if send, _ := p.decide(0, t0); send {
		t.Fatal("nothing waits: a push")
	}
	if send, _ := p.decide(1, t0); !send {
		t.Fatal("a new card: no push")
	}
	if send, wait := p.decide(2, at(time.Second)); send || wait <= 0 {
		t.Fatalf("while sending: send %v wait %v", send, wait)
	}
	p.sending = false
	if send, _ := p.decide(1, at(time.Minute)); send {
		t.Fatal("the same count pushed again")
	}
	if send, _ := p.decide(2, at(61*time.Second)); !send {
		t.Fatal("one more card after the cooldown: no push")
	}
	p.sending = false
	send, wait := p.decide(3, at(62*time.Second))
	if send || wait <= 0 || wait > webPushCooldown {
		t.Fatalf("inside the cooldown: send %v wait %v", send, wait)
	}
	if send, _ := p.decide(3, at(62*time.Second+wait)); !send {
		t.Fatal("the held push did not go when the cooldown ended")
	}
	p.sending = false
	if send, _ := p.decide(0, at(5*time.Minute)); send || p.lastN != 0 {
		t.Fatalf("all answered: send %v lastN %d", send, p.lastN)
	}
	if send, _ := p.decide(1, at(6*time.Minute)); !send {
		t.Fatal("a new card after all were answered: no push")
	}
}

// Spec: the VAPID key is made once, kept in the secret store, and only
// its public half leaves; a locked store makes and sends nothing.
func TestWebPushKeyLivesInTheSecretStore(t *testing.T) {
	r := newWebPushRig(t)
	if k, st := webPushKey(false); k != nil || st != webPushNone {
		t.Fatalf("no key yet: %v %q", k, st)
	}
	k, st := webPushKey(true)
	if k == nil || st != webPushReady {
		t.Fatalf("make: %v %q", k, st)
	}
	v, ok, _ := r.store.Get(webPushKeyName)
	if !ok || v != k.Private() {
		t.Fatal("the key is not in the secret store")
	}
	k2, _ := webPushKey(true)
	if k2.Public() != k.Public() {
		t.Error("a second key was made")
	}
	locked := secrets.NewStore(secretstest.NewLockedMemory("pw"), secrets.Options{Dir: t.TempDir()})
	webPushStore = func() (secrets.Store, error) { return locked, nil }
	if k, st := webPushKey(true); k != nil || st != webPushLocked {
		t.Errorf("locked store: %v %q", k, st)
	}
}

// Spec: only a paired device that is not a shared view gets pushes.
func TestWebPushDevice(t *testing.T) {
	r := newWebPushRig(t)
	r.devs = []webdevices.Device{{ID: "dev-0000000a"}, {ID: "dev-0000000b", Guest: true}}
	for dev, want := range map[string]bool{"dev-0000000a": true, "dev-0000000b": false, "dev-0000000c": false, "loc-ab12": false, "": false, "dev-../x": false} {
		if got := webPushDevice(dev); got != want {
			t.Errorf("%q: %v, want %v", dev, got, want)
		}
	}
}

// Spec: a push goes to each subscribed phone without a page open; a
// phone with the page open gets none; a dead subscription and one of a
// removed device are forgotten.
func TestWebPushSendAll(t *testing.T) {
	r := newWebPushRig(t)
	webPushKey(true)
	r.devs = []webdevices.Device{{ID: "dev-0000000a"}, {ID: "dev-0000000b"}}
	r.subscribe(t, "dev-0000000a")
	r.subscribe(t, "dev-0000000b")
	r.subscribe(t, "dev-0000000c") // removed from the paired list
	left := webPushSendAll(context.Background(), webpush.Payload(webpush.DetailCount, 2, ""), map[string]bool{"dev-0000000b": true})
	if !left {
		t.Fatal("no subscription left")
	}
	if r.pushes("dev-0000000a") != 1 || r.pushes("dev-0000000b") != 0 || r.pushes("dev-0000000c") != 0 {
		t.Fatalf("pushes %v", r.hits)
	}
	if webPushHasSub(r.store, "dev-0000000c") {
		t.Error("a removed device keeps its subscription")
	}
	r.code = http.StatusGone
	webPushSendAll(context.Background(), nil, map[string]bool{"dev-0000000b": true})
	if webPushHasSub(r.store, "dev-0000000a") || !webPushHasSub(r.store, "dev-0000000b") {
		t.Error("the dead subscription is kept, or the live one was dropped")
	}
}

// Spec: with no subscription the check costs nothing and sends nothing;
// with one, a new card while no page is open sends one push.
func TestWebPushAfterUpdate(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	if cmd := m.webPushAfterUpdate(nil); cmd != nil {
		t.Fatal("no subscription: a Cmd")
	}
	m.web.push.any = true
	if cmd := m.webPushAfterUpdate(nil); cmd != nil {
		t.Fatal("nothing waits: a Cmd")
	}
	if cmd := m.webPushAfterUpdate(nil); cmd == nil || !m.web.push.tick {
		t.Fatal("a check inside a second is not brought back by a tick")
	}
	if cmd := m.webPushAfterUpdate(webPushTickMsg{}); m.web.push.tick && cmd == nil {
		t.Fatal("the tick was not taken")
	}
}
