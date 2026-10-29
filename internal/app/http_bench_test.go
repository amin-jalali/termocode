package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// PERF-8: benchmarks of termocode's own work around an HTTP send (the
// server is an httptest one that answers at once, so the numbers are
// termocode's overhead, never the network's).

// httpBenchWS is a workspace with env files on two levels, a selected
// environment, a secret, 1 000 history entries and an .http file of n
// requests (variables, dynamic values and an auth header in each).
type httpBenchWS struct {
	root, file, src string
}

func newHTTPBenchWS(tb testing.TB, n int) httpBenchWS {
	tb.Helper()
	cfg := tb.TempDir()
	tb.Setenv("XDG_CONFIG_HOME", cfg)
	root := tb.TempDir()
	tb.Chdir(root)
	write := func(p, s string) {
		tb.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			tb.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			tb.Fatal(err)
		}
	}
	var env strings.Builder
	env.WriteString(`{"$shared": {"ua": "bench"}, "dev": {"host": "http://127.0.0.1:1"`)
	for i := 0; i < 50; i++ {
		fmt.Fprintf(&env, `, "k%d": "v%d"`, i, i)
	}
	env.WriteString("}}")
	write(filepath.Join(root, httpclient.EnvFile), env.String())
	write(filepath.Join(root, httpclient.PrivateEnvFile), `{"dev": {"token": "s3cr3t-token-123456"}}`)
	write(filepath.Join(root, ".env"), "API_KEY=abc\nOTHER=1\n")
	write(filepath.Join(root, "api", httpclient.EnvFile), `{"dev": {"path": "/v1"}}`)
	write(filepath.Join(cfg, "termocode", "config.json"), `{"http_history_max": 1000}`)
	if err := httpSetSelectedEnv(root, "dev"); err != nil {
		tb.Fatal(err)
	}
	write(filepath.Join(cfg, "termocode", "ai", "credentials.json"), `{"mcp_secrets": {"pass": "pw-123456789"}}`)
	// 1 000 history entries of about 1 KB.
	var hist strings.Builder
	body := strings.Repeat("x", 900)
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&hist, `{"id":"%012d","time":"2026-10-30T00:00:00Z","by":"user","method":"GET","url":"{{host}}/u/%d","elapsed_ms":3,"size":900,"status":200,"body":"%s"}`+"\n", i, i, body)
	}
	write(httpHistoryPath(), hist.String())
	var src strings.Builder
	src.WriteString("@base = {{host}}{{path}}\n\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&src, "### r%d\n# @name r%d\nPOST {{base}}/users/%d?ts={{$timestamp}}\nAuthorization: Bearer {{token}}\nX-Key: {{$dotenv API_KEY}}\nX-Pass: {{$secret pass}}\nUser-Agent: {{ua}}\nContent-Type: application/json\n\n{\"id\": \"{{$guid}}\", \"n\": %d, \"k\": \"{{k7}}\"}\n\n", i, i, i, i)
	}
	file := filepath.Join(root, "api", "users.http")
	write(file, src.String())
	return httpBenchWS{root: root, file: file, src: src.String()}
}

func BenchmarkHTTPParse200(b *testing.B) {
	ws := newHTTPBenchWS(b, 200)
	b.ReportAllocs()
	for b.Loop() {
		httpclient.Parse(ws.src)
	}
}

// The Go side of a buffer refresh (150 ms debounce after a keystroke):
// parse, resolver, lens + undercurl rows, completion names.
func BenchmarkHTTPBufRefresh200(b *testing.B) {
	ws := newHTTPBenchWS(b, 200)
	lines := strings.Split(ws.src, "\n")
	m := &Model{}
	b.ReportAllocs()
	for b.Loop() {
		f := httpclient.Parse(ws.src)
		rv := httpResolver(ws.file, f.Vars, "")
		httpBufRows(f, lines, rv.Known, func(r *httpclient.Request) [][]string {
			return m.httpLensChunks(ws.file, r.Label(), rv.Env, false)
		})
		rv.Envs.VarNames(rv.Env)
	}
}

// Request to wire, Go side: parse the file, build the resolver, resolve
// (file vars, env, private env, $dotenv, $secret, $guid, $timestamp).
func BenchmarkHTTPResolve(b *testing.B) {
	ws := newHTTPBenchWS(b, 200)
	b.ReportAllocs()
	for b.Loop() {
		f := httpclient.Parse(ws.src)
		req := f.AtLine(f.Requests[100].Line)
		rv := httpResolver(ws.file, f.Vars, "")
		if _, un := rv.Resolve(*req); len(un) > 0 {
			b.Fatal(un)
		}
	}
}

func httpBenchServer(tb testing.TB, body []byte, ct string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", ct)
		_, _ = w.Write(body)
	}))
	tb.Cleanup(srv.Close)
	return srv
}

// httpBenchSend is what startHTTPRun's goroutine does for one request
// of the file, plus the resolve before it: the answer is ready for the
// UI when it returns.
func httpBenchSend(tb testing.TB, ws httpBenchWS, srvURL string, f httpclient.File, req httpclient.Request) *httpclient.Response {
	vars := append([]httpclient.Var{}, f.Vars...)
	vars = append(vars, httpclient.Var{Name: "host", Value: srvURL})
	rv := httpResolver(ws.file, vars, "")
	rr, _ := rv.Resolve(req)
	set := loadHTTPSettings()
	s := newHTTPState()
	run := &httpRun{rr: rr, file: ws.file, line: req.Line, label: req.Label(), env: rv.Env, by: "user"}
	run.resp = s.httpClient(set).Send(context.Background(), rr, httpclient.Hooks{})
	httpRecordHistory(run)
	if run.resp.Err != "" {
		tb.Fatal(run.resp.Err)
	}
	return run.resp
}

// One send of a small JSON answer to a local server, history on.
func BenchmarkHTTPSendSmall(b *testing.B) {
	ws := newHTTPBenchWS(b, 200)
	srv := httpBenchServer(b, []byte(`{"ok":true}`), "application/json")
	f := httpclient.Parse(ws.src)
	b.ReportAllocs()
	for b.Loop() {
		httpBenchSend(b, ws, srv.URL, f, f.Requests[100])
	}
}

// "Send all": every request of a 200-request file, one after another.
func BenchmarkHTTPSendAll200(b *testing.B) {
	ws := newHTTPBenchWS(b, 200)
	srv := httpBenchServer(b, []byte(`{"ok":true}`), "application/json")
	b.ReportAllocs()
	for b.Loop() {
		f := httpclient.Parse(ws.src)
		for _, r := range f.Requests {
			httpBenchSend(b, ws, srv.URL, f, r)
		}
	}
}

// A 5 MB JSON answer, read and recorded.
func BenchmarkHTTPSendBig5MB(b *testing.B) {
	ws := newHTTPBenchWS(b, 1)
	srv := httpBenchServer(b, big5MB(), "application/json")
	f := httpclient.Parse(ws.src)
	b.ReportAllocs()
	for b.Loop() {
		httpBenchSend(b, ws, srv.URL, f, f.Requests[0])
	}
}

func BenchmarkHTTPHistoryAppend1000(b *testing.B) {
	newHTTPBenchWS(b, 1)
	run := &httpRun{rr: httpclient.Resolved{Template: httpclient.Request{Method: "GET", URL: "{{host}}/x"}},
		resp: &httpclient.Response{Status: 200, Body: []byte(strings.Repeat("y", 900)), Size: 900}, by: "user"}
	b.ReportAllocs()
	for b.Loop() {
		httpRecordHistory(run)
	}
}

func BenchmarkHTTPHistoryList1000(b *testing.B) {
	newHTTPBenchWS(b, 1)
	b.ReportAllocs()
	for b.Loop() {
		if l, err := httpHistoryStore().List(); err != nil || len(l) != 1000 {
			b.Fatal(len(l), err)
		}
	}
}

func BenchmarkHTTPPageBuildBinary5MB(b *testing.B) {
	body := make([]byte, 5<<20)
	run := &httpRun{rr: httpclient.Resolved{Request: httpclient.Request{Method: "GET", URL: "http://x/"}},
		resp: &httpclient.Response{Status: 200, StatusText: "OK", Body: body, Size: int64(len(body)), IsBinary: true, ContentType: "image/png"}}
	b.ReportAllocs()
	for b.Loop() {
		buildHTTPPage(run, httpViewBody, false, false)
	}
}
