//go:build unix

package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/relay"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/secrets/secretstest"
)

// Spec (ADR 0022): web_relay is the relay's https address; off by
// default; anything else (plain http beyond loopback, a path) is one
// warning and no relay.
func TestWebSettingsRelay(t *testing.T) {
	withConfigDir(t)
	if c := webSettings(); c.Relay != "" {
		t.Fatalf("relay on by default: %+v", c)
	}
	writeUserConfig(t, `{"web_relay":"https://relay.example.org"}`)
	if c := webSettings(); c.Relay != "https://relay.example.org" || len(c.Problems) != 0 {
		t.Fatalf("relay: %+v", c)
	}
	for _, bad := range []string{"http://relay.example.org", "https://relay.example.org/x", "relay.example.org"} {
		writeUserConfig(t, `{"web_relay":"`+bad+`"}`)
		if c := webSettings(); c.Relay != "" || len(c.Problems) != 1 {
			t.Fatalf("%q: %+v", bad, c)
		}
	}
}

// Spec: the gateway gets --relay <address>, never the key.
func TestWebGatewayArgsRelay(t *testing.T) {
	base := webConfig{IdleMinutes: 15, MaxHours: 12, MaxViewers: 4}
	if args := webGatewayArgs(base, "/w", "/a", "/d", 0); slices.Contains(args, "--relay") {
		t.Fatalf("off: %q", args)
	}
	base.Relay = "https://relay.example.org"
	args := webGatewayArgs(base, "/w", "/a", "/d", 0)
	if i := slices.Index(args, "--relay"); i < 0 || args[i+1] != "https://relay.example.org" {
		t.Fatalf("on: %q", args)
	}
}

// Spec: web_relay is file only; the AI can never set it.
func TestWebRelayIsAINever(t *testing.T) {
	p, ok := aiSettingPolicies["web_relay"]
	if !ok || p.Gate != aiGateNever {
		t.Fatal("the AI may change web_relay")
	}
	found := false
	for _, d := range fileOnlySettings {
		if d.Key == "web_relay" {
			found = strings.Contains(d.Category, "file only")
		}
	}
	if !found {
		t.Fatal("web_relay is not a file-only setting in the docs")
	}
}

func relayMemoryStore(t *testing.T) secrets.Store {
	t.Helper()
	s := secrets.NewStore(secretstest.NewMemory(), secrets.Options{Dir: t.TempDir()})
	old := webRelayStore
	t.Cleanup(func() { webRelayStore = old })
	webRelayStore = func() (secrets.Store, error) { return s, nil }
	return s
}

// Spec: the relay key and room are made once and kept in the secret
// store (web.relay.key), the same on every start.
func TestWebRelayIdentityKeptInTheSecretStore(t *testing.T) {
	s := relayMemoryStore(t)
	a, err := webRelayIdentity()
	if err != nil {
		t.Fatal(err)
	}
	b, err := webRelayIdentity()
	if err != nil || a.Key != b.Key || a.Room != b.Room || !relay.ValidRoom(a.Room) {
		t.Fatalf("second load %+v %v, first %+v", b, err, a)
	}
	v, ok, err := s.Get(webRelayKeyName)
	if err != nil || !ok || !strings.Contains(v, a.Room) {
		t.Fatalf("stored %q %v %v", v, ok, err)
	}
}

// Spec: the gateway's control connection gets the key and room in a
// relay_key event (over the 0600 hub socket); the QR code then shows
// the relay link: <relay>/#r=<room>&k=<public key>&p=<one-time secret>,
// also when the panel itself is on loopback only.
func TestWebPairShowsTheRelayLink(t *testing.T) {
	relayMemoryStore(t)
	r := newAccessRig(t)
	writeUserConfig(t, `{"web_panel":"on","web_relay":"https://relay.example.org"}`)
	r.m.webPanel.url = ""
	if err := r.m.webRelayArm(webSettings()); err != nil {
		t.Fatal(err)
	}
	ev := r.ctlEvent("relay_key")
	id, _ := webRelayIdentity()
	if ev["room"] != id.Room || ev["key"] != base64.RawURLEncoding.EncodeToString(id.Key.Private[:]) {
		t.Fatalf("relay_key event %v", ev)
	}
	r.m.openWebPair(false)
	pair := r.ctlEvent("pair")
	r.m.answerWebPairQR("copy")
	link := <-r.links
	prefix := "https://relay.example.org/#r=" + id.Room + "&k=" + id.Key.Public.String() + "&p="
	if !strings.HasPrefix(link, prefix) {
		t.Fatalf("link %q, want %q…", link, prefix)
	}
	sum := sha256.Sum256([]byte(strings.TrimPrefix(link, prefix)))
	if pair["sha256"] != hex.EncodeToString(sum[:]) {
		t.Fatal("the link's secret is not the one the gateway got")
	}
}

// Spec (NEW-12): Web: Stop Relay stops waiting on the relay while the
// panel keeps running: the gateway gets relay_off, the pairing link is
// the panel's own again, a control connection that comes later gets no
// key, and the AI can never run it. With no relay on it says so.
func TestWebStopRelay(t *testing.T) {
	relayMemoryStore(t)
	r := newAccessRig(t)
	found := false
	for _, it := range webPaletteItems() {
		if it.ID == "web-relay-stop" && it.Title == "Web: Stop Relay" {
			found = true
		}
	}
	if !found {
		t.Fatal("no palette entry Web: Stop Relay")
	}
	if p, ok := aiActionPolicies["web-relay-stop"]; !ok || p.Gate != aiGateNever {
		t.Fatal("the AI may stop the relay")
	}
	r.m.dispatchWebPalette("web-relay-stop")
	if !strings.Contains(webTestToasts(*r.m), "relay is not on") {
		t.Fatalf("no relay: toast %q", webTestToasts(*r.m))
	}
	writeUserConfig(t, `{"web_panel":"on","web_relay":"https://relay.example.org"}`)
	if err := r.m.webRelayArm(webSettings()); err != nil {
		t.Fatal(err)
	}
	r.ctlEvent("relay_key")
	if _, ok := r.m.dispatchWebPalette("web-relay-stop"); !ok {
		t.Fatal("web-relay-stop is not dispatched")
	}
	r.ctlEvent("relay_off")
	if r.m.webPanel.relay != nil {
		t.Error("the pairing link still goes through the relay")
	}
	r.m.web.mu.Lock()
	key := r.m.web.relayKey
	r.m.web.mu.Unlock()
	if key != nil {
		t.Error("a later control connection would still get the relay key")
	}
	if !strings.Contains(webTestToasts(*r.m), "Relay stopped") {
		t.Errorf("toast %q", webTestToasts(*r.m))
	}
}
