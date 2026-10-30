//go:build unix

package webgw

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func openAudit(t *testing.T, path string) *Audit {
	t.Helper()
	a, err := OpenAudit(path, (&fakeClock{now: t0}).Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// readAudit decodes every line of the audit file.
func readAudit(t *testing.T, path string) []Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var evs []Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Event
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		evs = append(evs, e)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return evs
}

// TestAuditMode0600: the log is private to the user (T37).
func TestAuditMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.audit.jsonl")
	a := openAudit(t, path)
	if err := a.Log(Event{Kind: EvStart}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", fi.Mode().Perm())
	}
}

// TestAuditTightensLooseMode: a log left world-readable is made 0600.
func TestAuditTightensLooseMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	openAudit(t, path)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", fi.Mode().Perm())
	}
}

// TestAuditRefusesSymlink: a planted symlink cannot point the log at
// another file.
func TestAuditRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "a.jsonl")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if a, err := OpenAudit(link, nil); err == nil {
		a.Close()
		t.Fatal("opened the log through a symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "keep\n" {
		t.Fatalf("target changed: %q", b)
	}
}

func TestAuditAppendsJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	a := openAudit(t, path)
	ok := true
	if err := a.Log(Event{Kind: EvAuthOK, Device: "loc-1a2b", Conn: "s-01"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Log(Event{Kind: EvOp, Conn: "s-01", Op: "git.commit", OK: &ok, Summary: "3 files"}); err != nil {
		t.Fatal(err)
	}
	a.Close()
	// Reopen: new lines go after the old ones.
	a2 := openAudit(t, path)
	if err := a2.Log(Event{Kind: EvStop}); err != nil {
		t.Fatal(err)
	}
	evs := readAudit(t, path)
	if len(evs) != 3 || evs[0].Kind != EvAuthOK || evs[1].Op != "git.commit" || evs[2].Kind != EvStop {
		t.Fatalf("events %+v", evs)
	}
	if !evs[0].T.Equal(t0) || evs[1].OK == nil || !*evs[1].OK || evs[1].Summary != "3 files" {
		t.Fatalf("fields %+v", evs[1])
	}
}

// TestAuditRotates: past the size cap the log moves to .1 (and .1 to .2);
// two old files are kept, all 0600, none over the cap (T37).
func TestAuditRotates(t *testing.T) {
	if AuditMaxBytes != 5<<20 || AuditKeep != 2 {
		t.Fatalf("AuditMaxBytes %d AuditKeep %d, want 5 MiB and 2", AuditMaxBytes, AuditKeep)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "a.jsonl")
	a := openAudit(t, path)
	a.max = 400
	for i := 0; i < 40; i++ {
		if err := a.Log(Event{Kind: EvOp, Op: "tree.list", Code: i}); err != nil {
			t.Fatal(err)
		}
	}
	last := -1
	for _, p := range []string{path + ".2", path + ".1", path} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != 0o600 || fi.Size() > 400 || fi.Size() == 0 {
			t.Fatalf("%s: mode %v size %d", filepath.Base(p), fi.Mode().Perm(), fi.Size())
		}
		for _, e := range readAudit(t, p) {
			if e.Code <= last {
				t.Fatalf("%s: event %d after %d", filepath.Base(p), e.Code, last)
			}
			last = e.Code
		}
	}
	if last != 39 {
		t.Fatalf("newest event %d, want 39", last)
	}
	if _, err := os.Stat(path + ".3"); !os.IsNotExist(err) {
		t.Fatalf("a third old file exists: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("%d files in the log folder, want 3", len(entries))
	}
}

// TestNoSecretInAuditSummary: the summary is built from policy (a path, a
// label, a count), but the log cleans it again: any long token-like run is
// masked, control and bidi characters go, and it is capped (T8).
func TestNoSecretInAuditSummary(t *testing.T) {
	launch, _, err := NewLaunchSecret(t0)
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{
		launch,
		"sk-" + "test" + "-0123456789abcdefghijklmnopqrstuvwxyz",
		"ghp_" + "0123456789abcdefABCDEF0123456789abcd",
		strings.Repeat("0123456789abcdef", 4),
		"QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo0123456789+/==",
	}
	path := filepath.Join(t.TempDir(), "a.jsonl")
	a := openAudit(t, path)
	for _, s := range secrets {
		for _, ev := range []Event{
			{Kind: EvOp, Op: "buf.save", Summary: "saved src/main.go with " + s},
			{Kind: EvOp, Op: "term.send", Summary: s},
			{Kind: EvAuthFail, Why: "wrong " + s, Conn: s, Device: s, Op: s},
		} {
			if err := a.Log(ev); err != nil {
				t.Fatal(err)
			}
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range secrets {
		if bytes.Contains(raw, []byte(s)) || bytes.Contains(raw, []byte(s[:24])) {
			t.Errorf("audit log holds %.12q…", s)
		}
	}
	if !bytes.Contains(raw, []byte("saved src/main.go with ")) {
		t.Error("the readable part of the summary was lost")
	}
}

func TestAuditSummaryCleaned(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"src/internal/webgw/listen_unix_test.go", "src/internal/webgw/listen_unix_test.go"},
		{"3 files", "3 files"},
		{"a\x1b[31mred\x07\r\nb", "a[31mredb"},
		{"x‮evil⁦y", "xevily"},
		{"bad \xff utf8", "bad � utf8"},
		{strings.Repeat("ab ", 100), strings.Repeat("ab ", 66) + "a…"},
	} {
		if got := cleanAuditText(tc.in); got != tc.want {
			t.Errorf("clean(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestGuardAuditsBlocked: a refused Host or Origin is written to the log
// (T1, T2), and a flood of them cannot push the real events out: at most
// a burst of blocked events per second is kept.
func TestGuardAuditsBlocked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	a := openAudit(t, path)
	g := GuardWithAudit(testSite, &okHandler{}, a)

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = "evil.com:41237"
	g.ServeHTTP(httptest.NewRecorder(), r)
	r = httptest.NewRequest(http.MethodPost, "/auth", nil)
	r.Host = testSite.Hosts[0]
	r.Header.Set("Origin", "http://evil.com")
	g.ServeHTTP(httptest.NewRecorder(), r)
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Host = testSite.Hosts[0]
	g.ServeHTTP(httptest.NewRecorder(), r) // allowed: not logged

	evs := readAudit(t, path)
	if len(evs) != 2 || evs[0].Kind != EvHostBlocked || evs[0].Code != 421 ||
		evs[1].Kind != EvOriginBlocked || evs[1].Code != 403 {
		t.Fatalf("events %+v", evs)
	}
	if !strings.Contains(evs[0].Summary, "evil.com") || !strings.Contains(evs[1].Summary, "evil.com") {
		t.Fatalf("summaries %q %q do not name the other site", evs[0].Summary, evs[1].Summary)
	}

	for i := 0; i < 200; i++ {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Host = "evil.com"
		g.ServeHTTP(httptest.NewRecorder(), r)
	}
	if n := len(readAudit(t, path)); n > 2+blockedAuditBurst+5 {
		t.Fatalf("%d lines after a flood, want at most about %d", n, 2+blockedAuditBurst)
	}
}

// TestServerAudits: the server's guard writes to the audit log it gets.
func TestServerAudits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.jsonl")
	a := openAudit(t, path)
	addr := startServer(t, NewServer(testSite, okServer().Handler, DefaultLimits, a), nil)
	if _, err := rawGet(addr, 10*time.Second); err != nil { // Host: x
		t.Fatal(err)
	}
	evs := readAudit(t, path)
	if len(evs) != 1 || evs[0].Kind != EvHostBlocked {
		t.Fatalf("events %+v", evs)
	}
}

// newAuditedAuth is an Auth on a fake clock that logs to a temp audit file.
func newAuditedAuth(t *testing.T) (*Auth, *Tokens, *fakeClock, string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.jsonl")
	clk := &fakeClock{now: t0}
	audit, err := OpenAudit(path, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = audit.Close() })
	ts := NewTokens(DefaultIdle, DefaultMax)
	a := NewAuth(ts, clk.Now, audit)
	plain, err := a.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	return a, ts, clk, plain, path
}

func kinds(evs []Event) string {
	var s []string
	for _, e := range evs {
		k := string(e.Kind)
		if e.Why != "" {
			k += ":" + e.Why
		}
		s = append(s, k)
	}
	return strings.Join(s, " ")
}

// TestAuthAudited: every /auth outcome leaves a line (T37): the good one
// names the new session, failures name the rule, a burn is its own event.
func TestAuthAudited(t *testing.T) {
	a, ts, _, plain, path := newAuditedAuth(t)
	do(a, authReq(secretBody("wrong")))
	ok := do(a, authReq(secretBody(plain)))
	do(a, authReq(secretBody(plain)))
	r := authReq(secretBody(plain))
	r.Header.Set("Content-Type", "text/plain")
	do(a, r)
	do(a, authReq(`{}`))
	r = authReq(secretBody(plain))
	r.URL.RawQuery = "s=x"
	do(a, r)

	fresh, err := a.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxBadTries; i++ {
		do(a, authReq(secretBody("wrong")))
	}
	do(a, authReq(secretBody(fresh)))

	evs := readAudit(t, path)
	want := "auth_fail:wrong auth_ok auth_fail:used auth_fail:need_json auth_fail:bad_request auth_fail:no_query " +
		"auth_fail:wrong auth_fail:wrong auth_fail:wrong auth_fail:wrong auth_fail:wrong secret_burned auth_fail:closed"
	if got := kinds(evs); got != want {
		t.Fatalf("events\n got %s\nwant %s", got, want)
	}
	var tok struct{ Token string }
	if err := json.Unmarshal(ok.Body.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}
	sess, err := ts.Check(tok.Token, t0)
	if err != nil {
		t.Fatal(err)
	}
	if evs[1].Conn != sess.ID || evs[1].Device != sess.Device || evs[1].Code != 200 {
		t.Fatalf("auth_ok %+v, session %+v", evs[1], sess)
	}
	if evs[0].Code != 401 || evs[3].Code != 415 {
		t.Fatalf("codes %d %d", evs[0].Code, evs[3].Code)
	}
}

// TestAuthRateLimitAudited: a 429 is logged, and a flood of them is
// rate-limited in the log too.
func TestAuthRateLimitAudited(t *testing.T) {
	a, _, _, _, path := newAuditedAuth(t)
	for i := 0; i < AuthRatePerSecond+200; i++ {
		do(a, authReq(`{}`))
	}
	evs := readAudit(t, path)
	limited := 0
	for _, e := range evs {
		if e.Kind == EvRateLimited {
			limited++
		}
	}
	if limited == 0 || len(evs) > 2*blockedAuditBurst+5 {
		t.Fatalf("%d rate_limited lines, %d in all", limited, len(evs))
	}
}

// TestNoSecretInLogsOrAudit: after a full auth flow (wrong tries, a good
// one, replays, a burn) neither the launch secrets nor the token appear
// anywhere in the audit files (T8).
func TestNoSecretInLogsOrAudit(t *testing.T) {
	a, _, _, plain, path := newAuditedAuth(t)
	do(a, authReq(secretBody(plain+"x")))
	ok := do(a, authReq(secretBody(plain)))
	do(a, authReq(secretBody(plain)))
	fresh, err := a.NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= MaxBadTries; i++ {
		do(a, authReq(secretBody(fresh[:42]+"Z")))
	}
	do(a, authReq(secretBody(fresh)))
	var tok struct{ Token string }
	if err := json.Unmarshal(ok.Body.Bytes(), &tok); err != nil || tok.Token == "" {
		t.Fatalf("no token: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{plain, fresh, tok.Token, plain[:16], fresh[:16], tok.Token[:16]} {
		if bytes.Contains(raw, []byte(s)) {
			t.Fatalf("audit log holds a secret part %.6q…", s)
		}
	}
}

// TestAuditEventsComplete: every event kind declared in audit.go is in
// AllEventKinds once, and each one round-trips through the log with all
// of its fields (T37).
func TestAuditEventsComplete(t *testing.T) {
	src, err := os.ReadFile("audit.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := regexp.MustCompile(`(?m)^\s+(Ev[A-Za-z]+)\s+EventKind = "([a-z_]+)"`).FindAllStringSubmatch(string(src), -1)
	listed := map[EventKind]int{}
	for _, k := range AllEventKinds {
		listed[k]++
	}
	if len(declared) != len(AllEventKinds) || len(listed) != len(AllEventKinds) {
		t.Fatalf("%d kinds declared, %d listed, %d distinct", len(declared), len(AllEventKinds), len(listed))
	}
	for _, d := range declared {
		if listed[EventKind(d[2])] != 1 {
			t.Errorf("%s (%q) is not in AllEventKinds once", d[1], d[2])
		}
	}
	for _, need := range []EventKind{EvStart, EvStop, EvAuthOK, EvAuthFail, EvSecretBurned,
		EvConnect, EvDisconnect, EvProtoError, EvOriginBlocked, EvHostBlocked,
		EvIdleStop, EvMaxStop, EvKillSwitch, EvOp} {
		if listed[need] != 1 {
			t.Errorf("design event %q missing", need)
		}
	}

	path := filepath.Join(t.TempDir(), "a.jsonl")
	a := openAudit(t, path)
	ok := false
	var want []Event
	for i, k := range AllEventKinds {
		e := Event{T: t0.Add(time.Duration(i) * time.Second), Kind: k, Conn: "s-0a0b0c0d",
			Device: "loc-1a2b", Op: "git.stage", OK: &ok, Code: 4000 + i, Why: "rule", Summary: "src/a.go"}
		want = append(want, e)
		if err := a.Log(e); err != nil {
			t.Fatal(err)
		}
	}
	got := readAudit(t, path)
	if len(got) != len(want) {
		t.Fatalf("%d lines, want %d", len(got), len(want))
	}
	for i := range want {
		g, w := got[i], want[i]
		if !g.T.Equal(w.T) || g.Kind != w.Kind || g.Conn != w.Conn || g.Device != w.Device ||
			g.Op != w.Op || g.OK == nil || *g.OK != *w.OK || g.Code != w.Code || g.Why != w.Why || g.Summary != w.Summary {
			t.Errorf("line %d: got %+v, want %+v", i, g, w)
		}
	}
}

// TestAuditNilIsQuiet: code with no audit log configured just runs.
func TestAuditNilIsQuiet(t *testing.T) {
	var a *Audit
	if err := a.Log(Event{Kind: EvStart}); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
}
