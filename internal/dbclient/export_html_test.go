package dbclient

import (
	"strings"
	"testing"
)

func TestExportHTMLEscapesAndMarksNull(t *testing.T) {
	r := Result{Columns: []Column{{Name: "a<b"}, {Name: "c"}}, Rows: [][]Cell{{{Text: `<script>&"x"`}, {Text: "l1\nl2"}}, {{Null: true}}}}
	got := exportString(t, FormatHTML, ExportOptions{Table: "users"}, r)
	want := "<!DOCTYPE html>\n<html>\n<head>\n<meta charset=\"utf-8\">\n<title>users</title>\n" +
		"<style>table{border-collapse:collapse}th,td{border:1px solid #888;padding:2px 6px;white-space:pre-wrap;text-align:left}td.null{color:#888;font-style:italic}</style>\n" +
		"</head>\n<body>\n<table>\n<thead>\n<tr><th>a&lt;b</th><th>c</th></tr>\n</thead>\n<tbody>\n" +
		"<tr><td>&lt;script&gt;&amp;&#34;x&#34;</td><td>l1\nl2</td></tr>\n" +
		"<tr><td class=\"null\">NULL</td><td></td></tr>\n" +
		"</tbody>\n</table>\n</body>\n</html>\n"
	if got != want {
		t.Fatalf("html:\n%s\nwant:\n%s", got, want)
	}
}

func TestExportHTMLTitleFallsBack(t *testing.T) {
	got := exportString(t, FormatHTML, ExportOptions{}, Result{Columns: []Column{{Name: "a"}}})
	if !strings.Contains(got, "<title>Export</title>") || !strings.Contains(got, "<tbody>\n</tbody>") {
		t.Fatalf("html:\n%s", got)
	}
}

func TestExportHTMLWriteError(t *testing.T) {
	w, err := NewRowWriter(FormatHTML, failWriter{}, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Columns([]Column{{Name: "a"}}); err == nil {
		t.Fatal("write error lost")
	}
}
