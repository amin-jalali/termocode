package httpclient

import (
	"strings"
	"testing"
)

func TestAPIDocsMarkdown(t *testing.T) {
	src := "@host = https://api.example.com\n@token = sk-" + "live1\n\n" +
		"### List users\n# @name users\nGET {{host}}/users?limit=10\nAccept: application/json\nAuthorization: Bearer abc" + "123\n\n" +
		"###\nPOST {{host}}/users\nContent-Type: application/json\n\n{\"name\":\"Ada\"}\n"
	md := APIDocs(Parse(src), "api.http")
	for _, want := range []string{
		"# API docs: api.http",
		"2 requests",
		"| `GET` | [users](#users) | `{{host}}/users?limit=10` |",
		"## users",
		"```http\nGET {{host}}/users?limit=10\n```",
		"| Accept | application/json |",
		"| Authorization | Bearer [REDACTED] |",
		"## POST /users",
		"```json\n{\"name\":\"Ada\"}\n```",
		"| `host` | `https://api.example.com` |",
		"| `token` | `[REDACTED]` |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in\n%s", want, md)
		}
	}
	for _, leak := range []string{"abc123", "live1"} {
		if strings.Contains(md, leak) {
			t.Errorf("secret %q leaked", leak)
		}
	}
}

func TestAPIDocsEmptyAndPipes(t *testing.T) {
	if md := APIDocs(File{}, "x.http"); !strings.Contains(md, "No requests in this file.") {
		t.Fatalf("empty: %s", md)
	}
	md := APIDocs(Parse("GET https://h/a\nX-Note: a|b\n"), "p.http")
	if !strings.Contains(md, "| X-Note | a\\|b |") {
		t.Fatalf("pipe not escaped:\n%s", md)
	}
}
