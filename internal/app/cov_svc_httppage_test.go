package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// covSvcHTTPQuick sends one quick request and waits for its answer.
func covSvcHTTPQuick(t *testing.T, m *Model, line string) *httpRun {
	t.Helper()
	m.promptKind = promptKindHTTPQuick
	c, _ := m.handleHTTPPromptSubmit(line)
	covSvcHTTPRun(t, m, c)
	r := m.httpS().last
	if r == nil || r.resp == nil {
		t.Fatalf("no answer for %q", line)
	}
	return r
}

func covSvcHTTPPage(t *testing.T, m *Model) string {
	t.Helper()
	out, err := m.nvim.EvalLuaString(`for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.api.nvim_buf_get_name(b):match('HTTP Response$') then return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '\n') end
end
return ''`)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The response page: views, copy, reveal, history and env keys, the
// redirect hops, and close.
func TestCovSvcHTTPPageKeys(t *testing.T) {
	m, _, _ := covSvcHTTP(t)
	extra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/hop":
			http.Redirect(w, r, "/final", http.StatusFound)
		case "/final":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/empty":
			w.WriteHeader(http.StatusNoContent)
		case "/png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x00"))
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		}
	}))
	t.Cleanup(extra.Close)
	var clip []string
	defer SetClipboardSink(func(s string) { clip = append(clip, s) })()

	r := covSvcHTTPQuick(t, m, "GET "+extra.URL+"/hop")
	if len(r.resp.Hops) == 0 {
		t.Errorf("no hops: %+v", r.resp)
	}
	if page := covSvcHTTPPage(t, m); !strings.Contains(page, `"ok": true`) {
		t.Errorf("page = %s", page)
	}
	for _, k := range []string{"tab", "2", "view:Hdrs", "view:Raw", "3", "1", "x", "x"} {
		covSvcHTTPRun(t, m, m.httpPageKey(k))
	}
	m.httpPageKey("c")
	m.httpPageKey("C")
	m.httpPageKey("hop:" + extra.URL + "/final")
	if len(clip) != 3 || !strings.Contains(clip[0], `"ok"`) || !strings.HasPrefix(clip[1], "curl") || clip[2] != extra.URL+"/final" {
		t.Errorf("clip = %q", clip)
	}
	m.httpPageKey("s")
	if !m.promptOpen || m.prompt.Value() != "final.json" {
		t.Errorf("save body prompt = %q", m.prompt.Value())
	}
	m.promptOpen = false
	m.httpPageKey("S")
	m.promptOpen = false
	m.httpPageKey("h")
	m.httpPageKey("e")
	m.pickerOpen = false
	if m.httpPageKey("k") != nil || m.httpPageKey("o") != nil || m.httpPageKey("jump") != nil || m.httpPageKey("zz") != nil {
		t.Error("a key with nothing to do did something")
	}
	// An empty body and an image.
	covSvcHTTPQuick(t, m, "GET "+extra.URL+"/empty")
	m.httpPageKey("s")
	if !strings.Contains(covSvcToast(m), "No response body to save") {
		t.Errorf("empty save toast = %q", covSvcToast(m))
	}
	r = covSvcHTTPQuick(t, m, "GET "+extra.URL+"/png")
	if !httpIsImage(r.resp) {
		t.Fatalf("not an image: %q", r.resp.ContentType)
	}
	if httpBodyFileName(r) != "png.png" {
		t.Errorf("image name = %q", httpBodyFileName(r))
	}
	m.httpPageKey("c")
	if !strings.Contains(covSvcToast(m), "Nothing to copy") {
		t.Errorf("binary copy toast = %q", covSvcToast(m))
	}
	// Too many redirects and a closed port are failures with help.
	r = covSvcHTTPQuick(t, m, "GET "+extra.URL+"/loop")
	if r.resp.ErrKind != httpclient.ErrKindRedirects {
		t.Errorf("loop kind = %v", r.resp.ErrKind)
	}
	if help := strings.Join(httpErrorHelp(r), "\n"); !strings.Contains(help, "Too many redirects") {
		t.Errorf("help = %s", help)
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	url := closed.URL
	closed.Close()
	r = covSvcHTTPQuick(t, m, "GET "+url+"/x")
	if help := strings.Join(httpErrorHelp(r), "\n"); !strings.Contains(help, "Could not connect") {
		t.Errorf("dial help = %s", help)
	}
	covSvcHTTPRun(t, m, m.httpPageKey("q"))
	covSvcHTTPRun(t, m, m.httpPageKey("esc"))
}

// A server with a certificate this machine does not trust: k resends
// once without the check, for that host only.
func TestCovSvcHTTPInsecureRetry(t *testing.T) {
	m, _, _ := covSvcHTTP(t)
	tls := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("secret page")) }))
	t.Cleanup(tls.Close)
	r := covSvcHTTPQuick(t, m, "GET "+tls.URL+"/")
	if r.resp.ErrKind != httpclient.ErrKindTLS {
		t.Fatalf("kind = %v (%s)", r.resp.ErrKind, r.resp.Err)
	}
	if help := strings.Join(httpErrorHelp(r), "\n"); !strings.Contains(help, "retry once without TLS") {
		t.Errorf("help = %s", help)
	}
	covSvcHTTPRun(t, m, m.httpPageKey("k"))
	if r := m.httpS().last; r.resp.Status != 200 || !strings.Contains(string(r.resp.Body), "secret page") {
		t.Errorf("insecure retry = %+v", r.resp)
	}
	// The host stays allowed for the session.
	r = covSvcHTTPQuick(t, m, "GET "+tls.URL+"/again")
	if r.resp.Status != 200 {
		t.Errorf("second send = %+v", r.resp)
	}
}
