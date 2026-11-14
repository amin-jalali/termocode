package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// The page keys after a real answer: views by tab, number and name,
// copy body and curl, save body and request, history, environments,
// reveal, jump to the request, copy a hop URL, close.
func TestCovHTTPPageKeys(t *testing.T) {
	m, root, clip := covHTTP(t)
	file := filepath.Join(root, "api.http")
	f, req := chainDepReq(t, m, file, "missing")
	covSvcHTTPRun(t, m, m.httpSendRequest(file, f.Vars, req, ""))
	s := m.httpS()
	if s.shown == nil || s.shown.resp.Status != 404 {
		t.Fatalf("shown = %+v", s.shown)
	}
	// Views.
	m.httpPageKey("tab")
	if s.view != 1 {
		t.Errorf("tab: view %d", s.view)
	}
	m.httpPageKey("3")
	if s.view != 2 {
		t.Errorf("3: view %d", s.view)
	}
	m.httpPageKey("view:Hdrs")
	if s.view != httpViewHeaders || !strings.Contains(covSvcHTTPPage(t, m), "RESPONSE  HTTP/1.1 404") {
		t.Errorf("view:Hdrs: %d\n%s", s.view, covSvcHTTPPage(t, m))
	}
	m.httpPageKey("view:body")
	if s.view != httpViewBody {
		t.Errorf("view:body: %d", s.view)
	}
	// Reveal toggles the secret header values.
	m.httpPageKey("x")
	if !s.reveal {
		t.Error("x did not reveal")
	}
	m.httpPageKey("x")
	// Copies.
	m.httpPageKey("c")
	if len(*clip) != 1 || !strings.Contains((*clip)[0], "404 page not found") {
		t.Errorf("clip = %q", *clip)
	}
	m.httpPageKey("C")
	if len(*clip) != 2 || !strings.HasPrefix((*clip)[1], "curl") {
		t.Errorf("clip = %q", *clip)
	}
	m.httpPageKey("hop:http://x/y")
	if len(*clip) != 3 || (*clip)[2] != "http://x/y" {
		t.Errorf("clip = %q", *clip)
	}
	// Prompts and pickers.
	m.httpPageKey("s")
	if !m.promptOpen || m.promptKind != promptKindHTTPSaveBody {
		t.Error("s: no save body prompt")
	}
	m.httpPageKey("S")
	if !m.promptOpen || m.promptKind != promptKindHTTPSaveName {
		t.Error("S: no save request prompt")
	}
	m.httpPageKey("h")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPHistory {
		t.Error("h: no history")
	}
	m.httpPageKey("e")
	if !m.pickerOpen || m.pickerKind != pickerKindHTTPEnv {
		t.Error("e: no env picker")
	}
	// k and o do nothing for a plain answer; unknown keys neither.
	if m.httpPageKey("k") != nil || m.httpPageKey("o") != nil || m.httpPageKey("?") != nil {
		t.Error("k / o / ? did something")
	}
	// r sends again.
	covSvcHTTPRun(t, m, m.httpPageKey("r"))
	if s.last.id == s.ring[0].id {
		t.Error("r did not send")
	}
	// jump opens the .http file at the request line.
	m.httpPageKey("jump")
	var name string
	_ = m.nvim.ExecLuaResult(`return vim.api.nvim_buf_get_name(0) .. ':' .. vim.api.nvim_win_get_cursor(0)[1]`, &name)
	if name != fmt.Sprintf("%s:%d", file, s.shown.line) {
		t.Errorf("jump: %q", name)
	}
	// The notify handler routes page keys too.
	notifyHandlers["termocode_http_key"](m, []any{"2"})
	if s.view != httpViewHeaders {
		t.Errorf("notify key: view %d", s.view)
	}
	// q and esc close the page.
	m.httpRedrawPage()
	m.httpPageKey("q")
	if covSvcHTTPPage(t, m) != "" {
		t.Error("q left the page")
	}
	m.httpRedrawPage()
	m.httpPageKey("esc")
	if covSvcHTTPPage(t, m) != "" {
		t.Error("esc left the page")
	}
	// No shown run: redraw, jump and copy do nothing / say so.
	s.shown = nil
	if m.httpRedrawPage() != nil || m.httpJumpToRequest() != nil || m.httpPreviewImage() != nil || m.httpRetryInsecure() != nil {
		t.Error("no run did something")
	}
	covHTTPReset(m)
	m.httpCopyBody()
	if !strings.Contains(covHTTPToast(m), "Nothing to copy") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// Without an editor the page calls are no-ops.
	nv := m.nvim
	m.nvim = nil
	if m.httpClosePage() != nil || m.httpPageAppend(httpAppendMsg{}) != nil || m.httpShowLive() != nil {
		t.Error("no editor did something")
	}
	if a, b := m.httpPageOpen(); a || b {
		t.Error("page open without an editor")
	}
	m.httpUpdateHead()
	m.nvim = nv
}

// Esc while a request runs cancels it; the page keeps the previous
// answer and says cancelled.
func TestCovHTTPPageEscCancels(t *testing.T) {
	m, root, _ := covHTTP(t)
	file := filepath.Join(root, "api.http")
	f, req := chainDepReq(t, m, file, "missing")
	covSvcHTTPRun(t, m, m.httpSendRequest(file, f.Vars, req, ""))
	srv := strings.TrimSuffix(m.httpS().last.rr.URL, "/nope")
	cmd := m.httpSendRequest("", nil, httpclient.Request{Method: "GET", URL: srv + "/slow"}, "")
	s := m.httpS()
	if s.running == nil {
		t.Fatal("not running")
	}
	m.httpUpdateHead()
	m.httpShowLive()
	if got := strings.Join(m.httpStatusTexts(), ""); !strings.Contains(got, "sending") {
		t.Errorf("chip %q", got)
	}
	m.httpPageKey("esc")
	covSvcHTTPRun(t, m, cmd)
	if s.running != nil || s.last.resp.ErrKind != httpclient.ErrKindCancelled {
		t.Fatalf("last = %+v", s.last.resp)
	}
	// The page still shows the 404 body, with row 2 saying cancelled.
	if page := covSvcHTTPPage(t, m); !strings.Contains(page, "404 page not found") || !strings.Contains(page, "cancel") {
		t.Errorf("page:\n%s", page)
	}
	// The lens says ERR.
	if got := strings.Join(m.httpStatusTexts(), ""); !strings.Contains(got, "ERR") {
		t.Errorf("chip %q", got)
	}
	m.httpRecordLens(&httpRun{file: file, label: "x", resp: s.last.resp})
	if l := s.lens[file+"\x00x"]; l.text != "ERR" {
		t.Errorf("lens = %+v", l)
	}
	if got := strings.Join(httpErrorHelp(s.last), "\n"); !strings.Contains(got, "cancelled") {
		t.Errorf("help:\n%s", got)
	}
}

// A TLS failure offers k: the request goes out once more without
// certificate checks for that host.
func TestCovHTTPPageRetryInsecure(t *testing.T) {
	m, _, _ := covHTTP(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("tls ok")) }))
	t.Cleanup(srv.Close)
	covSvcHTTPRun(t, m, m.httpSendRequest("", nil, httpclient.Request{Method: "GET", URL: srv.URL + "/"}, ""))
	s := m.httpS()
	if s.shown.resp.ErrKind != httpclient.ErrKindTLS {
		t.Fatalf("err kind %v (%s)", s.shown.resp.ErrKind, s.shown.resp.Err)
	}
	if got := strings.Join(httpErrorHelp(s.shown), "\n"); !strings.Contains(got, "k   retry once") {
		t.Errorf("help:\n%s", got)
	}
	s.running = s.shown
	covHTTPReset(m)
	m.httpPageKey("k")
	if !strings.Contains(covHTTPToast(m), "still running") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	s.running = nil
	covSvcHTTPRun(t, m, m.httpPageKey("k"))
	if s.last.resp.Status != 200 || !s.insecure[httpclient.HostKey(srv.URL)] {
		t.Fatalf("retry: %+v", s.last.resp)
	}
	// The next send to the host skips the checks too.
	covSvcHTTPRun(t, m, m.httpSendRequest("", nil, httpclient.Request{Method: "GET", URL: srv.URL + "/again"}, ""))
	if s.last.resp.Status != 200 {
		t.Errorf("again: %+v", s.last.resp)
	}
}

// buildHTTPPage for the rarer answers: no answer, redirect hops in the
// headers view, binary raw view, every error kind, the full timing.
func TestCovHTTPPageBuildRare(t *testing.T) {
	unicodeIcons(t)
	run := testRun()
	run.resp.Proto = ""
	p := buildHTTPPage(run, httpViewHeaders, false, false)
	text := strings.Join(p.lines, "\n")
	if !strings.Contains(text, "RESPONSE  HTTP 500") || !strings.Contains(text, "301 → http://localhost:8080/users") {
		t.Errorf("headers view:\n%s", text)
	}
	run.resp = &httpclient.Response{Status: 200, IsBinary: true, Size: 3, Body: []byte{0, 1, 2}}
	if got := strings.Join(buildHTTPPage(run, httpViewRaw, false, false).lines, "\n"); !strings.Contains(got, "binary · unknown type · 3 B") {
		t.Errorf("raw binary:\n%s", got)
	}
	run.resp = nil
	if p := buildHTTPPage(run, httpViewBody, false, false); p.lines[1] != "" {
		t.Errorf("no answer row 2 %q", p.lines[1])
	}
	for kind, want := range map[httpclient.ErrKind]string{
		httpclient.ErrKindTimeout:   "No answer in time",
		httpclient.ErrKindDial:      "Could not connect",
		httpclient.ErrKindRedirects: "Too many redirects",
		httpclient.ErrKindBlocked:   "private or special",
		httpclient.ErrKindOther:     "The request failed.",
	} {
		r := &httpRun{resp: &httpclient.Response{Err: "x", ErrKind: kind}}
		if got := strings.Join(httpErrorHelp(r), "\n"); !strings.Contains(got, want) {
			t.Errorf("%v: %s", kind, got)
		}
	}
	got := httpTimingText(httpclient.Timing{DNS: time.Millisecond, Connect: 2 * time.Millisecond, TLS: 3 * time.Millisecond, FirstByte: 4 * time.Millisecond, Total: 5 * time.Millisecond})
	if got != "dns 1 · connect 2 · tls 3 · first byte 4 · total 5 ms" {
		t.Errorf("timing %q", got)
	}
	if httpIsImage(&httpclient.Response{ContentType: "text/plain"}) {
		t.Error("text is an image")
	}
}

// A body longer than one batch fills the page in batches; a batch of
// an old page is dropped.
func TestCovHTTPPageBatches(t *testing.T) {
	m, _, _ := covHTTP(t)
	n := 2*httpPageBatch + 10
	lines := make([]string, n)
	var rows []map[string]any
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	rows = append(rows, map[string]any{"l": httpPageBatch + 3, "k": "note"}, map[string]any{"l": 2*httpPageBatch + 5, "k": "note"})
	cmd := m.httpOpenPage(httpPage{head: map[string]any{"req": "GET x", "hints": [][]string{}, "chips": []string{}, "g": map[string]string{}}, lines: lines, rows: rows})
	covSvcHTTPRun(t, m, cmd)
	page := covSvcHTTPPage(t, m)
	if !strings.Contains(page, fmt.Sprintf("line %d", n)) || strings.Count(page, "\n") != n-1 {
		t.Fatalf("page has %d lines", strings.Count(page, "\n")+1)
	}
	if m.httpPageAppend(httpAppendMsg{page: m.httpS().pageGen + 1, lines: []string{"stale"}}) != nil {
		t.Error("stale batch applied")
	}
}
