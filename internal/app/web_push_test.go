package app

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/secrets/secretstest"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/webpush"
)

// webPushRig swaps the store, the device list and the sender for a
// memory store, a fixed list and a local TLS push service.
type webPushRig struct {
	store    secrets.Store
	devs     []webdevices.Device
	mu       sync.Mutex
	hits     map[string]int    // path → pushes
	bodies   map[string][]byte // path → the last body
	keys     map[string]*ecdh.PrivateKey
	code     int
	endpoint string
}

func newWebPushRig(t *testing.T) *webPushRig {
	t.Helper()
	r := &webPushRig{store: secrets.NewStore(secretstest.NewMemory(), secrets.Options{Dir: t.TempDir()}), hits: map[string]int{}, bodies: map[string][]byte{}, keys: map[string]*ecdh.PrivateKey{}, code: http.StatusCreated}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.hits[req.URL.Path]++
		r.bodies[req.URL.Path] = body
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
	r.keys[dev] = k
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

// payload decrypts the last push to dev as the phone would (RFC 8291,
// auth secret all zero in this rig).
func (r *webPushRig) payload(t *testing.T, dev string) string {
	t.Helper()
	r.mu.Lock()
	body, k := r.bodies["/"+dev], r.keys[dev]
	r.mu.Unlock()
	if len(body) < 21+65+16 || k == nil {
		t.Fatalf("no push body for %s (%d bytes)", dev, len(body))
	}
	salt, idlen := body[:16], int(body[20])
	asPub, err := ecdh.P256().NewPublicKey(body[21 : 21+idlen])
	if err != nil {
		t.Fatal(err)
	}
	shared, err := k.ECDH(asPub)
	if err != nil {
		t.Fatal(err)
	}
	info := append(append([]byte("WebPush: info\x00"), k.PublicKey().Bytes()...), asPub.Bytes()...)
	ikm, _ := hkdf.Key(sha256.New, shared, make([]byte, 16), string(info), 32)
	cek, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	blk, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(blk)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	plain = bytes.TrimRight(plain, "\x00")
	return string(plain[:len(plain)-1])
}

// Spec (CMP-47): when an AI run ends or fails and the phone has no page
// open, it gets one push that says so (the phone decrypts it); the
// session name is in it only with web_push_detail title.
func TestWebPushWhenARunEnds(t *testing.T) {
	withConfigDir(t)
	r := newWebPushRig(t)
	webPushKey(true)
	r.devs = []webdevices.Device{{ID: "dev-0000000a"}}
	r.subscribe(t, "dev-0000000a")
	m := aiTestModel(t)
	webTestHub(t, &m)
	m.web.push.any = true
	m.web.attention(webproto.AttentionRunDone, "run-1")
	cmd := m.webPushAfterUpdate(nil)
	if cmd == nil {
		t.Fatal("a finished run sends no push")
	}
	cmd()
	if got := r.payload(t, "dev-0000000a"); got != `{"n":0,"k":"done"}` {
		t.Errorf("count: %s", got)
	}
	// A second end inside the cooldown is held, then sent; title names the session.
	writeUserConfig(t, `{"web_push_detail":"title"}`)
	m.web.push.sent, m.web.push.checked, m.web.push.sending = time.Time{}, time.Time{}, false
	m.web.attention(webproto.AttentionRunFailed, "run-2")
	cmd = m.webPushAfterUpdate(nil)
	if cmd == nil {
		t.Fatal("a failed run sends no push")
	}
	cmd()
	got := r.payload(t, "dev-0000000a")
	if !strings.HasPrefix(got, `{"n":0,"k":"failed","s":"`) {
		t.Errorf("title: %s", got)
	}
	// One end, one push: nothing more goes without a new end.
	m.web.push.sent, m.web.push.checked = time.Time{}, time.Time{}
	if cmd := m.webPushAfterUpdate(nil); cmd != nil {
		t.Error("a push went with no new end")
	}
}

// Spec (CMP-47): web_push "off" sends nothing, for cards and for ends;
// an end seen while it was off is not sent when it is turned on again.
func TestWebPushSettingOff(t *testing.T) {
	withConfigDir(t)
	c := webSettings()
	if !c.Push {
		t.Fatal("web_push is not on by default")
	}
	writeUserConfig(t, `{"web_push":"off"}`)
	if webSettings().Push {
		t.Fatal("web_push off is on")
	}
	writeUserConfig(t, `{"web_push":"maybe"}`)
	if c := webSettings(); !c.Push || len(c.Problems) != 1 {
		t.Fatalf("bad value: %+v", c)
	}
	r := newWebPushRig(t)
	webPushKey(true)
	r.devs = []webdevices.Device{{ID: "dev-0000000a"}}
	r.subscribe(t, "dev-0000000a")
	m := aiTestModel(t)
	webTestHub(t, &m)
	writeUserConfig(t, `{"web_push":"off"}`) // after the model: it sets its own config dir
	m.web.push.any = true
	m.web.attention(webproto.AttentionRunDone, "run-1")
	if cmd := m.webPushAfterUpdate(nil); cmd != nil {
		t.Fatal("web_push off sent a push")
	}
	writeUserConfig(t, `{}`)
	m.web.push.checked = time.Time{}
	if cmd := m.webPushAfterUpdate(nil); cmd != nil {
		t.Error("an end from while it was off is sent now")
	}
}
