package httpclient

import (
	"reflect"
	"strings"
	"testing"
)

const fieldsSrc = `@host = https://api.example.com

### list
GET {{host}}/users?page=2&q={{term}}&flag#top HTTP/1.1
Accept: application/json
# a comment inside the headers
X-Trace: on

{"a":1}

### other
POST {{host}}/login
`

func fieldsReq(t *testing.T, src string, line int) (*Request, []string) {
	t.Helper()
	f := Parse(src)
	r := f.AtLine(line)
	if r == nil {
		t.Fatalf("no request at line %d", line)
	}
	return r, strings.Split(src, "\n")
}

func TestRequestFieldsListsParamsThenHeaders(t *testing.T) {
	r, _ := fieldsReq(t, fieldsSrc, 4)
	want := []Field{
		{Kind: FieldParam, Name: "page", Value: "2", HasValue: true},
		{Kind: FieldParam, Name: "q", Value: "{{term}}", HasValue: true},
		{Kind: FieldParam, Name: "flag"},
		{Kind: FieldHeader, Name: "Accept", Value: "application/json", HasValue: true},
		{Kind: FieldHeader, Name: "X-Trace", Value: "on", HasValue: true},
	}
	if got := r.Fields(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Fields:\n got %+v\nwant %+v", got, want)
	}
	r2, _ := fieldsReq(t, fieldsSrc, 12)
	if got := r2.Fields(); len(got) != 0 {
		t.Fatalf("no params and no headers: got %+v", got)
	}
}

// apply runs EditField and returns the new file text.
func apply(t *testing.T, src string, line int, e FieldEdit) string {
	t.Helper()
	r, lines := fieldsReq(t, src, line)
	start, end, repl, err := EditField(lines, r, e)
	if err != nil {
		t.Fatalf("EditField(%+v): %v", e, err)
	}
	out := append(append(append([]string{}, lines[:start]...), repl...), lines[end:]...)
	return strings.Join(out, "\n")
}

func TestEditFieldParams(t *testing.T) {
	cases := []struct {
		name string
		e    FieldEdit
		want string
	}{
		{"change", FieldEdit{Kind: FieldParam, Index: 0, Name: "page", Value: "3"},
			"GET {{host}}/users?page=3&q={{term}}&flag#top HTTP/1.1"},
		{"delete", FieldEdit{Kind: FieldParam, Index: 1, Delete: true},
			"GET {{host}}/users?page=2&flag#top HTTP/1.1"},
		{"add", FieldEdit{Kind: FieldParam, Index: -1, Name: "size", Value: "10"},
			"GET {{host}}/users?page=2&q={{term}}&flag&size=10#top HTTP/1.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := apply(t, fieldsSrc, 4, c.e)
			if line := strings.Split(got, "\n")[3]; line != c.want {
				t.Fatalf("request line:\n got %q\nwant %q", line, c.want)
			}
			// The table read back from the new text shows the edit.
			f := Parse(got)
			if r := f.AtLine(4); r == nil || r.Line != 4 {
				t.Fatalf("request moved: %+v", r)
			}
		})
	}
	// Deleting every param drops the "?".
	src := "GET https://h/x?a=1\n"
	got := apply(t, src, 1, FieldEdit{Kind: FieldParam, Index: 0, Delete: true})
	if got != "GET https://h/x\n" {
		t.Fatalf("last param removed: %q", got)
	}
	// Adding to a URL without a query adds the "?".
	got = apply(t, "https://h/x\n", 1, FieldEdit{Kind: FieldParam, Index: -1, Name: "a", Value: "1"})
	if got != "https://h/x?a=1\n" {
		t.Fatalf("first param added: %q", got)
	}
}

func TestEditFieldHeaders(t *testing.T) {
	got := apply(t, fieldsSrc, 4, FieldEdit{Kind: FieldHeader, Index: 1, Name: "X-Trace", Value: "off"})
	if !strings.Contains(got, "\nX-Trace: off\n\n{\"a\":1}") || !strings.Contains(got, "# a comment inside the headers") {
		t.Fatalf("header changed, comment kept:\n%s", got)
	}
	got = apply(t, fieldsSrc, 4, FieldEdit{Kind: FieldHeader, Index: 0, Delete: true})
	if strings.Contains(got, "Accept:") || !strings.Contains(got, "X-Trace: on") {
		t.Fatalf("header deleted:\n%s", got)
	}
	got = apply(t, fieldsSrc, 4, FieldEdit{Kind: FieldHeader, Index: -1, Name: "Authorization", Value: "Bearer {{token}}"})
	if !strings.Contains(got, "X-Trace: on\nAuthorization: Bearer {{token}}\n\n{\"a\":1}") {
		t.Fatalf("header added after the last one:\n%s", got)
	}
	// A request without headers gets its first one right under the request line.
	got = apply(t, fieldsSrc, 12, FieldEdit{Kind: FieldHeader, Index: -1, Name: "Accept", Value: "*/*"})
	if !strings.HasSuffix(got, "POST {{host}}/login\nAccept: */*\n") {
		t.Fatalf("first header:\n%s", got)
	}
	// The body is never touched.
	if r := Parse(apply(t, fieldsSrc, 4, FieldEdit{Kind: FieldHeader, Index: 0, Name: "Accept", Value: "text/plain"})).AtLine(4); r.BodyText() != `{"a":1}` {
		t.Fatalf("body changed: %q", r.BodyText())
	}
}

func TestEditFieldRejectsBadInput(t *testing.T) {
	r, lines := fieldsReq(t, fieldsSrc, 4)
	bad := []FieldEdit{
		{Kind: FieldParam, Index: 9, Name: "a", Value: "1"},
		{Kind: FieldHeader, Index: 9, Name: "a", Value: "1"},
		{Kind: FieldParam, Index: -1, Name: "", Value: "1"},
		{Kind: FieldParam, Index: -1, Name: "a b", Value: "1"},
		{Kind: FieldParam, Index: -1, Name: "a", Value: "1&b=2"},
		{Kind: FieldParam, Index: -1, Name: "a", Value: "x#y"},
		{Kind: FieldHeader, Index: -1, Name: "", Value: "1"},
		{Kind: FieldHeader, Index: -1, Name: "A:B", Value: "1"},
		{Kind: FieldHeader, Index: -1, Name: "# x", Value: "1"},
		{Kind: FieldHeader, Index: -1, Name: "A", Value: "1\nB: 2"},
	}
	for _, e := range bad {
		if _, _, _, err := EditField(lines, r, e); err == nil {
			t.Errorf("EditField(%+v) = nil error, want one", e)
		}
	}
}
