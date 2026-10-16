package testkit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
)

// HTTP client behaviour (plan HC-4 … HC-6): .http buffers, the HTTP
// Response page and the HTTP commands, through the real app and nvim.
// Every request goes to an httptest server on 127.0.0.1: no network.

// httpAPI is a small test API. hits counts requests per path.
type httpAPI struct {
	srv  *httptest.Server
	hits map[string]*atomic.Int32
}

func (a *httpAPI) count(p string) int32 {
	if c, ok := a.hits[p]; ok {
		return c.Load()
	}
	return 0
}

func newHTTPAPI(t *testing.T, tlsServer bool) *httpAPI {
	t.Helper()
	a := &httpAPI{hits: map[string]*atomic.Int32{}}
	for _, p := range []string{"/users/42", "/users", "/slow", "/png", "/old", "/x"} {
		a.hits[p] = &atomic.Int32{}
	}
	release := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, ok := a.hits[r.URL.Path]; ok {
			c.Add(1)
		}
		switch r.URL.Path {
		case "/users/42":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Set-Cookie", "session=cookie-secret-77; Path=/")
			io.WriteString(w, `{"id":42,"name":"Ada Lovelace","roles":["admin","dev"]}`)
		case "/users":
			b, _ := io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			if strings.Contains(string(b), `"email"`) {
				w.WriteHeader(201)
				io.WriteString(w, `{"id":43}`)
				return
			}
			w.WriteHeader(500)
			io.WriteString(w, `{"error":"null value in column \"email\" violates not-null constraint"}`)
		case "/slow":
			select {
			case <-r.Context().Done():
			case <-release:
			}
		case "/png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
		case "/old":
			http.Redirect(w, r, "/users/42", 301)
		default:
			http.NotFound(w, r)
		}
	})
	if tlsServer {
		a.srv = httptest.NewTLSServer(h)
	} else {
		a.srv = httptest.NewServer(h)
	}
	t.Cleanup(func() { close(release); a.srv.Close() })
	return a
}

// usersHTTP is api/users.http for the test API. Request lines: 5
// (getUser), 10 (createUser), 18 (an unknown {{nope}}), 21 (slow),
// 24 (png), 27 (redirect).
func usersHTTP(base string) string {
	return "@host = " + base + "\n" +
		"\n" +
		"### Get one user\n" +
		"# @name getUser\n" +
		"GET {{host}}/users/42\n" +
		"Accept: application/json\n" +
		"\n" +
		"###\n" +
		"# @name createUser\n" +
		"POST {{host}}/users\n" +
		"Content-Type: application/json\n" +
		"\n" +
		"{\n" +
		"  \"mail\": \"ada@example.com\"\n" +
		"}\n" +
		"\n" +
		"###\n" +
		"GET {{host}}/x/{{nope}}\n" +
		"\n" +
		"###\n" +
		"GET {{host}}/slow\n" +
		"\n" +
		"###\n" +
		"GET {{host}}/png\n" +
		"\n" +
		"###\n" +
		"GET {{host}}/old\n"
}

// httpDriver starts the app on a workspace with api/users.http.
func httpDriver(t *testing.T, base string, w, h int, config string) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	files := textFiles()
	files["api/users.http"] = usersHTTP(base)
	opt := Options{W: w, H: h, Nvim: true, Files: files}
	if config != "" {
		opt.Config = map[string]string{"config.json": config}
	}
	d := New(t, opt)
	open(d, "api/users.http")
	return d
}

// httpCursor puts the cursor on a line of the current window.
func httpCursor(d *Driver, line int) {
	d.T.Helper()
	lua(d, `vim.api.nvim_win_set_cursor(0, { ..., 0 }); return ''`, line)
}

// httpLensRow is the lens text the app gave line l of the .http buffer.
func httpLensRow(d *Driver, l int) string {
	return lua(d, `local l = ...
local t = _G._termocode_http_bufs and _G._termocode_http_bufs[vim.api.nvim_get_current_buf()]
if not t or not t[l] then return '' end
local out = {}
for _, c in ipairs(t[l].lens or {}) do out[#out + 1] = c[1] end
if t[l].curls then out[#out + 1] = ' curls=' .. #t[l].curls end
return table.concat(out)`, l)
}

// httpWins is how many windows the current tab has.
func httpWins(d *Driver) string { return lua(d, `return tostring(#vim.api.nvim_tabpage_list_wins(0))`) }

// httpFocusPage moves the cursor into the page window.
func httpFocusPage(d *Driver) {
	d.T.Helper()
	lua(d, `local b = vim.g.termocode_http_buf
for _, w in ipairs(vim.fn.win_findbuf(b)) do vim.api.nvim_set_current_win(w) end
return ''`)
	d.Settle(100 * time.Millisecond)
}

func httpPageText(d *Driver) string {
	return lua(d, `local b = vim.g.termocode_http_buf
if not (b and vim.api.nvim_buf_is_valid(b)) then return '' end
return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '\n')`)
}

func waitPage(d *Driver, want string) {
	d.T.Helper()
	if !d.WaitFor(func() bool { return strings.Contains(httpPageText(d), want) }, 8*time.Second) {
		d.T.Fatalf("page never showed %q; page:\n%s\nscreen:\n%s", want, httpPageText(d), screen(d))
	}
}

// HC-4: the .http buffer gets the filetype, the lens on every request
// line and an undercurl under an unknown {{name}}.
func TestHTTPBufferLens(t *testing.T) {
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 140, 36, "")
	if ft := lua(d, `return vim.bo.filetype`); ft != "http" {
		t.Fatalf("filetype %q", ft)
	}
	if !d.WaitFor(func() bool { return strings.Contains(httpLensRow(d, 5), "send Ctrl+Alt+R") }, 5*time.Second) {
		t.Fatalf("no lens on line 5: %q\n%s", httpLensRow(d, 5), screen(d))
	}
	if !strings.Contains(httpLensRow(d, 10), "send") {
		t.Fatalf("no lens on line 10: %q", httpLensRow(d, 10))
	}
	if r := httpLensRow(d, 18); !strings.Contains(r, "curls=1") {
		t.Fatalf("{{nope}} must get an undercurl: %q", r)
	}
	if r := httpLensRow(d, 6); r != "" {
		t.Fatalf("a header line has no lens: %q", r)
	}
	if !d.WaitForText("send Ctrl+Alt+R", 3*time.Second) {
		t.Fatalf("lens not on screen:\n%s", screen(d))
	}
	if g := lua(d, `return vim.fn.synIDattr(vim.fn.synID(5, 1, 1), 'name')`); g != "TermocodeHttpMethod" {
		t.Fatalf("syntax group at the method: %q", g)
	}
	// Typing changes the table (150 ms debounce): a new request line.
	lua(d, `vim.api.nvim_buf_set_lines(0, -1, -1, false, { '', '###', 'GET {{host}}/users/42' }); return ''`)
	n := lua(d, `return tostring(vim.api.nvim_buf_line_count(0))`)
	var last int
	for _, c := range n {
		last = last*10 + int(c-'0')
	}
	if !d.WaitFor(func() bool { return strings.Contains(httpLensRow(d, last), "send") }, 5*time.Second) {
		t.Fatalf("the new request got no lens (line %d)", last)
	}
}

// A click on the lens sends that request; a click on the text does not.
func TestHTTPLensClick(t *testing.T) {
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 140, 36, "")
	if !d.WaitForText("send Ctrl+Alt+R", 5*time.Second) {
		t.Fatalf("no lens:\n%s", screen(d))
	}
	clickText(d, "GET {{host}}/users/42")
	d.Settle(200 * time.Millisecond)
	if api.count("/users/42") != 0 {
		t.Fatal("a click on the text sent the request")
	}
	clickText(d, "send Ctrl+Alt+R")
	waitPage(d, "200 OK")
	if api.count("/users/42") != 1 {
		t.Fatalf("hits %d", api.count("/users/42"))
	}
}

// The ASCII icon tier draws the lens with ">".
func TestHTTPBufferLensASCII(t *testing.T) {
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 140, 36, `{"icons":"ascii"}`)
	if !d.WaitFor(func() bool { return strings.HasPrefix(httpLensRow(d, 5), " > send") }, 5*time.Second) {
		t.Fatalf("ascii lens: %q", httpLensRow(d, 5))
	}
}

// HC-5: send from the .http buffer; the page opens in a split at ≥ 120
// editor columns; Tab / 1 switch views; q closes the page.
func TestHTTPSendOpensPageSplit(t *testing.T) {
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 170, 40, "")
	httpCursor(d, 5)
	d.Key("alt+ctrl+r")
	waitPage(d, "200 OK · ")
	if !d.WaitForText(`"name": "Ada Lovelace"`, 5*time.Second) {
		t.Fatalf("body not on screen:\n%s", screen(d))
	}
	if n := httpWins(d); n != "2" {
		t.Fatalf("want a split (2 windows), got %s", n)
	}
	if !d.WaitFor(func() bool { return strings.Contains(httpLensRow(d, 5), "200 · ") }, 5*time.Second) {
		t.Fatalf("lens shows no result: %q", httpLensRow(d, 5))
	}
	if !strings.Contains(screen(d), "GET 200") {
		t.Errorf("status chip missing:\n%s", screen(d))
	}
	httpFocusPage(d)
	d.Key("tab")
	waitPage(d, "RESPONSE  HTTP/1.1 200")
	if strings.Contains(httpPageText(d), "cookie-secret-77") {
		t.Fatal("the headers view shows a cookie value")
	}
	d.Key("1")
	waitPage(d, `"id": 42`)
	d.Key("q")
	if !d.WaitFor(func() bool { return httpPageText(d) == "" }, 3*time.Second) {
		t.Fatal("q did not close the page")
	}
	if !waitTabs(d, "api/users.http") {
		got, _ := tabs(d)
		t.Fatalf("tabs after q: %v", got)
	}
	if n := httpWins(d); n != "1" {
		t.Fatalf("the split stays after q: %s windows", n)
	}
}

// Narrow editor: the page takes the window; q goes back to the file.
func TestHTTPSendOpensPageSameWindow(t *testing.T) {
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 100, 30, "")
	httpCursor(d, 5)
	d.Palette("http-send")
	waitPage(d, "200 OK · ")
	if n := httpWins(d); n != "1" {
		t.Fatalf("want one window, got %s", n)
	}
	if _, a := tabs(d); !strings.HasSuffix(a, "HTTP Response") {
		t.Fatalf("active tab %q", a)
	}
	d.Key("r") // resend from the page
	if !d.WaitFor(func() bool { return api.count("/users/42") == 2 }, 5*time.Second) {
		t.Fatalf("r did not resend: %d", api.count("/users/42"))
	}
	d.Key("q")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "api/users.http" }, 3*time.Second) {
		_, a := tabs(d)
		t.Fatalf("q went to %q", a)
	}
}

// Esc while a slow request runs cancels it.
func TestHTTPCancel(t *testing.T) {
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 100, 30, "")
	httpCursor(d, 21)
	d.Palette("http-send")
	waitPage(d, "sending")
	d.Key("esc")
	waitPage(d, "cancelled after")
}

// The timeout setting ends a slow request with the timeout row.
func TestHTTPTimeout(t *testing.T) {
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 100, 30, `{"http_timeout":1}`)
	httpCursor(d, 21)
	d.Palette("http-send")
	waitPage(d, "timed out after 1 s")
	if !strings.Contains(httpPageText(d), "setting http_timeout") {
		t.Fatalf("timeout help missing:\n%s", httpPageText(d))
	}
}

// A TLS error offers k; k retries once without verification.
func TestHTTPTLSRetry(t *testing.T) {
	api := newHTTPAPI(t, true)
	d := httpDriver(t, api.srv.URL, 100, 30, "")
	httpCursor(d, 5)
	d.Palette("http-send")
	waitPage(d, "TLS: ")
	if !strings.Contains(httpPageText(d), "k   retry once without TLS verification") {
		t.Fatalf("no k hint:\n%s", httpPageText(d))
	}
	d.Key("k")
	waitPage(d, "200 OK")
}

// Redirect hops are listed; s saves the body; c copies it; a PNG shows
// the binary note.
func TestHTTPPageActions(t *testing.T) {
	var copied atomic.Value
	copied.Store("")
	restore := app.SetClipboardSink(func(s string) { copied.Store(s) })
	defer restore()
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 100, 30, "")
	httpCursor(d, 27)
	d.Palette("http-send")
	waitPage(d, "1 redirect")
	if !strings.Contains(httpPageText(d), "301 → "+api.srv.URL+"/users/42") {
		t.Fatalf("hop missing:\n%s", httpPageText(d))
	}
	d.Key("c")
	if !d.WaitFor(func() bool { return strings.Contains(copied.Load().(string), `"name": "Ada Lovelace"`) }, 3*time.Second) {
		t.Fatalf("clipboard got %q", copied.Load())
	}
	d.Key("s")
	if !d.WaitForText("File:", 3*time.Second) {
		t.Fatalf("no save prompt:\n%s", screen(d))
	}
	d.Key("enter")
	if !eventually(d, func() bool { return strings.Contains(diskFile(d, "42.json"), "Ada Lovelace") }) {
		t.Fatalf("42.json not written; files: %v", dirNames(d.Dir))
	}
	d.Action("open:" + filepath.Join(d.Dir, "api/users.http"))
	waitTabs(d, "api/users.http", "HTTP Response")
	open(d, "api/users.http")
	httpCursor(d, 24)
	d.Palette("http-send")
	waitPage(d, "binary · image/png")
}

func dirNames(dir string) []string {
	es, _ := os.ReadDir(dir)
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

// HC-6: Quick Request → prompt → page; History lists it and Enter sends
// it again.
func TestHTTPQuickRequestAndHistory(t *testing.T) {
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 120, 34, "")
	d.Palette("http-quick")
	if !d.WaitForText("Request:", 3*time.Second) {
		t.Fatalf("no prompt:\n%s", screen(d))
	}
	d.Type("GET " + api.srv.URL + "/users/42")
	d.Key("enter")
	waitPage(d, "200 OK")
	d.Palette("http-history")
	if !d.WaitForText("GET /users/42 · 200", 3*time.Second) {
		t.Fatalf("history row missing:\n%s", screen(d))
	}
	d.Key("enter")
	if !d.WaitFor(func() bool { return api.count("/users/42") == 2 }, 5*time.Second) {
		t.Fatalf("history Enter did not send again: %d", api.count("/users/42"))
	}
	hist, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "http", "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(hist), "cookie-secret-77") {
		t.Fatalf("history holds a cookie value:\n%s", hist)
	}
	st, _ := os.Stat(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "http", "history.jsonl"))
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("history mode %v", st.Mode())
	}
}

// HC-6: the environment picker switches env; the lens says so and the
// next send resolves with it.
func TestHTTPEnvironmentPicker(t *testing.T) {
	api := newHTTPAPI(t, false)
	if testing.Short() {
		t.Skip("starts nvim")
	}
	files := textFiles()
	files["api/env.http"] = "### one\nGET {{host}}/users/42\n"
	files["http-client.env.json"] = `{"dev": {"host": "http://127.0.0.1:9"}, "staging": {"host": "` + api.srv.URL + `"}}`
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: files})
	open(d, "api/env.http")
	d.Palette("http-env-select")
	if !d.WaitForText("staging", 3*time.Second) {
		t.Fatalf("env picker:\n%s", screen(d))
	}
	d.Type("staging")
	d.Key("enter")
	if !d.WaitFor(func() bool { return strings.Contains(httpLensRow(d, 2), "env staging") }, 5*time.Second) {
		t.Fatalf("lens: %q", httpLensRow(d, 2))
	}
	httpCursor(d, 2)
	d.Palette("http-send")
	waitPage(d, "200 OK")
}

// HC-6: Import from curl inserts a ### block into the open .http file.
func TestHTTPImportCurl(t *testing.T) {
	api := newHTTPAPI(t, false)
	d := httpDriver(t, api.srv.URL, 120, 34, "")
	d.Palette("http-import-curl")
	if !d.WaitForText("curl:", 3*time.Second) {
		t.Fatalf("no prompt:\n%s", screen(d))
	}
	d.Type(`curl -X PUT https://api.example.com/items/1 -H 'Accept: application/json' -d '{"a":1}'`)
	d.Key("enter")
	if !d.WaitFor(func() bool {
		return strings.Contains(bufLines(d), "###\nPUT https://api.example.com/items/1\nAccept: application/json\n\n{\"a\":1}")
	}, 3*time.Second) {
		t.Fatalf("buffer:\n%s", bufLines(d))
	}
	if c := cursor(d); !strings.HasPrefix(c, "30:") {
		t.Fatalf("cursor %s, want the new request line", c)
	}
}

// keymap.json can move the send key.
func TestHTTPKeymapOverride(t *testing.T) {
	api := newHTTPAPI(t, false)
	if testing.Short() {
		t.Skip("starts nvim")
	}
	files := textFiles()
	files["api/users.http"] = usersHTTP(api.srv.URL)
	d := New(t, Options{W: 100, H: 30, Nvim: true, Files: files, Config: map[string]string{"keymap.json": `{"f9": "HTTPSend"}`}})
	open(d, "api/users.http")
	httpCursor(d, 5)
	d.Key("f9")
	waitPage(d, "200 OK")
}
