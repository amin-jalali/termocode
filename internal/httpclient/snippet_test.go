package httpclient

import (
	"strings"
	"testing"
)

func snippetFixture() Resolved {
	body := `{"name":"it's \"Ada\""}`
	tmplBody := body
	return Resolved{
		Request: Request{Method: "POST", URL: "https://api.example.com/users", Headers: []Header{
			{"Content-Type", "application/json"}, {"Authorization", "Bearer sekrit-token-123"}}, Body: &body},
		Template: Request{Method: "POST", URL: "{{host}}/users", Headers: []Header{
			{"Content-Type", "application/json"}, {"Authorization", "Bearer {{token}}"}}, Body: &tmplBody},
		Secrets: []string{"sekrit-token-123"},
	}
}

func TestSnippetLangsList(t *testing.T) {
	want := []string{"curl", "go", "python", "fetch", "axios", "httpie", "powershell"}
	if len(SnippetLangs) != len(want) {
		t.Fatalf("langs = %+v", SnippetLangs)
	}
	for i, l := range SnippetLangs {
		if l.ID != want[i] || l.Title == "" {
			t.Errorf("lang %d = %+v, want id %q", i, l, want[i])
		}
	}
}

func TestToSnippetCurlIsToCurl(t *testing.T) {
	rr := snippetFixture()
	for _, sec := range []bool{false, true} {
		got, err := ToSnippet("curl", rr, sec)
		if err != nil || got != ToCurl(rr, sec) {
			t.Errorf("secrets=%v: %q %v", sec, got, err)
		}
	}
}

func TestToSnippetUnknownLang(t *testing.T) {
	if _, err := ToSnippet("cobol", snippetFixture(), false); err == nil {
		t.Fatal("no error for an unknown language")
	}
}

func TestToSnippetLanguages(t *testing.T) {
	cases := map[string][]string{
		"go": {
			"package main",
			`"net/http"`,
			`"strings"`,
			"body := strings.NewReader(\"{\\\"name\\\":\\\"it's \\\\\\\"Ada\\\\\\\"\\\"}\")",
			`req, err := http.NewRequest("POST", "https://api.example.com/users", body)`,
			`req.Header.Add("Content-Type", "application/json")`,
			`req.Header.Add("Authorization", "Bearer {{token}}")`,
			"resp, err := http.DefaultClient.Do(req)",
			"defer resp.Body.Close()",
		},
		"python": {
			"import requests",
			`url = "https://api.example.com/users"`,
			`    "Content-Type": "application/json",`,
			`    "Authorization": "Bearer {{token}}",`,
			`response = requests.request("POST", url, headers=headers, data=data)`,
			"print(response.status_code)",
		},
		"fetch": {
			`const response = await fetch("https://api.example.com/users", {`,
			`  method: "POST",`,
			`    "Authorization": "Bearer {{token}}",`,
			"  body: ",
			"console.log(response.status);",
		},
		"axios": {
			`const axios = require("axios");`,
			`  method: "POST",`,
			`  url: "https://api.example.com/users",`,
			`    "Authorization": "Bearer {{token}}",`,
			"  data: ",
			"console.log(response.data);",
		},
		"httpie": {
			`http POST 'https://api.example.com/users' 'Content-Type:application/json' 'Authorization:Bearer {{token}}' --raw '{"name":"it'\''s \"Ada\""}'`,
		},
		"powershell": {
			`$headers = @{`,
			`    'Authorization' = 'Bearer {{token}}'`,
			`$body = '{"name":"it''s \"Ada\""}'`,
			`-Uri 'https://api.example.com/users' -Method 'POST'`,
			`-ContentType 'application/json'`,
			"-Body $body",
		},
	}
	rr := snippetFixture()
	for lang, wants := range cases {
		got, err := ToSnippet(lang, rr, false)
		if err != nil {
			t.Errorf("%s: %v", lang, err)
			continue
		}
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in\n%s", lang, w, got)
			}
		}
		if strings.Contains(got, "sekrit") {
			t.Errorf("%s: secret leaked without secrets:\n%s", lang, got)
		}
		full, _ := ToSnippet(lang, rr, true)
		if !strings.Contains(full, "Bearer sekrit-token-123") {
			t.Errorf("%s: with secrets has no secret:\n%s", lang, full)
		}
	}
}

func TestToSnippetNoBody(t *testing.T) {
	rr := Resolved{Request: Request{Method: "GET", URL: "https://a.com/x"}, Template: Request{Method: "GET", URL: "https://a.com/x"}}
	for _, l := range SnippetLangs {
		got, err := ToSnippet(l.ID, rr, false)
		if err != nil {
			t.Fatalf("%s: %v", l.ID, err)
		}
		if !strings.Contains(got, "https://a.com/x") {
			t.Errorf("%s: no url:\n%s", l.ID, got)
		}
		for _, bad := range []string{"strings.NewReader", "data=data", "body:", "data:", "--raw", "-Body"} {
			if strings.Contains(got, bad) {
				t.Errorf("%s: body part %q without a body:\n%s", l.ID, bad, got)
			}
		}
	}
	if got, _ := ToSnippet("go", rr, false); !strings.Contains(got, `http.NewRequest("GET", "https://a.com/x", nil)`) || strings.Contains(got, `"strings"`) {
		t.Errorf("go without body:\n%s", got)
	}
}

// A method from a shared .http file must not run shell code when an
// HTTPie or PowerShell snippet is pasted.
func TestToSnippetQuotesOddMethod(t *testing.T) {
	rr := Resolved{Request: Request{Method: "GET;rm", URL: "https://a.com"}, Template: Request{Method: "GET;rm", URL: "https://a.com"}}
	if got, _ := ToSnippet("httpie", rr, false); !strings.HasPrefix(got, "http 'GET;rm' ") {
		t.Errorf("httpie: %s", got)
	}
	if got, _ := ToSnippet("powershell", rr, false); !strings.Contains(got, "-Method 'GET;rm'") {
		t.Errorf("powershell: %s", got)
	}
}
