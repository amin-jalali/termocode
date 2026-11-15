//go:build unix

package webgw

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// auditEvents reads the rig's audit log as events.
func auditEvents(t *testing.T, path string) []Event {
	t.Helper()
	var out []Event
	for _, l := range strings.Split(strings.TrimSpace(auditText(t, path)), "\n") {
		if l == "" {
			continue
		}
		var e Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("audit line %q: %v", l, err)
		}
		out = append(out, e)
	}
	return out
}

func rejects(t *testing.T, path string) []Event {
	t.Helper()
	var out []Event
	for _, e := range auditEvents(t, path) {
		if e.Kind == EvReject {
			out = append(out, e)
		}
	}
	return out
}

// A /ws attempt the gateway refuses before the upgrade leaves an audit
// line that says why (ev "reject"): after a kill switch and a new start
// the gateway has no session, and the page that still holds the old
// token must show up in the log, not vanish.
func TestWSRejectBeforeUpgradeIsAudited(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	if _, resp, err := r.dial(r.dialOpts()); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no session: %v %v, want 401", err, resp)
	}
	r.token()
	opts := r.dialOpts()
	opts.Subprotocols = nil
	if _, resp, err := r.dial(opts); err == nil || resp == nil || resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("no subprotocol: %v %v, want 400", err, resp)
	}
	r.ws.CloseAll(webproto.CloseStopped, time.Second)
	if _, resp, err := r.dial(r.dialOpts()); err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("stopping: %v %v, want 503", err, resp)
	}
	got := rejects(t, r.alog)
	want := []struct {
		why  string
		code int
	}{{"signed_out", 401}, {"subprotocol", 400}, {"stopping", 503}}
	if len(got) != len(want) {
		t.Fatalf("reject lines %+v, want %d", got, len(want))
	}
	for i, w := range want {
		if got[i].Why != w.why || got[i].Code != w.code {
			t.Errorf("reject %d = %q %d, want %q %d", i, got[i].Why, got[i].Code, w.why, w.code)
		}
	}
}

// A page that retries without end cannot flood the log: reject lines
// pass a rate limit (a burst, then a few a second).
func TestWSRejectAuditIsRateLimited(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	for i := 0; i < 3*blockedAuditBurst; i++ {
		if _, resp, err := r.dial(r.dialOpts()); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("try %d: %v %v, want 401", i, err, resp)
		}
	}
	if n := len(rejects(t, r.alog)); n == 0 || n > blockedAuditBurst+blockedAuditPerSecond*2 {
		t.Fatalf("%d reject lines for %d tries, want 1..%d", n, 3*blockedAuditBurst, blockedAuditBurst+blockedAuditPerSecond*2)
	}
}

// postCheck asks POST /ws/check about tok, as the page does after its
// WebSocket was refused before it opened.
func postCheck(t *testing.T, r *wsRig, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/ws/check", strings.NewReader(body))
	req.Host = r.addr
	req.Header.Set("Origin", "http://"+r.addr)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ws.ServeHTTP(rec, req)
	var m map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &m)
	return rec.Code, m
}

func checkBody(tok string) string {
	b, _ := json.Marshal(map[string]string{"token": tok})
	return string(b)
}

// The page cannot read the status of a refused WebSocket (the browser
// reports 1006, as for a dropped network). It asks /ws/check instead:
// a sign-in that is over (no such session, expired, the device
// removed) is a 401 with a reason the page turns into its sign-in; a
// live one is 200, so the page goes on retrying. The answer never
// marks the session used, and no token lands in the log.
func TestWSCheckTellsSignedOutFromLive(t *testing.T) {
	devs := webdevices.Open(t.TempDir())
	r := newWSRig(t, DefaultLimits, func(c *WSConfig) { c.Devices = devs })
	const stale = "stale-token-0123456789abcdefghijklmnopqrstuv"
	if code, m := postCheck(t, r, checkBody(stale)); code != http.StatusUnauthorized || m["error"] != "signed_out" {
		t.Fatalf("no session: %d %v, want 401 signed_out", code, m)
	}
	live := r.token()
	before, _ := r.tokens.Check(live, r.clock.Now())
	r.clock.Add(time.Minute)
	if code, m := postCheck(t, r, checkBody(live)); code != http.StatusOK || m["ok"] != true {
		t.Fatalf("live: %d %v, want 200 ok", code, m)
	}
	if s, _ := r.tokens.Peek(live, r.clock.Now()); !s.LastSeen.Equal(before.LastSeen) {
		t.Errorf("the check marked the session used: %v, was %v", s.LastSeen, before.LastSeen)
	}
	if code, m := postCheck(t, r, checkBody(stale)); code != http.StatusUnauthorized || m["error"] != "signed_out" {
		t.Fatalf("unknown token: %d %v, want 401 signed_out", code, m)
	}
	gone, _, err := r.tokens.Issue("dev-0a0b", RoleControl, r.clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if code, m := postCheck(t, r, checkBody(gone)); code != http.StatusUnauthorized || m["error"] != "revoked" {
		t.Fatalf("removed device: %d %v, want 401 revoked", code, m)
	}
	r.clock.Add(DefaultIdle + time.Minute)
	if code, m := postCheck(t, r, checkBody(live)); code != http.StatusUnauthorized || m["error"] != "expired" {
		t.Fatalf("idle too long: %d %v, want 401 expired", code, m)
	}
	for name, body := range map[string]string{"no token": `{}`, "extra field": `{"token":"a","x":1}`, "not json": `token=a`} {
		if code, _ := postCheck(t, r, body); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/ws/check", nil)
	rec := httptest.NewRecorder()
	r.ws.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /ws/check: %d, want 405", rec.Code)
	}
	for _, s := range []string{stale, live, gone} {
		if strings.Contains(auditText(t, r.alog), s) {
			t.Errorf("a token is in the audit log")
		}
	}
}

// /ws/check is rate-limited like /auth: a burst passes, then 429.
func TestWSCheckIsRateLimited(t *testing.T) {
	r := newWSRig(t, DefaultLimits, nil)
	limited := 0
	for i := 0; i < 3*AuthRatePerSecond; i++ {
		if code, _ := postCheck(t, r, checkBody("x")); code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatalf("no 429 in %d checks", 3*AuthRatePerSecond)
	}
}

// The site routes /ws/check to the WebSocket endpoint.
func TestSiteMuxRoutesWSCheck(t *testing.T) {
	mark := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ws")) })
	mux := siteMux(LoopbackSite(41237), http.NotFoundHandler(), mark, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/ws/check", nil))
	if rec.Body.String() != "ws" {
		t.Errorf("/ws/check went to %q", rec.Body.String())
	}
}
