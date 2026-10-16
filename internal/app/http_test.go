package app

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Every TermocodeHttp* group the .http and page Lua use is defined in
// the colourscheme, and the Lua holds no hex colour (theme tokens only).
func TestHTTPLuaGroupsDefined(t *testing.T) {
	re := regexp.MustCompile(`TermocodeHttp[A-Za-z0-9]+`)
	srcs := httpBufferLua + httpPageLua + strings.Join(httpAllHLs(), " ")
	for _, g := range re.FindAllString(srcs, -1) {
		if !strings.Contains(themeLuaBody, "hi('"+g+"',") {
			t.Errorf("%s is used but not defined in themeLuaBody", g)
		}
	}
	hex := regexp.MustCompile(`#[0-9a-fA-F]{6}\b`)
	for name, src := range map[string]string{"httpBufferLua": httpBufferLua, "httpPageLua": httpPageLua} {
		if m := hex.FindString(src); m != "" {
			t.Errorf("%s holds a hex colour %s; use theme groups", name, m)
		}
		if strings.Contains(src, "`") {
			t.Errorf("%s holds a backtick", name)
		}
	}
	// Only the page's status number is bold.
	bold := regexp.MustCompile(`hi\('(TermocodeHttp[A-Za-z]+)',[^\n]*bold = true`)
	for _, m := range bold.FindAllStringSubmatch(themeLuaBody, -1) {
		if !strings.HasPrefix(m[1], "TermocodeHttpStatus") {
			t.Errorf("%s is bold; only the status number may be", m[1])
		}
	}
}

// httpAllHLs lists the groups Go code names (status / lens colours).
func httpAllHLs() []string {
	out := []string{"TermocodeHttpSummary"}
	for _, st := range []int{200, 301, 404, 500} {
		hl := httpStatusHL(st)
		out = append(out, hl, strings.Replace(hl, "Status", "Lens", 1))
	}
	return out
}

func TestHTTPBufRows(t *testing.T) {
	src := "@host = http://x\n\n### one\n# @name getUser\nGET {{host}}/users/{{id}}\nAuthorization: Bearer {{ token }}\n\n###\n# {{commented}}\nPOST {{host}}/u\n\n{\"a\": \"{{nope}}\", \"g\": \"{{$guid}}\"}\n"
	lines := strings.Split(src, "\n")
	f := httpclient.Parse(src)
	known := func(n string) bool { return n == "host" || n == "token" || strings.HasPrefix(n, "$guid") }
	rows := httpBufRows(f, lines, known, func(r *httpclient.Request) [][]string {
		return [][]string{{"lens:" + r.Label(), "TermocodeHttpLens"}}
	})
	got := map[int]map[string]any{}
	for _, r := range rows {
		got[r["l"].(int)] = r
	}
	if got[5]["lens"] == nil || got[10]["lens"] == nil {
		t.Fatalf("lens rows missing: %v", rows)
	}
	if !reflect.DeepEqual(got[5]["curls"], [][]int{{19, 25}}) {
		t.Fatalf("{{id}} undercurl: %v", got[5]["curls"])
	}
	if got[6] != nil {
		t.Fatalf("{{ token }} is known: %v", got[6])
	}
	if got[9] != nil {
		t.Fatalf("comment lines get no undercurl: %v", got[9])
	}
	if !reflect.DeepEqual(got[12]["curls"], [][]int{{7, 15}}) {
		t.Fatalf("{{nope}} undercurl: %v", got[12]["curls"])
	}
}

func testRun() *httpRun {
	body := `{"name":"Ada"}`
	tmpl := httpclient.Request{Method: "POST", URL: "{{host}}/users?token={{token}}", Body: &body, Name: "createUser",
		Headers: []httpclient.Header{{Name: "Authorization", Value: "Bearer {{token}}"}}}
	res := tmpl
	res.URL = "http://localhost:8080/users?token=sekrit-token-123"
	res.Headers = []httpclient.Header{{Name: "Authorization", Value: "Bearer sekrit-token-123"}}
	resp := &httpclient.Response{Status: 500, StatusText: "Internal Server Error", Proto: "HTTP/1.1", Elapsed: 41 * time.Millisecond,
		Size: 96, ContentType: "application/json", IsJSON: true, Body: []byte(`{"error":"null value in column \"email\""}`),
		ReqHeaders: []httpclient.Header{{Name: "authorization", Value: "Bearer sekrit-token-123"}, {Name: "user-agent", Value: "termocode/test"}},
		Headers:    []httpclient.Header{{Name: "content-type", Value: "application/json"}, {Name: "set-cookie", Value: "s=sekrit-cookie; Path=/"}},
		Hops:       []httpclient.Hop{{Status: 301, URL: "http://localhost:8080/users"}},
		Timing:     httpclient.Timing{Connect: 2 * time.Millisecond, FirstByte: 30 * time.Millisecond, Total: 41 * time.Millisecond}}
	return &httpRun{rr: httpclient.Resolved{Request: res, Template: tmpl}, resp: resp, file: "/ws/api/users.http", label: "createUser", by: "ai"}
}

// unicodeIcons switches the glyph tier for a test.
func unicodeIcons(t *testing.T) {
	old := theme.CurrentIconMode
	theme.CurrentIconMode = theme.IconModeUnicode
	t.Cleanup(func() { theme.CurrentIconMode = old })
}

func TestBuildHTTPPageViews(t *testing.T) {
	unicodeIcons(t)
	run := testRun()
	p := buildHTTPPage(run, httpViewBody, false, false)
	if p.lines[0] != "POST http://localhost:8080/users?token=[REDACTED]" {
		t.Fatalf("row 1 %q", p.lines[0])
	}
	if p.lines[1] != "500 Internal Server Error · 41 ms · 96 B · json · 1 redirect" || p.head["st_n"] != 3 || p.head["st_hl"] != "TermocodeHttpStatusErr" {
		t.Fatalf("row 2 %q %v %v", p.lines[1], p.head["st_n"], p.head["st_hl"])
	}
	if p.head["by"] != "by AI" || p.head["name"] != "createUser" || p.head["syntax"] != "json" {
		t.Fatalf("head %v", p.head)
	}
	text := strings.Join(p.lines, "\n")
	if !strings.Contains(text, "{\n  \"error\": \"null value in column \\\"email\\\"\"\n}") {
		t.Fatalf("pretty body missing:\n%s", text)
	}
	p = buildHTTPPage(run, httpViewHeaders, false, false)
	text = strings.Join(p.lines, "\n")
	for _, want := range []string{"REQUEST", "authorization: Bearer [REDACTED]", "RESPONSE  HTTP/1.1 500", "set-cookie: s=[REDACTED]; Path=/", "TIMING  connect 2 · first byte 30 · total 41 ms", "↳ 301 → http://localhost:8080/users"} {
		if !strings.Contains(text, want) {
			t.Errorf("headers view lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "sekrit") {
		t.Fatalf("headers view leaks a secret:\n%s", text)
	}
	p = buildHTTPPage(run, httpViewHeaders, true, false)
	if !strings.Contains(strings.Join(p.lines, "\n"), "Bearer sekrit-token-123") {
		t.Fatal("x reveals the values")
	}
	p = buildHTTPPage(run, httpViewRaw, false, false)
	if p.lines[3] != `{"error":"null value in column \"email\""}` {
		t.Fatalf("raw %q", p.lines[3])
	}
}

func TestBuildHTTPPageStates(t *testing.T) {
	unicodeIcons(t)
	run := testRun()
	run.resp = &httpclient.Response{Err: "TLS: x509: certificate signed by unknown authority", ErrKind: httpclient.ErrKindTLS}
	run.by = "user"
	p := buildHTTPPage(run, httpViewBody, false, false)
	text := strings.Join(p.lines, "\n")
	if p.lines[1] != "✘ TLS: x509: certificate signed by unknown authority" || !strings.Contains(text, "k   retry once without TLS verification") {
		t.Fatalf("tls state:\n%s", text)
	}
	if hs := p.head["hints"].([][]string); hs[0][0] != "k" {
		t.Fatalf("hints %v", hs)
	}
	run.by = "ai"
	if p = buildHTTPPage(run, httpViewBody, false, false); strings.Contains(strings.Join(p.lines, "\n"), "retry once") {
		t.Fatal("no insecure retry for AI sends")
	}
	run.resp = nil
	run.progress = 1258291
	p = buildHTTPPage(run, httpViewBody, false, true)
	if p.lines[1] != "⇄ sending… · 1.2 MB received" {
		t.Fatalf("sending %q", p.lines[1])
	}
	run.resp = &httpclient.Response{Status: 200, StatusText: "OK", ContentType: "image/png", IsBinary: true, Size: 48 << 10, Body: []byte{1, 2}}
	p = buildHTTPPage(run, httpViewBody, false, false)
	if p.lines[3] != "binary · image/png · 48.0 KB — s save, o preview" {
		t.Fatalf("binary %q", p.lines[3])
	}
}

func TestHTTPBodyFileName(t *testing.T) {
	r := testRun()
	if got := httpBodyFileName(r); got != "users.json" {
		t.Fatalf("got %q", got)
	}
}

// The ⋮ menu's context section offers the HTTP rows on .http files.
func TestHTTPOverflowMenuRows(t *testing.T) {
	if got := overflowMenuDetectLang("/w/api/users.REST", ""); got != "http" {
		t.Fatalf("lang %q", got)
	}
	var labels []string
	for _, it := range overflowMenuAllItems(map[keymap.Action]string{keymap.ActionHTTPSend: "Ctrl+Alt+R"}) {
		if it.visible != nil && it.visible(overflowMenuContext{Lang: "http"}) && it.group == groupContext {
			labels = append(labels, it.label+"|"+it.shortcut)
		}
	}
	want := []string{"Send Request|Ctrl+Alt+R", "Copy as curl|", "Select Environment…|"}
	if !reflect.DeepEqual(labels, want) {
		t.Fatalf("context rows %v, want %v", labels, want)
	}
}

func TestHTTPPlaceholderLocs(t *testing.T) {
	got := httpPlaceholderLocs("a {{ x }} {{$guid}} {{a{b}} }}")
	want := [][4]int{{2, 9, 5, 6}, {10, 19, 12, 17}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
