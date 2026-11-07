package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/httpclient"
)

func TestHTTPFieldsPaletteItem(t *testing.T) {
	for _, it := range httpPaletteItems() {
		if it.ID == "http-fields" {
			if it.Title != "HTTP: Edit Params and Headers…" {
				t.Fatalf("title %q", it.Title)
			}
			return
		}
	}
	t.Fatal("no http-fields palette item")
}

// The table shows one row per param and header (in that order), then
// the two add rows; the row ids point back at the field.
func TestHTTPFieldItemsTable(t *testing.T) {
	fields := []httpclient.Field{
		{Kind: httpclient.FieldParam, Name: "page", Value: "2", HasValue: true},
		{Kind: httpclient.FieldParam, Name: "flag"},
		{Kind: httpclient.FieldHeader, Name: "Accept", Value: "application/json", HasValue: true},
	}
	items := httpFieldItems(fields)
	want := []struct{ id, title, hint string }{
		{"p0", "page = 2", "query param"},
		{"p1", "flag", "query param"},
		{"h0", "Accept: application/json", "header"},
		{"p-1", "+ Add Query Param…", ""},
		{"h-1", "+ Add Header…", ""},
	}
	if len(items) != len(want) {
		t.Fatalf("items %+v", items)
	}
	for i, w := range want {
		if items[i].ID != w.id || items[i].Title != w.title || items[i].Hint != w.hint {
			t.Errorf("row %d = %q %q %q, want %q %q %q", i, items[i].ID, items[i].Title, items[i].Hint, w.id, w.title, w.hint)
		}
	}
	kind, idx, ok := httpFieldRowID("h0")
	if !ok || kind != httpclient.FieldHeader || idx != 0 {
		t.Fatalf("h0 = %v %d %v", kind, idx, ok)
	}
	kind, idx, ok = httpFieldRowID("p-1")
	if !ok || kind != httpclient.FieldParam || idx != -1 {
		t.Fatalf("p-1 = %v %d %v", kind, idx, ok)
	}
	if _, _, ok := httpFieldRowID("x"); ok {
		t.Fatal("bad id accepted")
	}
}

// The prompt text is "name=value" for a param and "name: value" for a
// header; empty text removes the row.
func TestHTTPFieldInput(t *testing.T) {
	cases := []struct {
		kind        httpclient.FieldKind
		in          string
		name, value string
		has, del    bool
	}{
		{httpclient.FieldParam, " page = 3 ", "page", "3", true, false},
		{httpclient.FieldParam, "flag", "flag", "", false, false},
		{httpclient.FieldParam, "  ", "", "", false, true},
		{httpclient.FieldHeader, "Accept: text/plain", "Accept", "text/plain", true, false},
		{httpclient.FieldHeader, "X-Url: https://a", "X-Url", "https://a", true, false},
		{httpclient.FieldHeader, "", "", "", false, true},
	}
	for _, c := range cases {
		e := httpFieldInput(c.kind, 1, c.in)
		if e.Name != c.name || e.Value != c.value || e.HasValue != c.has || e.Delete != c.del || e.Index != 1 || e.Kind != c.kind {
			t.Errorf("%q -> %+v", c.in, e)
		}
	}
	if got := httpFieldText(httpclient.Field{Kind: httpclient.FieldHeader, Name: "A", Value: "b", HasValue: true}); got != "A: b" {
		t.Errorf("header text %q", got)
	}
	if got := httpFieldText(httpclient.Field{Kind: httpclient.FieldParam, Name: "a", Value: "b", HasValue: true}); got != "a=b" {
		t.Errorf("param text %q", got)
	}
}
