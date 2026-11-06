package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webHTTPRig is a workspace with a .http file whose requests go to a
// local test server that echoes what it got.
type webHTTPRig struct {
	*webFilesRig
	srv  *httptest.Server
	mu   sync.Mutex
	got  []*http.Request
	body []string
}

// The token of the private environment file, split so no scanner sees a
// real key.
var webHTTPToken = "tk-" + "web-9f8e7d6c5b4a"

func newWebHTTPRig(t *testing.T) *webHTTPRig {
	t.Helper()
	r := &webHTTPRig{webFilesRig: newWebFilesRig(t)}
	r.m.output = newOutputStore()
	r.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		b, _ := io.ReadAll(q.Body)
		r.mu.Lock()
		r.got = append(r.got, q)
		r.body = append(r.body, string(b))
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Set-Cookie", "sid="+"cookie-"+"secret-1; Path=/")
		fmt.Fprintf(w, `{"path":%q,"auth":%q}`, q.URL.Path, q.Header.Get("Authorization"))
	}))
	t.Cleanup(r.srv.Close)
	r.write("http-client.env.json", `{"dev":{"host":"`+r.srv.URL+`"},"prod":{"host":"https://api.example.com"}}`)
	r.write("http-client.private.env.json", `{"dev":{"token":"`+webHTTPToken+`"}}`)
	r.write("api/users.http", "### list\n# @name list\nGET {{host}}/users\nAuthorization: Bearer {{token}}\n\n###\n# @name create\nPOST {{host}}/users\nContent-Type: application/json\n\n{\"name\":\"a\"}\n\n###\nGET {{host}}/x?q={{nope}}\n")
	if err := httpSetSelectedEnv(r.root, "dev"); err != nil {
		t.Fatal(err)
	}
	return r
}

// do runs op with JSON args (decoded by the wire rules first).
func (r *webHTTPRig) do(op, args string) map[string]any {
	r.t.Helper()
	a, err := webproto.DecodeArgs(op, json.RawMessage(args))
	if err != nil {
		r.t.Fatalf("%s %s: %v", op, args, err)
	}
	return r.call(op, a)
}

func (r *webHTTPRig) sent() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

// noLeak fails when the frame holds the token or the cookie value.
func noLeak(t *testing.T, what string, f any) {
	t.Helper()
	b, _ := json.Marshal(f)
	for _, s := range []string{webHTTPToken, "cookie-" + "secret-1"} {
		if strings.Contains(string(b), s) {
			t.Fatalf("%s shows a secret: %s", what, b)
		}
	}
}

// Spec (WEBP-HTTP): http.files lists the workspace's .http requests as
// written ({{variables}} kept), the selected environment and the
// environments' names.
func TestWebHTTPFilesListsRequests(t *testing.T) {
	r := newWebHTTPRig(t)
	var out webproto.HTTPFiles
	webTestData(t, r.do(webproto.OpHTTPFiles, `{}`), &out)
	if out.Env != "dev" || strings.Join(out.Envs, ",") != "dev,prod" || len(out.Files) != 1 {
		t.Fatalf("files %+v", out)
	}
	f := out.Files[0]
	if f.Path != "api/users.http" || len(f.Requests) != 3 || f.Requests[0].Name != "list" || f.Requests[0].Method != "GET" ||
		f.Requests[0].URL != "{{host}}/users" || f.Requests[1].Index != 2 || f.Requests[1].Method != "POST" {
		t.Fatalf("file %+v", f)
	}
}

// Spec: the laptop sends a .http request with the environment's values
// (the server gets the token); the page's answer has the status and the
// body, and never the token or a cookie value: the request's
// Authorization header and the echo of it in the body are masked.
func TestWebHTTPSendLoopbackRunsAtOnceAndMasksSecrets(t *testing.T) {
	r := newWebHTTPRig(t)
	var sent webproto.HTTPSent
	webTestData(t, r.do(webproto.OpHTTPSend, `{"path":"api/users.http","index":1}`), &sent)
	if sent.Run == 0 || sent.Card != "" || r.m.confirmOpen {
		t.Fatalf("sent %+v (card open %v)", sent, r.m.confirmOpen)
	}
	if r.sent() != 1 || r.got[0].Header.Get("Authorization") != "Bearer "+webHTTPToken || r.got[0].URL.Path != "/users" {
		t.Fatalf("the server got %d requests: %+v", r.sent(), r.got)
	}
	f := r.do(webproto.OpHTTPResponse, fmt.Sprintf(`{"run":%d}`, sent.Run))
	noLeak(t, "http.response", f)
	var res webproto.HTTPResponse
	webTestData(t, f, &res)
	if res.Run != sent.Run || res.Running || res.Status != 200 || res.By != "web" || res.Method != "GET" || !strings.Contains(res.Body, `"path":"/users"`) ||
		res.File != "api/users.http" || res.Env != "dev" || res.ContentType != "application/json" {
		t.Fatalf("response %+v", res)
	}
	auth := ""
	for _, h := range res.ReqHeaders {
		if h.Name == "Authorization" {
			auth = h.Value
		}
	}
	if auth == "" {
		t.Fatalf("the request's headers %+v", res.ReqHeaders)
	}
	// The newest run is the default.
	var last webproto.HTTPResponse
	webTestData(t, r.do(webproto.OpHTTPResponse, `{}`), &last)
	if last.Run != sent.Run {
		t.Fatalf("last %+v", last)
	}
}

// Spec: a request typed on the page (method, url, headers, body) is sent
// as typed; its secret header is masked in the answer.
func TestWebHTTPSendTypedRequest(t *testing.T) {
	r := newWebHTTPRig(t)
	args := fmt.Sprintf(`{"method":"PUT","url":"%s/items/7","headers":[{"name":"Authorization","value":"Bearer %s"},{"name":"X-Mode","value":"a"}],"body":"{\"n\":1}"}`, r.srv.URL, webHTTPToken)
	var sent webproto.HTTPSent
	webTestData(t, r.do(webproto.OpHTTPSend, args), &sent)
	if r.sent() != 1 || r.got[0].Method != "PUT" || r.got[0].Header.Get("X-Mode") != "a" || r.body[0] != `{"n":1}` {
		t.Fatalf("the server got %+v %v", r.got, r.body)
	}
	noLeak(t, "http.response", r.do(webproto.OpHTTPResponse, fmt.Sprintf(`{"run":%d}`, sent.Run)))
}

// Spec: a request with a {{variable}} no environment defines is not
// sent; the answer names the variable.
func TestWebHTTPSendUnresolvedIsRefused(t *testing.T) {
	r := newWebHTTPRig(t)
	f := r.do(webproto.OpHTTPSend, `{"path":"api/users.http","index":3}`)
	if webTestErr(f) != "bad_request" || !strings.Contains(fmt.Sprint(f["msg"]), "nope") || r.sent() != 0 {
		t.Fatalf("unresolved: %v (sent %d)", f, r.sent())
	}
	if f := r.do(webproto.OpHTTPSend, `{"path":"api/none.http","index":1}`); webTestErr(f) != "not_found" {
		t.Fatalf("no file: %v", f)
	}
	if f := r.do(webproto.OpHTTPSend, `{"path":"api/users.http","index":9}`); webTestErr(f) != "not_found" {
		t.Fatalf("no request 9: %v", f)
	}
	if f := r.do(webproto.OpHTTPSend, `{"url":"ftp://example.com/a"}`); webTestErr(f) != "bad_request" {
		t.Fatalf("ftp: %v", f)
	}
}

// Spec: a request to a host that is not this machine waits for a laptop
// card (Cancel first) the first time; nothing is sent before the yes.
// After the yes it is sent, and the next request to that host from the
// web runs at once. A private address asks every time.
func TestWebHTTPOtherHostNeedsLaptopCardOnce(t *testing.T) {
	r := newWebHTTPRig(t)
	var hits []string
	webHTTPDo = func(_ context.Context, _ *httpclient.Client, rr httpclient.Resolved, _ httpclient.Hooks) *httpclient.Response {
		hits = append(hits, rr.Method+" "+rr.URL)
		return &httpclient.Response{Status: 204, StatusText: "No Content"}
	}
	t.Cleanup(func() { webHTTPDo = webHTTPSendReal })
	var sent webproto.HTTPSent
	webTestData(t, r.do(webproto.OpHTTPSend, `{"url":"https://api.example.com/v1/me"}`), &sent)
	if sent.Card == "" || sent.Run != 0 || sent.Host != "api.example.com" || len(hits) != 0 {
		t.Fatalf("sent %+v hits %v", sent, hits)
	}
	if !r.m.confirmOpen || r.m.confirmKind != confirmKindWebAsk || r.m.confirm.SafeID() != "cancel" {
		t.Fatal("no laptop card")
	}
	if v := r.m.confirm.View(); !strings.Contains(v, "api.example.com") || !strings.Contains(v, "GET") {
		t.Errorf("card:\n%s", v)
	}
	webTestLaptopSelect(t, r.m, "confirm")
	if len(hits) != 1 || hits[0] != "GET https://api.example.com/v1/me" {
		t.Fatalf("after yes: %v", hits)
	}
	var last webproto.HTTPResponse
	webTestData(t, r.do(webproto.OpHTTPResponse, `{}`), &last)
	if last.Status != 204 || last.By != "web" {
		t.Fatalf("last %+v", last)
	}
	sent = webproto.HTTPSent{}
	webTestData(t, r.do(webproto.OpHTTPSend, `{"method":"DELETE","url":"https://api.example.com/v1/x"}`), &sent)
	if sent.Card != "" || sent.Run == 0 || len(hits) != 2 || r.m.confirmOpen {
		t.Fatalf("second send to the host %+v hits %v", sent, hits)
	}
	// Another host asks again; a private one asks every time.
	sent = webproto.HTTPSent{}
	webTestData(t, r.do(webproto.OpHTTPSend, `{"url":"https://other.example.org/"}`), &sent)
	if sent.Card == "" || len(hits) != 2 {
		t.Fatalf("other host %+v", sent)
	}
	webTestLaptopSelect(t, r.m, "cancel")
	if len(hits) != 2 {
		t.Fatalf("sent after cancel: %v", hits)
	}
	for i := 0; i < 2; i++ {
		sent = webproto.HTTPSent{}
		webTestData(t, r.do(webproto.OpHTTPSend, `{"url":"http://192.168.1.20/status"}`), &sent)
		if sent.Card == "" {
			t.Fatalf("private host, try %d: %+v", i, sent)
		}
		webTestLaptopSelect(t, r.m, "confirm")
	}
	if len(hits) != 4 {
		t.Fatalf("private sends %v", hits)
	}
}

// Spec: http.cancel stops the web's running request; the run then says
// cancelled.
func TestWebHTTPCancel(t *testing.T) {
	r := newWebHTTPRig(t)
	release := make(chan struct{})
	webHTTPDo = func(ctx context.Context, _ *httpclient.Client, _ httpclient.Resolved, _ httpclient.Hooks) *httpclient.Response {
		select {
		case <-ctx.Done():
			return &httpclient.Response{Err: "cancelled", ErrKind: httpclient.ErrKindCancelled}
		case <-release:
			return &httpclient.Response{Status: 200}
		}
	}
	t.Cleanup(func() { webHTTPDo = webHTTPSendReal; close(release) })
	if f := r.do(webproto.OpHTTPCancel, `{}`); webTestErr(f) != "not_found" {
		t.Fatalf("nothing running: %v", f)
	}
	a, _ := webproto.DecodeArgs(webproto.OpHTTPSend, json.RawMessage(`{"path":"api/users.http","index":1}`))
	r.id++
	r.c.inflight.Add(1)
	call := &webCall{ctx: r.c.ctx, conn: r.c, id: r.id, op: webproto.OpHTTPSend, args: a}
	cmd, _ := r.m.webRunCall(r.h, call)
	if cmd == nil {
		t.Fatal("no send started")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var run webproto.HTTPResponse
	webTestData(t, r.do(webproto.OpHTTPResponse, `{}`), &run)
	if !run.Running {
		t.Fatalf("not running: %+v", run)
	}
	if f := r.do(webproto.OpHTTPCancel, `{}`); f["t"] != "res" {
		t.Fatalf("cancel: %v", f)
	}
	webDrain(t, r.m, func() tea.Msg { return <-done })
	run = webproto.HTTPResponse{}
	webTestData(t, r.do(webproto.OpHTTPResponse, `{}`), &run)
	if run.Running || run.Err == "" {
		t.Fatalf("after cancel: %+v", run)
	}
}

// Spec: history lists the sends newest first with who sent them; the
// environments show values, a private or secret one hidden; selecting
// an environment is what the laptop's .http files use next.
func TestWebHTTPHistoryAndEnvs(t *testing.T) {
	r := newWebHTTPRig(t)
	r.do(webproto.OpHTTPSend, `{"path":"api/users.http","index":1}`)
	r.do(webproto.OpHTTPSend, `{"path":"api/users.http","index":2}`)
	f := r.do(webproto.OpHTTPHistory, `{}`)
	noLeak(t, "http.history", f)
	var h webproto.HTTPHistory
	webTestData(t, f, &h)
	if len(h.Entries) != 2 || h.Entries[0].Method != "POST" || h.Entries[0].By != "web" || h.Entries[0].Status != 200 || h.Entries[1].Name != "list" {
		t.Fatalf("history %+v", h)
	}
	// Send a history entry again.
	n := r.sent()
	var sent webproto.HTTPSent
	webTestData(t, r.do(webproto.OpHTTPSend, `{"history":"`+h.Entries[1].ID+`"}`), &sent)
	if r.sent() != n+1 || r.got[n].Header.Get("Authorization") != "Bearer "+webHTTPToken {
		t.Fatalf("resend: %d %+v", r.sent(), r.got)
	}
	f = r.do(webproto.OpHTTPEnvs, `{}`)
	noLeak(t, "http.envs", f)
	var e webproto.HTTPEnvs
	webTestData(t, f, &e)
	if e.Selected != "dev" || len(e.Envs) != 2 || e.Envs[0].Name != "dev" {
		t.Fatalf("envs %+v", e)
	}
	seen := map[string]webproto.HTTPVar{}
	for _, v := range e.Envs[0].Vars {
		seen[v.Name] = v
	}
	if seen["host"].Value != r.srv.URL || !seen["token"].Hidden || seen["token"].Value != "" {
		t.Fatalf("dev vars %+v", e.Envs[0].Vars)
	}
	if f := r.do(webproto.OpHTTPEnvSelect, `{"name":"staging"}`); webTestErr(f) != "not_found" {
		t.Fatalf("unknown env: %v", f)
	}
	r.do(webproto.OpHTTPEnvSelect, `{"name":"prod"}`)
	if got := httpSelectedEnv(r.root); got != "prod" {
		t.Fatalf("selected %q", got)
	}
	r.do(webproto.OpHTTPEnvSelect, `{"name":""}`)
	if got := httpSelectedEnv(r.root); got != "" {
		t.Fatalf("selected %q after none", got)
	}
}

// Spec: http.env_file opens the workspace's environment file, made from
// the template when there is none; the private file is never offered.
func TestWebHTTPEnvFile(t *testing.T) {
	r := newWebHTTPRig(t)
	_ = os.Remove(filepath.Join(r.root, "http-client.env.json"))
	var p webproto.HTTPPath
	webTestData(t, r.do(webproto.OpHTTPEnvFile, `{}`), &p)
	b, err := os.ReadFile(filepath.Join(r.root, "http-client.env.json"))
	if p.Path != "http-client.env.json" || err != nil || !strings.Contains(string(b), `"dev"`) {
		t.Fatalf("env file %+v %v %s", p, err, b)
	}
}

// Spec: a run's request is saved to the laptop's saved requests (as
// written, a literal secret header masked); the saved list sends again.
func TestWebHTTPSaveAndSaved(t *testing.T) {
	r := newWebHTTPRig(t)
	args := fmt.Sprintf(`{"url":"%s/a","headers":[{"name":"Authorization","value":"Bearer %s"}]}`, r.srv.URL, webHTTPToken)
	var sent webproto.HTTPSent
	webTestData(t, r.do(webproto.OpHTTPSend, args), &sent)
	if f := r.do(webproto.OpHTTPSave, fmt.Sprintf(`{"run":%d,"name":"get a"}`, sent.Run)); f["t"] != "res" {
		t.Fatalf("save: %v", f)
	}
	r.do(webproto.OpHTTPSave, `{"path":"api/users.http","index":2,"name":"make user"}`)
	b, _ := os.ReadFile(httpSavedPath())
	if strings.Contains(string(b), webHTTPToken) || !strings.Contains(string(b), "get a") {
		t.Fatalf("saved.http:\n%s", b)
	}
	var s webproto.HTTPSaved
	webTestData(t, r.do(webproto.OpHTTPSaved, `{}`), &s)
	if len(s.Requests) != 2 || s.Requests[0].Name != "get a" || s.Requests[1].Method != "POST" || s.Requests[1].Index != 2 {
		t.Fatalf("saved %+v", s)
	}
	n := r.sent()
	r.do(webproto.OpHTTPSend, `{"saved":2}`)
	if r.sent() != n+1 || r.got[n].Method != "POST" {
		t.Fatalf("saved send: %d", r.sent())
	}
	if f := r.do(webproto.OpHTTPSend, `{"saved":5}`); webTestErr(f) != "not_found" {
		t.Fatalf("saved 5: %v", f)
	}
}

// Spec: copy as curl gives one line with secret values as their
// {{template}}, never the value.
func TestWebHTTPCurlKeepsTemplates(t *testing.T) {
	r := newWebHTTPRig(t)
	f := r.do(webproto.OpHTTPCurl, `{"path":"api/users.http","index":1}`)
	noLeak(t, "http.curl", f)
	var c webproto.HTTPCurl
	webTestData(t, f, &c)
	if !strings.HasPrefix(c.Curl, "curl ") || !strings.Contains(c.Curl, r.srv.URL+"/users") || !strings.Contains(c.Curl, "{{token}}") {
		t.Fatalf("curl %q", c.Curl)
	}
	var sent webproto.HTTPSent
	webTestData(t, r.do(webproto.OpHTTPSend, `{"path":"api/users.http","index":1}`), &sent)
	f = r.do(webproto.OpHTTPCurl, fmt.Sprintf(`{"run":%d}`, sent.Run))
	noLeak(t, "http.curl of a run", f)
}

// Spec: a curl line becomes a request at the end of a .http file
// (scratch.http when none is named); a new .http file starts from the
// template; a response body is saved to a new workspace file. Names that
// exist are refused.
func TestWebHTTPImportNewFileSaveBody(t *testing.T) {
	r := newWebHTTPRig(t)
	var p webproto.HTTPPath
	webTestData(t, r.do(webproto.OpHTTPImportCurl, `{"curl":"curl -X POST https://api.example.com/items -H 'Content-Type: application/json' -d '{\"a\":1}'"}`), &p)
	b, _ := os.ReadFile(filepath.Join(r.root, "scratch.http"))
	if p.Path != "scratch.http" || p.Line == 0 || !strings.Contains(string(b), "POST https://api.example.com/items") {
		t.Fatalf("import %+v:\n%s", p, b)
	}
	webTestData(t, r.do(webproto.OpHTTPImportCurl, `{"curl":"curl https://api.example.com/b","path":"api/users.http"}`), &p)
	b, _ = os.ReadFile(filepath.Join(r.root, "api", "users.http"))
	if p.Path != "api/users.http" || !strings.Contains(string(b), "GET https://api.example.com/b") || !strings.Contains(string(b), "# @name list") {
		t.Fatalf("import into users.http %+v:\n%s", p, b)
	}
	if f := r.do(webproto.OpHTTPImportCurl, `{"curl":"wget x"}`); webTestErr(f) != "bad_request" {
		t.Fatalf("not curl: %v", f)
	}
	if f := r.do(webproto.OpHTTPNewFile, `{"path":"api/orders"}`); webTestErr(f) != "bad_request" {
		t.Fatalf("not a .http name: %v", f)
	}
	webTestData(t, r.do(webproto.OpHTTPNewFile, `{"path":"api/orders.http"}`), &p)
	b, _ = os.ReadFile(filepath.Join(r.root, "api", "orders.http"))
	if p.Path != "api/orders.http" || !strings.Contains(string(b), "{{host}}") {
		t.Fatalf("new file %+v:\n%s", p, b)
	}
	if f := r.do(webproto.OpHTTPNewFile, `{"path":"api/orders.http"}`); webTestErr(f) != "conflict" {
		t.Fatalf("existing file: %v", f)
	}
	var sent webproto.HTTPSent
	webTestData(t, r.do(webproto.OpHTTPSend, `{"path":"api/users.http","index":2}`), &sent)
	webTestData(t, r.do(webproto.OpHTTPSaveBody, fmt.Sprintf(`{"run":%d,"path":"out.json"}`, sent.Run)), &p)
	b, _ = os.ReadFile(filepath.Join(r.root, "out.json"))
	if p.Path != "out.json" || p.Size == 0 || !strings.Contains(string(b), `"path":"/users"`) {
		t.Fatalf("save body %+v: %s", p, b)
	}
	if f := r.do(webproto.OpHTTPSaveBody, fmt.Sprintf(`{"run":%d,"path":"out.json"}`, sent.Run)); webTestErr(f) != "conflict" {
		t.Fatalf("existing body file: %v", f)
	}
}
