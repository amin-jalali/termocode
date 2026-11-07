package dbclient

import (
	"bytes"
	"strings"
	"testing"
)

func TestExportJSONLinesWritesOneObjectPerLine(t *testing.T) {
	got := exportString(t, FormatJSONLines, ExportOptions{}, exportResult())
	want := `{"id":1,"name":"O'Hara","note":null}` + "\n" +
		`{"id":2,"name":"back\\slash","note":"line1\nline2"}` + "\n" +
		`{"id":-3,"name":"=1+1","note":"a,b \"q\""}` + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if empty := exportString(t, FormatJSONLines, ExportOptions{}, Result{Columns: []Column{{Name: "a"}}}); empty != "" {
		t.Fatalf("empty result wrote %q", empty)
	}
}

func TestExportYAMLQuotesEveryStringAndKeepsNull(t *testing.T) {
	r := Result{
		Columns: []Column{{Name: "id"}, {Name: "a: b"}},
		Rows: [][]Cell{
			{{Text: "1", Kind: "number"}, {Text: "yes"}},
			{{Text: "2", Kind: "number"}, {Null: true}},
		},
	}
	got := exportString(t, FormatYAML, ExportOptions{}, r)
	want := "- \"id\": 1\n  \"a: b\": \"yes\"\n- \"id\": 2\n  \"a: b\": null\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if empty := exportString(t, FormatYAML, ExportOptions{}, Result{Columns: []Column{{Name: "a"}}}); empty != "[]\n" {
		t.Fatalf("empty result wrote %q", empty)
	}
}

func TestExportHTMLEscapesCellsAndMarksNull(t *testing.T) {
	r := Result{
		Columns: []Column{{Name: "<b>"}, {Name: "n"}},
		Rows:    [][]Cell{{{Text: `<script>&"`}, {Null: true}}},
	}
	var b bytes.Buffer
	if err := WriteResult(&htmlWriter{w: &b}, r); err != nil {
		t.Fatal(err)
	}
	got := b.String()
	want := "<table>\n<thead><tr><th>&lt;b&gt;</th><th>n</th></tr></thead>\n<tbody>\n" +
		"<tr><td>&lt;script&gt;&amp;&#34;</td><td class=\"null\">NULL</td></tr>\n</tbody>\n</table>\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestExportXMLEscapesNamesAndText(t *testing.T) {
	r := Result{
		Columns: []Column{{Name: `a"<`}, {Name: "n"}},
		Rows:    [][]Cell{{{Text: "x & <y>"}, {Null: true}}},
	}
	got := exportString(t, FormatXML, ExportOptions{}, r)
	want := "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<rows>\n  <row>\n" +
		"    <column name=\"a&#34;&lt;\">x &amp; &lt;y&gt;</column>\n" +
		"    <column name=\"n\" null=\"true\"/>\n  </row>\n</rows>\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestExportSQLUpdateUsesTheKeyColumns(t *testing.T) {
	got := exportString(t, FormatSQLUpdate, ExportOptions{Driver: "postgres", Table: "t", KeyColumns: []string{"id"}}, exportResult())
	want := `UPDATE "t" SET "name" = 'O''Hara', "note" = NULL WHERE "id" = 1;` + "\n" +
		`UPDATE "t" SET "name" = 'back\slash', "note" = 'line1` + "\n" + `line2' WHERE "id" = 2;` + "\n" +
		`UPDATE "t" SET "name" = '=1+1', "note" = 'a,b "q"' WHERE "id" = -3;` + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	nullKey := exportString(t, FormatSQLUpdate, ExportOptions{Driver: "postgres", Table: "t", KeyColumns: []string{"note"}},
		Result{Columns: []Column{{Name: "id"}, {Name: "note"}}, Rows: [][]Cell{{{Text: "1", Kind: "number"}, {Null: true}}}})
	if nullKey != `UPDATE "t" SET "id" = 1 WHERE "note" IS NULL;`+"\n" {
		t.Fatalf("null key: %q", nullKey)
	}
}

func TestExportSQLUpdateRefusesMissingOrUnknownKeys(t *testing.T) {
	if _, err := NewRowWriter(FormatSQLUpdate, &bytes.Buffer{}, ExportOptions{Driver: "postgres", Table: "t"}); err == nil {
		t.Fatal("accepted UPDATE export without key columns")
	}
	w, err := NewRowWriter(FormatSQLUpdate, &bytes.Buffer{}, ExportOptions{Driver: "postgres", Table: "t", KeyColumns: []string{"nope"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteResult(w, exportResult()); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown key: %v", err)
	}
	w, _ = NewRowWriter(FormatSQLUpdate, &bytes.Buffer{}, ExportOptions{Driver: "postgres", Table: "t", KeyColumns: []string{"id", "name", "note"}})
	if err := WriteResult(w, exportResult()); err == nil {
		t.Fatal("accepted UPDATE with nothing left to SET")
	}
}

func TestExportCSVOptionsSeparatorHeaderAndNullText(t *testing.T) {
	o := ExportOptions{Separator: ';', NoHeader: true, NullText: `\N`}
	got := exportString(t, FormatCSV, o, exportResult())
	want := "1;O'Hara;\\N\r\n2;back\\slash;\"line1\r\nline2\"\r\n-3;'=1+1;\"a,b \"\"q\"\"\"\r\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if _, err := NewRowWriter(FormatCSV, &bytes.Buffer{}, ExportOptions{Separator: '"'}); err == nil {
		t.Fatal("accepted a quote as the separator")
	}
}

func TestExportSelectedColumnsOnly(t *testing.T) {
	got := exportString(t, FormatCSV, ExportOptions{Columns: []string{"note", "id"}}, exportResult())
	want := "note,id\r\n,1\r\n\"line1\r\nline2\",2\r\n\"a,b \"\"q\"\"\",-3\r\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	w, err := NewRowWriter(FormatCSV, &bytes.Buffer{}, ExportOptions{Columns: []string{"missing"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteResult(w, exportResult()); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("unknown column: %v", err)
	}
}
