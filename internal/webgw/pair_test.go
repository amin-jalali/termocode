//go:build unix

package webgw

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// pairRig is a Pairing with a fake laptop and a fake clock.
type pairRig struct {
	t      *testing.T
	p      *Pairing
	clock  *fakeClock
	store  *webdevices.Store
	tokens *Tokens
	asks   chan webproto.PairAskArgs
	answer chan pairAnswer
	plain  string
}

type pairAnswer struct {
	role string
	err  error
}

func newPairRig(t *testing.T) *pairRig {
	t.Helper()
	r := &pairRig{t: t, clock: &fakeClock{now: time.Unix(1700000000, 0)},
		store: webdevices.Open(filepath.Join(t.TempDir(), "web")), tokens: NewTokens(DefaultIdle, DefaultMax),
		asks: make(chan webproto.PairAskArgs, 4), answer: make(chan pairAnswer, 4)}
	r.p = NewPairing(PairingConfig{Tokens: r.tokens, Store: r.store, Now: r.clock.Now, WaitPoll: 200 * time.Millisecond,
		Ask: func(ctx context.Context, a webproto.PairAskArgs) (string, error) {
			r.asks <- a
			select {
			case ans := <-r.answer:
				return ans.role, ans.err
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}})
	r.open(false)
	return r
}

// open makes a new pairing secret, as "Web: Pair a Device…" does.
func (r *pairRig) open(guest bool) {
	plain, digest, err := NewLinkSecret()
	if err != nil {
		r.t.Fatal(err)
	}
	r.plain = plain
	r.p.SetSecret(digest, PairTTL, guest)
}

func (r *pairRig) post(path, body string) (int, map[string]any) {
	r.t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.p.ServeHTTP(w, req)
	var m map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &m)
	return w.Code, m
}

func (r *pairRig) start(name string) (string, string) {
	r.t.Helper()
	code, m := r.post("/pair/start", `{"p":"`+r.plain+`","name":"`+name+`"}`)
	if code != http.StatusOK {
		r.t.Fatalf("start: %d %v", code, m)
	}
	return m["pair_id"].(string), m["code"].(string)
}

// approve answers the laptop card with role.
func (r *pairRig) approve(role string) webproto.PairAskArgs {
	r.t.Helper()
	select {
	case a := <-r.asks:
		r.answer <- pairAnswer{role: role}
		return a
	case <-time.After(10 * time.Second):
		r.t.Fatal("the laptop was not asked")
	}
	return webproto.PairAskArgs{}
}

func (r *pairRig) waitState(id, want string) map[string]any {
	r.t.Helper()
	for i := 0; i < 100; i++ {
		_, m := r.post("/pair/wait", `{"pair_id":"`+id+`"}`)
		if m["state"] == want || m["error"] == want {
			return m
		}
	}
	r.t.Fatalf("pairing never reached %s", want)
	return nil
}

// Spec (WEB-32): the QR secret works once and for PairTTL only; a second
// phone gets "used", a late one "expired" (T11, T29).
func TestPairSecretSingleUse120s(t *testing.T) {
	r := newPairRig(t)
	r.start("iPhone · Safari")
	if code, m := r.post("/pair/start", `{"p":"`+r.plain+`","name":"Other"}`); code != http.StatusUnauthorized || m["error"] != "used" {
		t.Fatalf("second use: %d %v", code, m)
	}
	r.open(false)
	r.clock.Add(PairTTL)
	if code, m := r.post("/pair/start", `{"p":"`+r.plain+`","name":"Late"}`); code != http.StatusUnauthorized || m["error"] != "expired" {
		t.Fatalf("expired: %d %v", code, m)
	}
	if PairTTL != 120*time.Second {
		t.Fatalf("PairTTL %v", PairTTL)
	}
}

// Five wrong secrets burn the pairing link (T13).
func TestPairWrongSecretBurns(t *testing.T) {
	r := newPairRig(t)
	for i := 0; i < MaxBadTries; i++ {
		r.post("/pair/start", `{"p":"`+strings.Repeat("A", 43)+`","name":"x"}`)
	}
	if code, m := r.post("/pair/start", `{"p":"`+r.plain+`","name":"x"}`); code != http.StatusUnauthorized || m["error"] != "pair_failed" {
		t.Fatalf("after burn: %d %v", code, m)
	}
}

// Spec (WEB-32): the phone and the laptop card show the same 6-digit
// code; a second phone's request gets another code.
func TestSASCodeMatches(t *testing.T) {
	r := newPairRig(t)
	id, code := r.start("iPhone · Safari")
	a := r.approve(webdevices.RoleViewer)
	if a.Code != code || a.PairID != id || a.Name != "iPhone · Safari" || len(code) != 6 {
		t.Fatalf("phone %s/%s, card %+v", id, code, a)
	}
	r.open(false)
	_, code2 := r.start("Other")
	r2 := 0
	for code2 == code && r2 < 3 { // 1 in a million: try again
		r.open(false)
		_, code2 = r.start("Other")
		r2++
	}
	if code2 == code {
		t.Fatal("two pairings got the same code")
	}
}

// Spec (WEB-32): only the owner's yes on the laptop pairs: Not now is
// denied, and the phone never gets a token; yes stores the device with
// the role the owner picked and logs that browser in with it.
func TestPairNeedsOwnerConfirm(t *testing.T) {
	r := newPairRig(t)
	id, _ := r.start("Phone")
	select {
	case <-r.asks:
		r.answer <- pairAnswer{err: &CtlError{Code: webproto.CodeForbidden, Msg: "Not now"}}
	case <-time.After(10 * time.Second):
		t.Fatal("not asked")
	}
	r.waitState(id, "denied")
	if code, m := r.post("/pair/finish", `{"pair_id":"`+id+`"}`); code == http.StatusOK || m["token"] != nil {
		t.Fatalf("finish after deny: %d %v", code, m)
	}
	if ds, _ := r.store.List(); len(ds) != 0 {
		t.Fatalf("a denied device was stored: %v", ds)
	}

	r.open(false)
	id, _ = r.start("Phone")
	r.approve(webdevices.RoleViewer)
	m := r.waitState(id, "approved")
	if m["role"] != "viewer" {
		t.Fatalf("approved: %v", m)
	}
	code, m := r.post("/pair/finish", `{"pair_id":"`+id+`"}`)
	if code != http.StatusOK {
		t.Fatalf("finish: %d %v", code, m)
	}
	sess, err := r.tokens.Check(m["token"].(string), r.clock.Now())
	if err != nil || sess.Role != RoleViewer || !strings.HasPrefix(sess.Device, "dev-") {
		t.Fatalf("session %+v %v", sess, err)
	}
	d, ok, _ := r.store.Get(sess.Device)
	if !ok || d.Role != webdevices.RoleViewer || d.Name != "Phone" {
		t.Fatalf("device %+v %v", d, ok)
	}
	// Replay: the same finish again gets nothing.
	if code, m := r.post("/pair/finish", `{"pair_id":"`+id+`"}`); code == http.StatusOK || m["token"] != nil {
		t.Fatalf("finish replay: %d %v", code, m)
	}
}

// A share link (WEB-50) pairs a viewer, even if the answer said control.
func TestPairGuestIsViewer(t *testing.T) {
	r := newPairRig(t)
	r.open(true)
	id, _ := r.start("Sara")
	a := r.approve(webdevices.RoleControl)
	if !a.Guest {
		t.Fatal("the card did not say guest")
	}
	r.waitState(id, "approved")
	_, m := r.post("/pair/finish", `{"pair_id":"`+id+`"}`)
	sess, err := r.tokens.Check(m["token"].(string), r.clock.Now())
	if err != nil || sess.Role != RoleViewer {
		t.Fatalf("guest session %+v %v", sess, err)
	}
	d, _, _ := r.store.Get(sess.Device)
	if !d.Guest || d.Role != webdevices.RoleViewer {
		t.Fatalf("guest device %+v", d)
	}
}

// An approval that comes after the pairing expired pairs nothing.
func TestPairExpiredPending(t *testing.T) {
	r := newPairRig(t)
	id, _ := r.start("Phone")
	r.clock.Add(PairTTL + time.Second)
	r.approve(webdevices.RoleControl)
	if code, m := r.post("/pair/finish", `{"pair_id":"`+id+`"}`); code != http.StatusGone {
		t.Fatalf("finish after expiry: %d %v", code, m)
	}
}

// Pairing endpoints refuse a query, a form, unknown fields, and a flood.
func TestPairRequestRules(t *testing.T) {
	r := newPairRig(t)
	for _, tc := range []struct {
		path, ctype, body string
		code              int
	}{
		{"/pair/start?p=x", "application/json", `{"p":"x","name":"a"}`, http.StatusBadRequest},
		{"/pair/start", "application/x-www-form-urlencoded", `p=x`, http.StatusUnsupportedMediaType},
		{"/pair/start", "application/json", `{"p":"x","name":"a","role":"control"}`, http.StatusBadRequest},
		{"/pair/start", "application/json", `{"p":"x"}{}`, http.StatusBadRequest},
		{"/pair/nope", "application/json", `{}`, http.StatusNotFound},
		{"/pair/wait", "application/json", `{"pair_id":"unknown"}`, http.StatusGone},
	} {
		req := httptest.NewRequest(http.MethodPost, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", tc.ctype)
		w := httptest.NewRecorder()
		r.p.ServeHTTP(w, req)
		if w.Code != tc.code {
			t.Errorf("%s %s: %d, want %d", tc.path, tc.body, w.Code, tc.code)
		}
	}
	limited := false
	for i := 0; i < 100 && !limited; i++ {
		code, _ := r.post("/pair/wait", `{"pair_id":"x"}`)
		limited = code == http.StatusTooManyRequests
	}
	if !limited {
		t.Fatal("no rate limit on /pair")
	}
}

func FuzzPairStart(f *testing.F) {
	f.Add(`{"p":"abc","name":"Phone"}`)
	f.Add(`{"pair_id":"` + strings.Repeat("a", 32) + `"}`)
	f.Add(`{"p":null}`)
	f.Fuzz(func(t *testing.T, body string) {
		r := newPairRig(t)
		for _, path := range []string{"/pair/start", "/pair/wait", "/pair/finish"} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.p.ServeHTTP(w, req)
			if w.Code == http.StatusOK && path != "/pair/wait" {
				t.Fatalf("%s took %q", path, body)
			}
		}
	})
}

// Spec (WEB-50): the page of a share link learns it is a guest pairing,
// so it tells the person to read the code to whoever shared the link.
func TestPairStartSaysGuest(t *testing.T) {
	r := newPairRig(t)
	r.open(true)
	code, m := r.post("/pair/start", `{"p":"`+r.plain+`","name":"Sara"}`)
	if code != http.StatusOK || m["guest"] != true {
		t.Fatalf("start: %d %v", code, m)
	}
	r.open(false)
	if _, m := r.post("/pair/start", `{"p":"`+r.plain+`","name":"Me"}`); m["guest"] == true {
		t.Fatal("a plain pairing says guest")
	}
}
