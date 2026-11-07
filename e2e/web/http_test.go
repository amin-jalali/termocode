//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The HTTP client (WEBP-HTTP) in the desktop's side bar against the fake
// hub: the workspace's .http requests, the environment, saved requests
// and history; a send shows the laptop's answer in the HTTP RESPONSE
// panel as text; a request to a new host says the laptop asks.

func httpFiles() webproto.HTTPFiles {
	return webproto.HTTPFiles{Env: "dev", Envs: []string{"dev", "prod"}, Files: []webproto.HTTPFile{{Path: "api/users.http", Requests: []webproto.HTTPReqItem{
		{Index: 1, Name: "list", Method: "GET", URL: "{{host}}/users", Line: 3},
		{Index: 2, Name: "create", Method: "POST", URL: "{{host}}/users", Line: 8},
	}}}}
}

// click runs an element's click (row actions show on hover only).
func click(p *Page, el string) {
	p.T.Helper()
	p.WaitJS(el)
	Eval[bool](p, `(`+el+`.click(), true)`)
}

// fill types v into an input, select or textarea as the user would.
func fill(p *Page, sel, v string) {
	p.T.Helper()
	p.WaitJS(`document.querySelector(` + jsq(sel) + `)`)
	Eval[bool](p, `(() => { const e = document.querySelector(`+jsq(sel)+`); e.value = `+jsq(v)+`; e.dispatchEvent(new Event(e.tagName === "SELECT" ? "change" : "input", {bubbles: true})); return true; })()`)
}

func openHTTP(t *testing.T) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	c := p.Login("c-http")
	p.TapAct("http")
	reply(t, c, expectOp(t, c, webproto.OpHTTPFiles, `{}`), httpFiles())
	reply(t, c, expectOp(t, c, webproto.OpHTTPSaved, `{}`), webproto.HTTPSaved{Requests: []webproto.HTTPReqItem{{Index: 1, Name: "health", Method: "GET", URL: "http://localhost:8080/health"}}})
	reply(t, c, expectOp(t, c, webproto.OpHTTPHistory, `{}`), webproto.HTTPHistory{Entries: []webproto.HTTPHistItem{{ID: "h1", At: 1, Method: "GET", URL: "{{host}}/users", Status: 200, ElapsedMS: 12, File: "api/users.http", Name: "list", By: "user"}}})
	p.WaitJS(`document.querySelectorAll('.pv[data-view="http"] .tr[data-req]').length === 2 && document.querySelector(".http-env").value === "dev"`)
	return p, c
}

func TestHTTPSendShowsTheLaptopsAnswer(t *testing.T) {
	p, c := openHTTP(t)
	click(p, `document.querySelector('.tr[data-req="api/users.http#1"] [data-act="send"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPSend, `{"path":"api/users.http","index":1}`), webproto.HTTPSent{Run: 7})
	reply(t, c, expectOp(t, c, webproto.OpHTTPResponse, `{"run":7}`), webproto.HTTPResponse{Run: 7, Running: true, Label: "list", Method: "GET", URL: "{{host}}/users", ReqHeaders: []webproto.HTTPHeader{}, Headers: []webproto.HTTPHeader{}})
	p.WaitJS(`document.querySelector('.http-resp [data-act="cancel"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPResponse, `{"run":7}`), webproto.HTTPResponse{Run: 7, By: "web", Label: "list", Method: "GET", URL: "{{host}}/users",
		ReqHeaders: []webproto.HTTPHeader{{Name: "Authorization", Value: "Bearer [REDACTED]"}}, Status: 200, StatusText: "OK", ElapsedMS: 18, Size: 40,
		ContentType: "application/json", Headers: []webproto.HTTPHeader{{Name: "content-type", Value: "application/json"}},
		Body: `{"auth":"Bearer [REDACTED]","html":"<b>x</b>"}`})
	p.WaitJS(`document.querySelector(".http-resp .http-status")?.textContent.includes("200 OK")`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPHistory, `{}`), webproto.HTTPHistory{Entries: []webproto.HTTPHistItem{}})
	// WEBP-10: in a right-to-left page the status, the request and the
	// time stay left to right ("404 Not Found" read "Not 404 Found").
	if bad := Eval[string](p, `[".http-status", ".http-what", ".http-bar .tr-age"].filter((s) => document.querySelector(".http-resp " + s)?.getAttribute("dir") !== "ltr").join(" ")`); bad != "" {
		t.Errorf("not left to right: %s", bad)
	}
	body := Eval[string](p, `document.querySelector(".http-resp .http-body-text").textContent`)
	if !strings.Contains(body, `"auth": "Bearer [REDACTED]"`) || Eval[int](p, `document.querySelectorAll(".http-resp b").length`) != 0 {
		t.Fatalf("body %q (or it became HTML)", body)
	}
	click(p, `document.querySelector('.http-resp [data-tab="headers"]')`)
	p.WaitJS(`document.querySelector(".http-resp").textContent.includes("content-type")`)
	click(p, `document.querySelector('.http-resp [data-act="curl"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPCurl, `{"run":7}`), webproto.HTTPCurl{Curl: "curl -X GET 'http://localhost:8080/users' -H 'Authorization: Bearer {{token}}'"})
	// Save the request under a name, and the body to a file.
	click(p, `document.querySelector('.http-resp [data-act="save"]')`)
	fill(p, ".http-resp .http-in", "list users")
	click(p, `document.querySelector('.http-resp .http-form [data-act="ok"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPSave, `{"run":7,"name":"list users"}`), nil)
	reply(t, c, expectOp(t, c, webproto.OpHTTPSaved, `{}`), webproto.HTTPSaved{Requests: []webproto.HTTPReqItem{}})
	click(p, `document.querySelector('.http-resp [data-act="save-body"]')`)
	fill(p, ".http-resp .http-in", "out/users.json")
	click(p, `document.querySelector('.http-resp .http-form [data-act="ok"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPSaveBody, `{"run":7,"path":"out/users.json"}`), webproto.HTTPPath{Path: "out/users.json", Size: 40})
	p.WaitJS(`document.body.textContent.includes("out/users.json")`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

func TestHTTPQuickRequestAndLaptopCard(t *testing.T) {
	p, c := openHTTP(t)
	click(p, `document.querySelector('.pv[data-view="http"] [data-act="http-quick"]')`)
	fill(p, ".http-form .http-method", "POST")
	fill(p, ".http-form .http-url", "https://api.example.com/x")
	fill(p, ".http-form .http-hdrs", "X-A: 1\n")
	fill(p, ".http-form .http-body", "{}")
	click(p, `document.querySelector('.http-form [data-act="ok"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPSend, `{"method":"POST","url":"https://api.example.com/x","headers":[{"name":"X-A","value":"1"}],"body":"{}"}`),
		webproto.HTTPSent{Card: "card-1", Host: "api.example.com"})
	p.WaitJS(`document.body.textContent.includes("The laptop asks before the first request to api.example.com.")`)
	// Show the last response after the laptop's yes.
	click(p, `document.querySelector('.pv[data-view="http"] [data-act="http-last"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPResponse, `{}`), webproto.HTTPResponse{Run: 9, Method: "POST", URL: "https://api.example.com/x", Status: 201, StatusText: "Created", ReqHeaders: []webproto.HTTPHeader{}, Headers: []webproto.HTTPHeader{}})
	p.WaitJS(`document.querySelector(".http-resp .http-status")?.textContent.includes("201 Created")`)
	// A running request can be cancelled.
	click(p, `document.querySelector('.tr[data-saved="1"] [data-act="send"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPSend, `{"saved":1}`), webproto.HTTPSent{Run: 10})
	reply(t, c, expectOp(t, c, webproto.OpHTTPResponse, `{"run":10}`), webproto.HTTPResponse{Run: 10, Running: true, Method: "GET", URL: "http://localhost:8080/health", ReqHeaders: []webproto.HTTPHeader{}, Headers: []webproto.HTTPHeader{}})
	click(p, `document.querySelector('.http-resp [data-act="cancel"]')`)
	for {
		r := expectAnyOp(t, c, webproto.OpHTTPResponse, webproto.OpHTTPCancel)
		if r.Op == webproto.OpHTTPCancel {
			reply(t, c, r, nil)
			break
		}
		reply(t, c, r, webproto.HTTPResponse{Run: 10, Running: true, Method: "GET", URL: "http://localhost:8080/health", ReqHeaders: []webproto.HTTPHeader{}, Headers: []webproto.HTTPHeader{}})
	}
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

func TestHTTPEnvHistoryImportNewFile(t *testing.T) {
	p, c := openHTTP(t)
	fill(p, ".http-env", "prod")
	reply(t, c, expectOp(t, c, webproto.OpHTTPEnvSelect, `{"name":"prod"}`), nil)
	click(p, `document.querySelector('.tr[data-hist="h1"] [data-act="send"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPSend, `{"history":"h1"}`), webproto.HTTPSent{Run: 3})
	reply(t, c, expectOp(t, c, webproto.OpHTTPResponse, `{"run":3}`), webproto.HTTPResponse{Run: 3, Method: "GET", URL: "{{host}}/users", Status: 500, StatusText: "Internal Server Error", ReqHeaders: []webproto.HTTPHeader{}, Headers: []webproto.HTTPHeader{}})
	p.WaitJS(`document.querySelector(".http-resp .http-status")?.textContent.includes("500")`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPHistory, `{}`), webproto.HTTPHistory{Entries: []webproto.HTTPHistItem{}})
	click(p, `document.querySelector('.pv[data-view="http"] [data-act="http-import"]')`)
	fill(p, ".http-form .http-in", "curl https://api.example.com/b")
	click(p, `document.querySelector('.http-form [data-act="ok"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPImportCurl, `{"curl":"curl https://api.example.com/b"}`), webproto.HTTPPath{Path: "scratch.http", Line: 1})
	reply(t, c, expectOp(t, c, webproto.OpHTTPFiles, `{}`), httpFiles())
	click(p, `document.querySelector('.pv[data-view="http"] [data-act="http-new-file"]')`)
	fill(p, ".http-form .http-in", "api/orders")
	click(p, `document.querySelector('.http-form [data-act="ok"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPNewFile, `{"path":"api/orders.http"}`), webproto.HTTPPath{Path: "api/orders.http"})
	reply(t, c, expectOp(t, c, webproto.OpHTTPFiles, `{}`), httpFiles())
	click(p, `document.querySelector('.pv[data-view="http"] [data-act="http-env-file"]')`)
	reply(t, c, expectOp(t, c, webproto.OpHTTPEnvFile, `{}`), webproto.HTTPPath{Path: "http-client.env.json"})
	p.WaitJS(`location.hash === "#/files/http-client.env.json"`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

// expectAnyOp reads the page's next request, one of ops.
func expectAnyOp(t *testing.T, c *HubConn, ops ...string) *webproto.Req {
	t.Helper()
	for {
		switch v := c.Next(t).(type) {
		case *webproto.Sub, *webproto.Unsub:
			continue
		case *webproto.Req:
			for _, op := range ops {
				if v.Op == op {
					return v
				}
			}
			t.Fatalf("page asked %s %s; want one of %v", v.Op, v.Args, ops)
		default:
			t.Fatalf("page sent %s; want one of %v", show(v), ops)
		}
	}
}

// The HTTP client's side bar, a form and the HTTP RESPONSE panel pass
// the accessibility checker in both themes.
func TestAxeHTTPView(t *testing.T) {
	src := axeSource(t)
	for _, theme := range []string{"dark", "light"} {
		t.Run(theme, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, Desktop)
			p.Media(map[string]string{"prefers-color-scheme": theme, "prefers-reduced-motion": "reduce"})
			c := p.Login("c-http")
			p.TapAct("http")
			reply(t, c, expectOp(t, c, webproto.OpHTTPFiles, `{}`), httpFiles())
			reply(t, c, expectOp(t, c, webproto.OpHTTPSaved, `{}`), webproto.HTTPSaved{Requests: []webproto.HTTPReqItem{{Index: 1, Name: "health", Method: "GET", URL: "http://localhost:8080/health"}}})
			reply(t, c, expectOp(t, c, webproto.OpHTTPHistory, `{}`), webproto.HTTPHistory{Entries: []webproto.HTTPHistItem{{ID: "h1", At: 1, Method: "GET", URL: "{{host}}/users", Status: 404, ElapsedMS: 12}}})
			p.WaitJS(`document.querySelectorAll('.tr[data-req]').length === 2`)
			click(p, `document.querySelector('.pv[data-view="http"] [data-act="http-quick"]')`)
			p.WaitJS(`document.querySelector(".http-form .http-url")`)
			click(p, `document.querySelector('.pv[data-view="http"] [data-act="http-last"]')`)
			reply(t, c, expectOp(t, c, webproto.OpHTTPResponse, `{}`), webproto.HTTPResponse{Run: 2, Method: "GET", URL: "{{host}}/users", Status: 200, StatusText: "OK",
				ContentType: "application/json", Body: `{"a":1}`, ReqHeaders: []webproto.HTTPHeader{}, Headers: []webproto.HTTPHeader{{Name: "content-type", Value: "application/json"}}})
			p.WaitJS(`document.querySelector(".http-resp .http-status")?.textContent.includes("200 OK")`)
			p.axeDense(src, "http "+theme) // a dense tree, as the other side views
		})
	}
}
