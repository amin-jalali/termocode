package httpclient

import (
	"strings"
	"testing"
)

func TestImportedHTTPRoundTrips(t *testing.T) {
	im := Imported{
		Format:       FormatHAR,
		Requests:     []Request{{Method: "GET", URL: "{{host}}/a", Name: "one"}, {Method: "POST", URL: "{{host}}/b", Name: "two"}},
		Vars:         []Var{{Name: "host", Value: "https://x.io"}},
		Placeholders: []string{"authorization"},
	}
	text := im.HTTP()
	if !strings.HasPrefix(text, "# Imported from a HAR file") || !strings.Contains(text, "{{authorization}}") {
		t.Fatalf("text:\n%s", text)
	}
	f := Parse(text)
	if len(f.Requests) != 2 || f.Requests[1].URL != "{{host}}/b" || len(f.Vars) != 1 || f.Vars[0].Value != "https://x.io" {
		t.Fatalf("parsed = %+v", f)
	}
}

func TestCollectionFileNames(t *testing.T) {
	if got := ImportedFileName("/w/My Shop.postman_collection.json"); got != "My-Shop.http" {
		t.Fatalf("import name = %q", got)
	}
	if got := ImportedFileName("/w/net.har"); got != "net.http" {
		t.Fatalf("import name = %q", got)
	}
	if got := HARFileName("/w/api/users.http"); got != "/w/api/users.har" {
		t.Fatalf("har name = %q", got)
	}
}
