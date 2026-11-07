package httpclient

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// A fake secret, split so scanners do not flag the source.
var fakeToken = "sk-" + "test-collection-123"

func TestImportCollectionV21(t *testing.T) {
	src := `{
	  "info": {"name": "Shop", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
	  "auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{token}}", "type": "string"}]},
	  "variable": [{"key": "host", "value": "https://api.example.com"}, {"key": "token", "value": ""}],
	  "item": [
	    {"name": "Users", "item": [
	      {"name": "List users", "request": {"method": "get", "url": {"raw": "{{host}}/users?page=1"},
	        "header": [{"key": "Accept", "value": "application/json"}, {"key": "X-Off", "value": "1", "disabled": true}]}},
	      {"name": "Create user", "request": {"method": "POST", "url": "{{host}}/users",
	        "header": [{"key": "Content-Type", "value": "application/json"}],
	        "body": {"mode": "raw", "raw": "{\"name\":\"a\"}"}}}
	    ]},
	    {"name": "Login", "request": {"method": "POST", "url": "{{host}}/login", "auth": {"type": "noauth"},
	      "body": {"mode": "urlencoded", "urlencoded": [{"key": "user", "value": "a b"}, {"key": "skip", "value": "x", "disabled": true}]}}}
	  ]
	}`
	im, err := ImportCollection([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if im.Format != FormatCollection {
		t.Fatalf("format %q", im.Format)
	}
	want := "### Users / List users\n" +
		"GET {{host}}/users?page=1\n" +
		"Accept: application/json\n" +
		"Authorization: Bearer {{token}}\n" +
		"###\n### Users / Create user\n" +
		"POST {{host}}/users\n" +
		"Content-Type: application/json\n" +
		"Authorization: Bearer {{token}}\n" +
		"\n{\"name\":\"a\"}\n" +
		"###\n### Login\n" +
		"POST {{host}}/login\n" +
		"Content-Type: application/x-www-form-urlencoded\n" +
		"\nuser=a+b\n"
	if got := Serialize(im.Requests); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if len(im.Vars) != 2 || im.Vars[0] != (Var{Name: "host", Value: "https://api.example.com"}) || im.Vars[1].Name != "token" {
		t.Fatalf("vars %+v", im.Vars)
	}
}

func TestImportHAR(t *testing.T) {
	src := `{"log": {"version": "1.2", "entries": [
	  {"request": {"method": "POST", "url": "https://api.example.com/items?x=1", "httpVersion": "HTTP/2",
	    "headers": [{"name": ":authority", "value": "api.example.com"}, {"name": "content-type", "value": "application/json"},
	      {"name": "content-length", "value": "7"}, {"name": "cookie", "value": "sid=` + fakeToken + `"}],
	    "postData": {"mimeType": "application/json", "text": "{\"a\":1}"}},
	   "response": {"status": 201}},
	  {"request": {"method": "GET", "url": "https://api.example.com/items", "headers": []}, "comment": "list"}
	]}}`
	im, err := ImportCollection([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if im.Format != FormatHAR {
		t.Fatalf("format %q", im.Format)
	}
	got := Serialize(im.Requests)
	want := "POST https://api.example.com/items?x=1\n" +
		"content-type: application/json\n" +
		"cookie: {{cookie}}\n" +
		"\n{\"a\":1}\n" +
		"###\n### list\n" +
		"GET https://api.example.com/items\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(got, fakeToken) {
		t.Fatal("secret cookie copied into the .http text")
	}
	if strings.Join(im.Placeholders, ",") != "cookie" {
		t.Fatalf("placeholders %v", im.Placeholders)
	}
}

func TestImportInsomnia(t *testing.T) {
	src := `{"_type": "export", "__export_format": 4, "resources": [
	  {"_id": "wrk_1", "_type": "workspace", "name": "Shop"},
	  {"_id": "env_1", "_type": "environment", "parentId": "wrk_1", "data": {"host": "https://api.example.com", "n": 3, "obj": {"a": 1}}},
	  {"_id": "fld_1", "_type": "request_group", "parentId": "wrk_1", "name": "Users"},
	  {"_id": "req_1", "_type": "request", "parentId": "fld_1", "name": "Get user", "method": "GET",
	    "url": "{{ _.host }}/users/{{_.id}}", "headers": [{"name": "Accept", "value": "*/*"}, {"name": "X-Off", "value": "1", "disabled": true}],
	    "parameters": [{"name": "full", "value": "1"}],
	    "authentication": {"type": "bearer", "token": "` + fakeToken + `"}},
	  {"_id": "req_2", "_type": "request", "parentId": "wrk_1", "name": "Login", "method": "POST", "url": "{{ _.host }}/login",
	    "body": {"mimeType": "application/json", "text": "{\"u\":\"{{ _.user }}\"}"},
	    "authentication": {"type": "basic", "username": "{{ _.user }}", "password": "{{ _.pass }}"}}
	]}`
	im, err := ImportCollection([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if im.Format != FormatInsomnia {
		t.Fatalf("format %q", im.Format)
	}
	got := Serialize(im.Requests)
	want := "### Users / Get user\n" +
		"GET {{host}}/users/{{id}}?full=1\n" +
		"Accept: */*\n" +
		"Authorization: Bearer {{authorization}}\n" +
		"###\n### Login\n" +
		"POST {{host}}/login\n" +
		"Content-Type: application/json\n" +
		"Authorization: Basic {{user}} {{pass}}\n" +
		"\n{\"u\":\"{{user}}\"}\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if strings.Contains(got, fakeToken) {
		t.Fatal("secret token copied into the .http text")
	}
	if len(im.Vars) != 2 || im.Vars[0] != (Var{Name: "host", Value: "https://api.example.com"}) || im.Vars[1] != (Var{Name: "n", Value: "3"}) {
		t.Fatalf("vars %+v", im.Vars)
	}
}

func TestImportCollectionErrors(t *testing.T) {
	for _, src := range []string{``, `not json`, `[]`, `{"a":1}`, `{"log":{}}`} {
		if _, err := ImportCollection([]byte(src)); err == nil {
			t.Errorf("%q: no error", src)
		}
	}
	big := make([]byte, maxImportBytes+1)
	if _, err := ImportCollection(big); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("big: %v", err)
	}
	// A request line that would break the .http text is cleaned.
	im, err := ImportCollection([]byte(`{"info":{},"item":[{"name":"a\nGET x","request":{"method":"rm -rf","url":"http://a/\nPOST b"}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := Serialize(im.Requests); got != "### a GET x\nGET http://a/ POST b\n" {
		t.Fatalf("cleaned: %q", got)
	}
}

func TestExportHAR(t *testing.T) {
	body := `{"a":1}`
	reqs := []Request{
		{Name: "create", Method: "POST", URL: "https://api.example.com/items?x=1&y=two", Body: &body,
			Headers: []Header{{"Content-Type", "application/json"}, {"Authorization", "Bearer " + fakeToken}}},
		{Method: "GET", URL: "{{host}}/items", Headers: []Header{{"Authorization", "Bearer {{token}}"}}},
	}
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	out, err := ExportHAR(reqs, now)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), fakeToken) {
		t.Fatal("secret in HAR")
	}
	var har struct {
		Log struct {
			Version string `json:"version"`
			Creator struct {
				Name string `json:"name"`
			} `json:"creator"`
			Entries []struct {
				Started string `json:"startedDateTime"`
				Comment string `json:"comment"`
				Request struct {
					QueryString []struct{ Name, Value string } `json:"queryString"`
				} `json:"request"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(out, &har); err != nil {
		t.Fatal(err)
	}
	if har.Log.Version != "1.2" || har.Log.Creator.Name != "termocode" || len(har.Log.Entries) != 2 {
		t.Fatalf("har %+v", har.Log)
	}
	e := har.Log.Entries[0]
	if e.Started != "2026-11-07T10:00:00Z" || e.Comment != "create" || len(e.Request.QueryString) != 2 || e.Request.QueryString[1].Value != "two" {
		t.Fatalf("entry %+v", e)
	}
	// Round trip: the HAR reads back to the same requests (the hidden
	// secret becomes a placeholder, the placeholder one is kept).
	im, err := ImportCollection(out)
	if err != nil {
		t.Fatal(err)
	}
	want := "### create\n" +
		"POST https://api.example.com/items?x=1&y=two\n" +
		"Content-Type: application/json\n" +
		"Authorization: Bearer {{authorization}}\n" +
		"\n{\"a\":1}\n" +
		"###\n" +
		"GET {{host}}/items\n" +
		"Authorization: Bearer {{token}}\n"
	if got := Serialize(im.Requests); got != want {
		t.Fatalf("round trip\n%s\nwant\n%s", got, want)
	}
}

// FuzzImportCollection reads any bytes as a collection: it never
// panics, and every imported request has a method and a URL on one
// line, so the .http text it writes stays well formed.
func FuzzImportCollection(f *testing.F) {
	f.Add([]byte(`{"info":{},"item":[{"name":"a","request":{"method":"GET","url":"http://a"}}]}`))
	f.Add([]byte(`{"log":{"entries":[{"request":{"method":"GET","url":"http://a","headers":[]}}]}}`))
	f.Add([]byte(`{"_type":"export","resources":[{"_id":"r","_type":"request","parentId":"r","method":"GET","url":"http://a"}]}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		im, err := ImportCollection(data)
		if err != nil {
			return
		}
		for _, r := range im.Requests {
			if r.Method == "" || r.URL == "" || strings.ContainsAny(r.Method+r.URL+r.Name, "\r\n") {
				t.Fatalf("bad request %+v", r)
			}
		}
	})
}
