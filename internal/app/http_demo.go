package app

import (
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

// httpDemoFile is the .http sample of the screenshot scenes (dev action
// "httpdemo:file|response|tls|headers"): no network, fixed numbers.
const httpDemoFile = `@host = {{baseUrl}}

### Get one user
# @name getUser
GET {{host}}/users/42
Accept: application/json
Authorization: Bearer {{token}}

###
# @name createUser
POST {{host}}/users
Content-Type: application/json

{
  "name": "Ada",
  "email": "ada@example.com"
}

###
DELETE {{host}}/users/{{$guid}}
`

const httpDemoBody = `{"id":42,"name":"Ada Lovelace","email":"ada@example.com","roles":["admin","dev"],"address":{"street":"12 St James's Square","city":"London","zip":"SW1Y 4JH","country":"UK"},"createdAt":"2026-10-16T09:12:33Z"}`

// httpDemo writes docs/users.http (git-excluded, so Source Control stays
// the same), opens it and, for the response variants, shows a canned
// answer on the page.
func (m *Model) httpDemo(variant string) tea.Cmd {
	root := aiWorkspaceRoot()
	file := filepath.Join(root, "docs", "users.http")
	if _, err := os.Stat(file); err != nil {
		_ = os.MkdirAll(filepath.Dir(file), 0o755)
		_ = os.WriteFile(file, []byte(httpDemoFile), 0o644)
		if ex := filepath.Join(root, ".git", "info", "exclude"); dirExists(filepath.Dir(ex)) {
			if f, err := os.OpenFile(ex, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				_, _ = f.WriteString("/docs/users.http\n")
				_ = f.Close()
			}
		}
	}
	m.openFileAtLine(file, 5)
	tmpl := httpclient.Parse(httpDemoFile).Requests[0]
	res := tmpl
	res.URL = "https://api.example.com/users/42"
	res.Headers = []httpclient.Header{{Name: "Accept", Value: "application/json"}, {Name: "Authorization", Value: "Bearer demo-token-1234"}}
	resp := &httpclient.Response{
		Method: "GET", URL: res.URL, Status: 200, StatusText: "OK", Proto: "HTTP/2.0",
		Body: []byte(httpDemoBody), Size: int64(len(httpDemoBody)), Elapsed: 118 * time.Millisecond,
		ContentType: "application/json", Charset: "utf-8", IsJSON: true,
		ReqHeaders: []httpclient.Header{{Name: "accept", Value: "application/json"}, {Name: "authorization", Value: "Bearer demo-token-1234"}, {Name: "user-agent", Value: "termocode/0.9"}},
		Headers: []httpclient.Header{{Name: "content-length", Value: "211"}, {Name: "content-type", Value: "application/json; charset=utf-8"},
			{Name: "set-cookie", Value: "session=abc123; Path=/; HttpOnly"}, {Name: "x-request-id", Value: "8f1c2d"}},
		Timing: httpclient.Timing{DNS: 12 * time.Millisecond, Connect: 31 * time.Millisecond, TLS: 48 * time.Millisecond, FirstByte: 102 * time.Millisecond, Total: 118 * time.Millisecond},
	}
	switch variant {
	case "tls":
		resp = &httpclient.Response{Method: "GET", URL: res.URL, Err: "TLS: x509: certificate signed by unknown authority",
			ErrKind: httpclient.ErrKindTLS, Elapsed: 52 * time.Millisecond}
	}
	run := &httpRun{rr: httpclient.Resolved{Request: res, Template: tmpl, Env: "dev"}, resp: resp, file: file,
		line: tmpl.Line, label: tmpl.Label(), env: "dev", by: "user", at: time.Now()}
	s := m.httpS()
	s.nextID++
	run.id = s.nextID
	if variant != "tls" {
		m.httpRecordLens(run)
	}
	m.httpRefreshLens(file)
	if variant == "file" {
		return m.fetchStateCmd()
	}
	s.remember(run)
	s.view = httpViewBody
	if variant == "headers" {
		s.view = httpViewHeaders
	}
	return m.httpShowRun(run, false)
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}
