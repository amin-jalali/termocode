package trigger

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type hookSink struct{ got []Event }

func newServer(from string, sink *hookSink, now time.Time) *Server {
	return &Server{
		Lookup: func(name string) (Hook, bool) {
			if name != "h" {
				return Hook{}, false
			}
			return Hook{Workflow: "wf", Trigger: 1, Name: "h", From: from}, true
		},
		Secret:  func(name string) (string, bool) { return "s3cret", name == "h" },
		Deliver: func(e Event) { sink.got = append(sink.got, e) },
		Now:     func() time.Time { return now },
	}
}

func post(s *Server, path string, body []byte, h map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	for k, v := range h {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func hexMAC(secret string, parts ...string) string {
	m := hmac.New(sha256.New, []byte(secret))
	for _, p := range parts {
		m.Write([]byte(p))
	}
	return hex.EncodeToString(m.Sum(nil))
}

// A generic hook takes a signed JSON body; a retry of the same key is
// dropped; a bad or missing signature and an unknown hook are refused.
func TestWebhookGeneric(t *testing.T) {
	sink := &hookSink{}
	s := newServer("generic", sink, time.Now())
	body := []byte(`{"key":"k1","title":"Deploy failed","body":"see log","labels":["prod"]}`)
	if w := post(s, "/hooks/h", body, map[string]string{"X-Termocode-Signature": Sign("s3cret", body)}); w.Code != http.StatusAccepted {
		t.Fatalf("code %d %s", w.Code, w.Body)
	}
	if len(sink.got) != 1 {
		t.Fatalf("got %+v", sink.got)
	}
	e := sink.got[0]
	if e.Workflow != "wf" || e.Trigger != 1 || e.Key != "hook:h:k1" || e.Title != "Deploy failed" || e.Labels[0] != "prod" || e.Source != "generic" {
		t.Errorf("event %+v", e)
	}
	post(s, "/hooks/h", body, map[string]string{"X-Termocode-Signature": Sign("s3cret", body)})
	if len(sink.got) != 1 {
		t.Error("retry delivered twice")
	}
	for _, tc := range []struct {
		path string
		h    map[string]string
		code int
	}{
		{"/hooks/h", nil, http.StatusUnauthorized},
		{"/hooks/h", map[string]string{"X-Termocode-Signature": Sign("wrong", body)}, http.StatusUnauthorized},
		{"/hooks/nope", map[string]string{"X-Termocode-Signature": Sign("s3cret", body)}, http.StatusNotFound},
		{"/hooks/", nil, http.StatusNotFound},
		{"/other", nil, http.StatusNotFound},
	} {
		if w := post(s, tc.path, body, tc.h); w.Code != tc.code {
			t.Errorf("%s %v: code %d, want %d", tc.path, tc.h, w.Code, tc.code)
		}
	}
	// Bearer works for senders that cannot sign.
	b2 := []byte(`{"id":"k2","text":"hi"}`)
	if w := post(s, "/hooks/h", b2, map[string]string{"Authorization": "Bearer s3cret"}); w.Code != http.StatusAccepted || len(sink.got) != 2 {
		t.Errorf("bearer: %d %+v", w.Code, sink.got)
	}
	// GET, a big body and bad JSON.
	r := httptest.NewRequest(http.MethodGet, "/hooks/h", nil)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET %d", w.Code)
	}
	big := bytes.Repeat([]byte("a"), MaxHookBody+1)
	if w := post(s, "/hooks/h", big, nil); w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("big %d", w.Code)
	}
	bad := []byte(`{nope`)
	if w := post(s, "/hooks/h", bad, map[string]string{"X-Termocode-Signature": Sign("s3cret", bad)}); w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "nope") {
		t.Errorf("bad json %d %q", w.Code, w.Body)
	}
}

// A hook with no secret in the store is refused: unsigned hooks never run.
func TestWebhookNoSecret(t *testing.T) {
	sink := &hookSink{}
	s := newServer("generic", sink, time.Now())
	s.Secret = func(string) (string, bool) { return "", false }
	body := []byte(`{}`)
	if w := post(s, "/hooks/h", body, map[string]string{"X-Termocode-Signature": Sign("", body)}); w.Code != http.StatusUnauthorized {
		t.Errorf("code %d", w.Code)
	}
}

func TestWebhookGitHub(t *testing.T) {
	sink := &hookSink{}
	s := newServer("github", sink, time.Now())
	body := []byte(`{"action":"labeled","label":{"name":"agent"},"repository":{"full_name":"o/r"},"issue":{"number":5,"title":"Bug","html_url":"u","body":"b","user":{"login":"ann"},"labels":[{"name":"agent"},{"name":"x"}]}}`)
	h := map[string]string{"X-Hub-Signature-256": "sha256=" + hexMAC("s3cret", string(body)), "X-GitHub-Event": "issues", "X-GitHub-Delivery": "d1"}
	if w := post(s, "/hooks/h", body, h); w.Code != http.StatusAccepted {
		t.Fatalf("code %d", w.Code)
	}
	if len(sink.got) != 1 || sink.got[0].Kind != KindIssueLabeled || sink.got[0].Labels[0] != "agent" || len(sink.got[0].Labels) != 1 || sink.got[0].Repo != "o/r" || sink.got[0].Key != "hook:h:d1" {
		t.Errorf("got %+v", sink.got)
	}
	// A closed issue and a ping are taken but start nothing.
	for i, b := range []string{`{"action":"closed","issue":{"number":5}}`, `{"zen":"x"}`} {
		ev := "issues"
		if i == 1 {
			ev = "ping"
		}
		post(s, "/hooks/h", []byte(b), map[string]string{"X-Hub-Signature-256": "sha256=" + hexMAC("s3cret", b), "X-GitHub-Event": ev, "X-GitHub-Delivery": "d" + strconv.Itoa(i+2)})
	}
	wr := `{"action":"completed","workflow_run":{"id":3,"name":"ci","head_branch":"main","conclusion":"failure","html_url":"r"}}`
	post(s, "/hooks/h", []byte(wr), map[string]string{"X-Hub-Signature-256": "sha256=" + hexMAC("s3cret", wr), "X-GitHub-Event": "workflow_run", "X-GitHub-Delivery": "d9"})
	if len(sink.got) != 2 || sink.got[1].Kind != KindCIFailed || sink.got[1].Branch != "main" {
		t.Errorf("got %+v", sink.got)
	}
}

// Slack: the url_verification challenge is answered after the
// signature check; an old timestamp is a replay; a bot message is
// ignored (no loops).
func TestWebhookSlack(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	sink := &hookSink{}
	s := newServer("slack", sink, now)
	sign := func(body string, ts int64) map[string]string {
		t := strconv.FormatInt(ts, 10)
		return map[string]string{"X-Slack-Request-Timestamp": t, "X-Slack-Signature": "v0=" + hexMAC("s3cret", "v0:"+t+":", body)}
	}
	ch := `{"type":"url_verification","challenge":"abc"}`
	if w := post(s, "/hooks/h", []byte(ch), sign(ch, now.Unix())); w.Code != 200 || w.Body.String() != "abc" {
		t.Errorf("challenge %d %q", w.Code, w.Body)
	}
	ev := `{"type":"event_callback","event_id":"E1","event":{"type":"app_mention","text":"<@U1> fix the login bug","user":"U2","channel":"C1","ts":"1.2"}}`
	if w := post(s, "/hooks/h", []byte(ev), sign(ev, now.Unix()-10*60)); w.Code != http.StatusUnauthorized {
		t.Errorf("replay %d", w.Code)
	}
	if w := post(s, "/hooks/h", []byte(ev), sign(ev, now.Unix())); w.Code != http.StatusAccepted {
		t.Errorf("event %d", w.Code)
	}
	bot := `{"type":"event_callback","event_id":"E2","event":{"type":"message","text":"done","bot_id":"B1"}}`
	post(s, "/hooks/h", []byte(bot), sign(bot, now.Unix()))
	if len(sink.got) != 1 || sink.got[0].Source != "slack" || sink.got[0].Author != "U2" || !strings.Contains(sink.got[0].Body, "login bug") {
		t.Errorf("got %+v", sink.got)
	}
}

func TestWebhookLinearJira(t *testing.T) {
	sink := &hookSink{}
	s := newServer("linear", sink, time.Now())
	lb := `{"action":"create","type":"Issue","webhookTimestamp":5,"data":{"id":"i1","identifier":"ENG-4","title":"Crash","description":"d","url":"lu","labels":[{"name":"agent"}]}}`
	if w := post(s, "/hooks/h", []byte(lb), map[string]string{"Linear-Signature": hexMAC("s3cret", lb)}); w.Code != http.StatusAccepted {
		t.Fatalf("linear %d", w.Code)
	}
	s2 := newServer("jira", sink, time.Now())
	jb := `{"webhookEvent":"jira:issue_created","timestamp":7,"issue":{"key":"PRJ-9","self":"ju","fields":{"summary":"Slow page","labels":["perf"],"description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"It is slow"}]}]}}}}`
	if w := post(s2, "/hooks/h", []byte(jb), map[string]string{"X-Hub-Signature": "sha256=" + hexMAC("s3cret", jb)}); w.Code != http.StatusAccepted {
		t.Fatalf("jira %d", w.Code)
	}
	if len(sink.got) != 2 {
		t.Fatalf("got %+v", sink.got)
	}
	l, j := sink.got[0], sink.got[1]
	if l.Title != "ENG-4 Crash" || l.Labels[0] != "agent" || l.URL != "lu" || l.Source != "linear" {
		t.Errorf("linear %+v", l)
	}
	if j.Title != "PRJ-9 Slow page" || j.Body != "It is slow" || j.Labels[0] != "perf" || j.Source != "jira" {
		t.Errorf("jira %+v", j)
	}
}

// More than HookPerMinute requests in a minute are refused.
func TestWebhookRateLimit(t *testing.T) {
	sink := &hookSink{}
	s := newServer("generic", sink, time.Now())
	code := 0
	for i := 0; i <= HookPerMinute; i++ {
		b := []byte(`{"key":"` + strconv.Itoa(i) + `"}`)
		code = post(s, "/hooks/h", b, map[string]string{"X-Termocode-Signature": Sign("s3cret", b)}).Code
	}
	if code != http.StatusTooManyRequests || len(sink.got) != HookPerMinute {
		t.Errorf("last code %d, delivered %d", code, len(sink.got))
	}
}

// Outside text is cut and folded before a card shows it.
func TestEventClip(t *testing.T) {
	e := Event{Kind: KindWebhook, Source: "generic", Title: "a\x1b[31mb\nc", Body: strings.Repeat("x", MaxBody+10)}
	if s := e.Summary(); strings.ContainsAny(s, "\x1b\n") {
		t.Errorf("summary %q", s)
	}
	if b := e.Vars()["trigger_body"].(string); len(b) > MaxBody+4 {
		t.Errorf("body %d bytes", len(b))
	}
}
