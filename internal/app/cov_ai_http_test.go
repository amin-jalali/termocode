package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/httpclient"
)

// The http_* AI tools (ai_http_tools.go) against an httptest server on
// 127.0.0.1: request files, environments, sends with every refusal, the
// answer pages, history and saving a request.

type covHTTPEnv struct {
	root  string
	srv   *httptest.Server
	m     *Model
	tools map[string]ai.Tool
	wrote map[string]string
}

func covHTTPSetup(t *testing.T) *covHTTPEnv {
	t.Helper()
	withConfigDir(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	mux := http.NewServeMux()
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[` + strings.Repeat(`{"name":"item"},`, 40) + `{"name":"last"}]}`))
	})
	mux.HandleFunc("/empty", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3, 0, 0})
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(r.Method + " " + r.Header.Get("X-Mode")))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	root := t.TempDir()
	e := &covHTTPEnv{root: root, srv: srv, m: &Model{w: 120, h: 40}, wrote: map[string]string{}}
	wf := ai.Tool{Name: "write_file", Mutating: true, Class: ai.ClassFileWrite,
		Gate: func(a map[string]any) (bool, string) { return true, ai.ClassFileWrite },
		Run: func(_ context.Context, a map[string]any) (string, error) {
			e.wrote[ai.ArgString(a, "path")] = ai.ArgString(a, "content")
			return "wrote", nil
		}}
	e.tools = map[string]ai.Tool{}
	for _, tl := range e.m.aiHTTPTools(root, covSyncCall(e.m), []ai.Tool{wf}) {
		e.tools[tl.Name] = tl
	}
	return e
}

func (e *covHTTPEnv) write(t *testing.T, rel, body string) {
	t.Helper()
	p := filepath.Join(e.root, rel)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *covHTTPEnv) run(t *testing.T, name string, a map[string]any) (string, error) {
	t.Helper()
	if a == nil {
		a = map[string]any{}
	}
	return e.tools[name].Run(context.Background(), a)
}

// The request list: one file or all, file variables, the selected env.
func TestCovAIHTTPListRequests(t *testing.T) {
	e := covHTTPSetup(t)
	out, err := e.run(t, "http_list_requests", nil)
	if err != nil || !strings.HasPrefix(out, "no .http or .rest files") {
		t.Errorf("no files: %q %v", out, err)
	}
	e.write(t, "api/a.http", "@base = http://x\n@base = http://y\n\n### one\nGET {{base}}/1\n\n###\nPOST {{base}}/2\n")
	out, err = e.run(t, "http_list_requests", map[string]any{"path": "api/a.http"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "list", out, "api/a.http:", "#1  one  GET {{base}}/1", "#2  -  POST {{base}}/2", "file variables: base\n", "selected environment: (none)")
	if _, err := e.run(t, "http_list_requests", map[string]any{"path": "notes.txt"}); err == nil {
		t.Error("a .txt file listed")
	}
	if _, err := e.run(t, "http_list_requests", map[string]any{"path": "../../etc/x.http"}); err == nil {
		t.Error("a path outside the workspace listed")
	}
	if got := e.tools["http_list_requests"].Describe(map[string]any{"path": "a.http"}) + "|" + e.tools["http_list_requests"].Describe(nil); got != "List HTTP requests in a.http|List HTTP requests" {
		t.Errorf("cards = %q", got)
	}
}

// Environments: names and values, secrets hidden, a broken file named,
// selecting one and clearing it.
func TestCovAIHTTPEnvironments(t *testing.T) {
	e := covHTTPSetup(t)
	out, err := e.run(t, "http_environments", nil)
	if err != nil || !strings.HasPrefix(out, "no environment files") {
		t.Errorf("none: %q %v", out, err)
	}
	e.write(t, "http-client.env.json", `{"$shared":{"ver":"1"},"dev":{"host":"`+e.srv.URL+`","api_key":"k-cov-123456789"}}`)
	e.write(t, "http-client.private.env.json", `{"dev":{"pass":"p-cov-123456789"}}`)
	out, err = e.run(t, "http_environments", nil)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "envs", out, "file: http-client.env.json", "dev:", "host = "+e.srv.URL, "api_key = [", "pass = [hidden]", "$shared:", "ver = 1")
	if strings.Contains(out, "k-cov-123456789") || strings.Contains(out, "p-cov-123456789") {
		t.Errorf("a secret leaked:\n%s", out)
	}
	set := e.tools["http_set_environment"]
	if set.Validate(map[string]any{"name": "prod"}) == nil || set.Validate(map[string]any{"name": "none"}) != nil || set.Validate(map[string]any{"name": "dev"}) != nil {
		t.Error("set env validate")
	}
	if out, err := set.Run(context.Background(), map[string]any{"name": "dev"}); err != nil || out != "environment set to dev" || httpSelectedEnv(e.root) != "dev" {
		t.Errorf("set dev: %q %v", out, err)
	}
	if _, err := set.Run(context.Background(), map[string]any{"name": "none"}); err != nil || httpSelectedEnv(e.root) != "" {
		t.Errorf("clear: %v %q", err, httpSelectedEnv(e.root))
	}
	if set.Describe(map[string]any{"name": "dev"}) != "Select HTTP environment dev" || e.tools["http_environments"].Describe(nil) != "Read HTTP environments" {
		t.Error("env cards")
	}
	e.write(t, "http-client.env.json", `{broken`)
	if out, _ := e.run(t, "http_environments", nil); !strings.Contains(out, "error: ") {
		t.Errorf("broken env file:\n%s", out)
	}
}

// Every refusal before a send: no url, a bad file or name, unknown env,
// unresolved variables, a non-http URL, a huge body, too many headers, a
// bad header name.
func TestCovAIHTTPSendRefusals(t *testing.T) {
	e := covHTTPSetup(t)
	e.write(t, "a.http", "### one\nGET http://127.0.0.1/{{nope}}\n")
	send := e.tools["http_send"]
	if d := send.Describe(map[string]any{"path": "a.txt"}); !strings.HasPrefix(d, "HTTP request (invalid: ") {
		t.Errorf("invalid card = %q", d)
	}
	many := map[string]any{}
	for i := 0; i < httpAIMaxHeaders+1; i++ {
		many["X-H"+string(rune('a'+i%26))+strings.Repeat("x", i/26)] = "v"
	}
	for _, c := range []struct {
		a    map[string]any
		want string
	}{
		{map[string]any{}, "give path + name"},
		{map[string]any{"path": "a.txt"}, "not a .http or .rest file"},
		{map[string]any{"path": "missing.http"}, "missing.http"},
		{map[string]any{"path": "../x.http"}, ""},
		{map[string]any{"path": "a.http", "name": "two"}, `no request "two" in a.http`},
		{map[string]any{"path": "a.http"}, "give name (one of: one) or index 1..1"},
		{map[string]any{"path": "a.http", "index": 1}, "unresolved variables: nope"},
		{map[string]any{"url": e.srv.URL, "env": "prod"}, `no environment "prod"`},
		{map[string]any{"url": "ftp://x/y"}, "only http:// and https:// URLs"},
		{map[string]any{"url": e.srv.URL, "method": "POST", "body": strings.Repeat("x", httpAIMaxBody+1)}, "the limit is 1 MB"},
		{map[string]any{"url": e.srv.URL, "headers": many}, "headers; the limit is"},
		{map[string]any{"url": e.srv.URL, "headers": map[string]any{"bad name": "v"}}, `header name "bad name" is not valid`},
	} {
		err := send.Validate(c.a)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: %v", c.a, err)
		}
		if _, err := send.Run(context.Background(), c.a); err == nil {
			t.Errorf("%v ran", c.a)
		}
	}
}

// A send: a GET to loopback runs at once; the answer has the head,
// headers and a pretty body cut at the AI limit; the history lists it;
// the last-response tool pages on and shows headers or the summary.
func TestCovAIHTTPSendAndRead(t *testing.T) {
	e := covHTTPSetup(t)
	if _, err := e.run(t, "http_get_last_response", nil); err == nil || !strings.Contains(err.Error(), "no HTTP response in memory") {
		t.Errorf("no response: %v", err)
	}
	if out, err := e.run(t, "http_history", nil); err != nil || out != "no HTTP history yet" {
		t.Errorf("no history: %q %v", out, err)
	}
	dir, _ := configDir()
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"http_ai_max_kb": 1}`), 0o644)
	send := e.tools["http_send"]
	a := map[string]any{"url": e.srv.URL + "/json", "env": "none"}
	if mut, class := send.Gate(a); mut || class != "" {
		t.Errorf("loopback GET gate = %v %q", mut, class)
	}
	if d := send.Describe(a); !strings.HasPrefix(d, "GET "+e.srv.URL+"/json (0 headers)") {
		t.Errorf("card = %q", d)
	}
	out, err := send.Run(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "send", out, "200 OK", "application/json", "response headers:", "body:\n{\n", "… [truncated: ", "http_get_last_response(id:1")
	out, err = e.run(t, "http_get_last_response", map[string]any{"offset": 1000})
	if err != nil || !strings.HasPrefix(out, "body (from offset 1000):") {
		t.Errorf("page on: %.80q %v", out, err)
	}
	out, _ = e.run(t, "http_get_last_response", map[string]any{"part": "headers", "id": 1})
	if !strings.Contains(out, "response headers:") || strings.Contains(out, "body") {
		t.Errorf("headers part: %q", out)
	}
	out, _ = e.run(t, "http_get_last_response", map[string]any{"part": "summary"})
	if strings.Contains(out, "response headers") || !strings.Contains(out, "response #1") {
		t.Errorf("summary part: %q", out)
	}
	for path, want := range map[string]string{"/empty": "body: (empty)", "/bin": "body: binary (image/png"} {
		out, err := send.Run(context.Background(), map[string]any{"url": e.srv.URL + path})
		if err != nil || !strings.Contains(out, want) {
			t.Errorf("%s: %q %v", path, out, err)
		}
	}
	e.write(t, "a.http", "### echo it\nPUT "+e.srv.URL+"/echo\nX-Mode: {{mode}}\n\n")
	e.write(t, "http-client.env.json", `{"dev":{"mode":"fast"}}`)
	b := map[string]any{"path": "a.http", "name": "echo it", "env": "dev"}
	if mut, class := send.Gate(b); !mut || !strings.HasPrefix(class, "http:127.0.0.1:") || !strings.HasSuffix(class, "/write") {
		t.Errorf("loopback PUT gate = %v %q", mut, class)
	}
	if d := send.Describe(b); !strings.Contains(d, "from a.http, env: dev") || !strings.HasPrefix(d, "PUT ") {
		t.Errorf("file card = %q", d)
	}
	out, err = send.Run(context.Background(), b)
	if err != nil || !strings.Contains(out, "PUT fast") {
		t.Errorf("file send: %q %v", out, err)
	}
	// A server that is gone: the failure is the answer, not a Go error.
	gone := httptest.NewServer(http.NotFoundHandler())
	gone.Close()
	out, err = send.Run(context.Background(), map[string]any{"url": gone.URL + "/x"})
	if err != nil || !strings.Contains(out, "request failed: ") {
		t.Errorf("gone server: %q %v", out, err)
	}
	out, err = e.run(t, "http_history", map[string]any{"limit": 2})
	if err != nil || strings.Count(out, "\n") != 2 || !strings.Contains(out, "by ai") {
		t.Errorf("history: %q %v", out, err)
	}
	if out, _ := e.run(t, "http_history", map[string]any{"limit": 100}); !strings.Contains(out, "ERR ") || !strings.Contains(out, "a.http:echo it") {
		t.Errorf("full history:\n%s", out)
	}
}

// The gate classes: private hosts always ask with no class, other hosts
// ask per host, writes per host/write; redirects to another host stop.
func TestCovAIHTTPGateAndHops(t *testing.T) {
	for _, c := range []struct {
		method, url string
		mut         bool
		class       string
	}{
		{"GET", "::bad", true, ""},
		{"GET", "http://10.0.0.5/x", true, ""},
		{"GET", "https://api.example.com/x", true, "http:api.example.com"},
		{"DELETE", "https://api.example.com/x", true, "http:api.example.com/write"},
		{"HEAD", "http://localhost:8080/", false, ""},
	} {
		if mut, class := httpAIGate(c.method, c.url); mut != c.mut || class != c.class {
			t.Errorf("%s %s = %v %q", c.method, c.url, mut, class)
		}
	}
	h := httpAIHooks("GET", "https://api.example.com/x")
	if h.CheckHop("https://API.example.com/y") != nil || h.CheckHop("http://localhost/x") != nil {
		t.Error("same host / loopback GET redirect stopped")
	}
	if err := h.CheckHop("https://other.example.com/?token=abc"); err == nil || !strings.Contains(err.Error(), "needs its own approval") {
		t.Errorf("other host: %v", err)
	}
	if h.CheckHop("::bad") == nil {
		t.Error("bad redirect URL")
	}
	if !httpAIHooks("GET", "http://10.0.0.5/").AllowIP(nil) {
		t.Error("an allowed private host must dial its own IP")
	}
	r := &httpclient.Response{Status: 200, Body: []byte("x"), Charset: "latin-1", Truncated: true, MaxBody: 1024, Headers: make([]httpclient.Header, httpAIMaxRespHdrs+2)}
	if out := httpAIResultText(3, r, "body", 0, 100); !strings.Contains(out, "… 2 more") || !strings.Contains(out, "no content type") || !strings.Contains(out, "http_max_body_mb") {
		t.Errorf("result text:\n%s", out)
	}
	if out := httpAIResultText(3, r, "body", 50, 100); !strings.HasPrefix(out, "body (from offset 1):") {
		t.Errorf("offset past the end: %q", out)
	}
}

// http_save_request: required args, the file kind, append, replace, a
// name clash and a missing name; it writes through write_file.
func TestCovAIHTTPSaveRequest(t *testing.T) {
	e := covHTTPSetup(t)
	save := e.tools["http_save_request"]
	for _, a := range []map[string]any{
		{"path": "a.http"}, {"path": "a.txt", "name": "x", "url": "u"}, {"path": "../a.http", "name": "x", "url": "u"}, {"path": "a.http", "name": "x"},
	} {
		if save.Validate(a) == nil {
			t.Errorf("%v accepted", a)
		}
		if _, err := save.Run(context.Background(), a); err == nil {
			t.Errorf("%v ran", a)
		}
	}
	a := map[string]any{"path": "api/a.http", "name": "list", "method": "post", "url": "{{host}}/items",
		"headers": map[string]any{"B": "2", "A": "1"}, "body": "{}"}
	if mut, class := save.Gate(a); !mut || class != ai.ClassFileWrite {
		t.Errorf("gate = %v %q", mut, class)
	}
	if save.Describe(a) != "Add request list in api/a.http" || save.Describe(map[string]any{"replace": true, "name": "x", "path": "p"}) != "Change request x in p" {
		t.Error("save cards")
	}
	if _, err := save.Run(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	text := e.wrote["api/a.http"]
	if !strings.Contains(text, "POST {{host}}/items") || strings.Index(text, "A: 1") > strings.Index(text, "B: 2") {
		t.Errorf("new file:\n%s", text)
	}
	e.write(t, "api/a.http", "### old\nGET http://x/old")
	if _, err := save.Run(context.Background(), a); err != nil || !strings.HasPrefix(e.wrote["api/a.http"], "### old\nGET http://x/old\n\n") {
		t.Errorf("append: %v\n%s", err, e.wrote["api/a.http"])
	}
	e.write(t, "api/a.http", e.wrote["api/a.http"])
	if err := save.Validate(a); err == nil || !strings.Contains(err.Error(), "use replace=true") {
		t.Errorf("clash: %v", err)
	}
	a["replace"], a["url"] = true, "{{host}}/v2/items"
	if _, err := save.Run(context.Background(), a); err != nil || !strings.Contains(e.wrote["api/a.http"], "/v2/items") || strings.Contains(e.wrote["api/a.http"], "{{host}}/items\n") {
		t.Errorf("replace: %v\n%s", err, e.wrote["api/a.http"])
	}
	a["name"] = "nope"
	if err := save.Validate(a); err == nil || !strings.Contains(err.Error(), `no request named "nope" to replace`) {
		t.Errorf("replace missing: %v", err)
	}
}

// A request cached for an args map that is gone (Describe without a
// Release) is never served to a new map the runtime put at the same
// address. On macOS the "too many headers" case of
// TestCovAIHTTPSendRefusals got the error of its earlier "a.txt" map.
func TestHTTPAICacheNeverServesARecycledMap(t *testing.T) {
	root := t.TempDir()
	var old []uintptr
	describe := func() {
		a := map[string]any{"path": "a.txt"}
		_ = httpAIGet(root, a) // cached, never released
		old = append(old, httpAIKey(a))
	}
	t.Cleanup(func() {
		for _, k := range old {
			httpAICache.Delete(k)
		}
	})
	for i := 0; i < 200; i++ {
		describe()
		runtime.GC()
		for j := 0; j < 64; j++ {
			b := map[string]any{"url": "http://127.0.0.1/x"}
			if r := httpAIGet(root, b); r.err != nil {
				t.Fatalf("a new map got another map's request: %v", r.err)
			}
			httpAIForget(b)
		}
	}
}
