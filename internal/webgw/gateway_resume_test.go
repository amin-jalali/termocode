//go:build unix

package webgw

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// authToken signs in with the launch secret and returns the session token.
func (r *gwRig) authToken() string {
	r.t.Helper()
	origin := "http://127.0.0.1:" + strconv.Itoa(r.port)
	req, _ := http.NewRequest(http.MethodPost, origin+"/auth", strings.NewReader(`{"secret":"`+r.secret+`"}`))
	req.Header.Set("Origin", origin)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	var got struct {
		Token string `json:"token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != http.StatusOK || got.Token == "" {
		r.t.Fatalf("auth: %d", resp.StatusCode)
	}
	return got.Token
}

// dialWS opens the WebSocket with tok and says hello; the hub peer is
// returned once the open line and the hello reached it.
func (r *gwRig) dialWS(tok string) (*websocket.Conn, *hubPeer) {
	r.t.Helper()
	origin := "http://127.0.0.1:" + strconv.Itoa(r.port)
	h := http.Header{}
	h.Set("Origin", origin)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws://127.0.0.1:"+strconv.Itoa(r.port)+"/ws",
		&websocket.DialOptions{HTTPHeader: h, Subprotocols: []string{Subprotocol}})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.CloseNow() })
	writeText(r.t, c, helloMsg(tok))
	p := r.hub.next()
	p.line(r.t) // open
	p.line(r.t) // hello
	return c, p
}

// Spec (reload on a new build): the hub's bye 1012 is a restart, not the
// kill switch: the gateway writes its browser sessions (token digests,
// never a token) and its port to the 0600 resume file, closes every
// browser with 1012 (the page retries) and stops. A gateway started with
// that file on the same port lets the same browser in with its old
// token: no new link, no new pairing.
func TestGatewayRestartHandsOverTheSessions(t *testing.T) {
	resume := filepath.Join(t.TempDir(), "web-resume.json")
	r := startedGateway(t, func(c *RunConfig) { c.ResumePath = resume })
	tok := r.authToken()
	c, _ := r.dialWS(tok)
	r.ctl.send(t, `{"t":"bye","code":1012}`)
	if got := closeCode(t, c); got != websocket.StatusCode(webproto.CloseRestarting) {
		t.Fatalf("browser closed with %d, want 1012", got)
	}
	if code := r.wait(); code != webproto.CloseRestarting {
		t.Fatalf("stopped with %d, want 1012", code)
	}
	fi, err := os.Stat(resume)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("resume file %v %v", fi, err)
	}
	raw, _ := os.ReadFile(resume)
	if strings.Contains(string(raw), tok) || strings.Contains(string(raw), r.secret) {
		t.Fatal("a token or the secret is in the resume file")
	}
	if a := auditText(t, r.cfg.AuditPath); !strings.Contains(a, `"why":"restart"`) {
		t.Errorf("the restart is not in the audit log:\n%s", a)
	}
	res, err := ReadResume(resume, time.Now())
	if err != nil || res.Port != r.port || len(res.Tokens) != 1 {
		t.Fatalf("ReadResume = %+v, %v", res, err)
	}
	if _, err := os.Stat(resume); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the resume file is still there after it was read: %v", err)
	}

	r2 := startedGateway(t, func(c *RunConfig) { c.Port, c.Resume = res.Port, &res })
	if r2.port != r.port {
		t.Fatalf("the next gateway serves on %d, want the same port %d", r2.port, r.port)
	}
	c2, p2 := r2.dialWS(tok) // the old token: the page's reconnect
	_ = p2
	_ = c2
	st, err := ReadStatus(r2.cfg.StatusPath)
	if err != nil || !st.Started.Equal(res.Started) {
		t.Errorf("the next gateway's start %v, want the first one's %v (Max counts on): %v", st.Started, res.Started, err)
	}
}

// Spec: a bye other than 1012 writes no resume file, and an old token
// is unknown to a gateway started without one.
func TestGatewayKillSwitchWritesNoResume(t *testing.T) {
	resume := filepath.Join(t.TempDir(), "web-resume.json")
	r := startedGateway(t, func(c *RunConfig) { c.ResumePath = resume })
	c, _ := r.browser()
	r.ctl.send(t, `{"t":"bye","code":4009}`)
	closeCode(t, c)
	r.wait()
	if _, err := os.Stat(resume); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the kill switch wrote a resume file: %v", err)
	}
}

// Spec: a resume file is good once and only for a short time: an old
// one (a reload that never came back) is refused and removed.
func TestReadResumeRefusesAStaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "r.json")
	now := time.Now()
	if err := WriteResume(path, Resume{At: now.Add(-ResumeTTL - time.Second), Port: 5000}); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadResume(path, now); err == nil {
		t.Fatal("a stale resume file was read")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the stale resume file was kept")
	}
	if _, err := ReadResume(path, now); err == nil {
		t.Fatal("a missing resume file was read")
	}
}

// Spec: Export gives the live sessions by digest; Import into a new
// Tokens makes the same token work, with its device, role and times.
// An expired session is not handed over.
func TestTokensExportImport(t *testing.T) {
	now := time.Now()
	a := NewTokens(time.Hour, 12*time.Hour)
	tok, s, err := a.Issue("phone", RoleControl, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	old, _, _ := a.Issue("old", RoleViewer, now.Add(-2*time.Hour)) // idle too long
	recs := a.Export(now)
	if len(recs) != 1 {
		t.Fatalf("exported %d sessions, want 1", len(recs))
	}
	b := NewTokens(time.Hour, 12*time.Hour)
	if n := b.Import(recs, now); n != 1 {
		t.Fatalf("imported %d", n)
	}
	got, err := b.Check(tok, now)
	if err != nil || got.ID != s.ID || got.Device != "phone" || got.Role != RoleControl || !got.Created.Equal(s.Created) {
		t.Fatalf("Check after import = %+v, %v; want %+v", got, err, s)
	}
	if _, err := b.Check(old, now); err == nil {
		t.Fatal("an expired session came over")
	}
	if n := b.Import([]TokenRecord{{Hash: "zz"}}, now); n != 0 {
		t.Fatalf("a bad digest was imported")
	}
}
