package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// The export picker offers the extra formats too.
func TestDBExportFormatListHasTheExtraFormats(t *testing.T) {
	have := map[dbclient.ExportFormat]bool{}
	for _, f := range dbExportFormatList() {
		have[f.Format] = true
	}
	for _, f := range []dbclient.ExportFormat{dbclient.FormatCSV, dbclient.FormatJSONLines, dbclient.FormatYAML, dbclient.FormatXML, dbclient.FormatSQLUpdate, dbclient.FormatHTML} {
		if !have[f] {
			t.Errorf("no %s in the picker", f)
		}
	}
}

// The CSV settings shape the file: separator, header and NULL text.
func TestDBExportCSVSettings(t *testing.T) {
	sep, null, header := ";", "NULL", false
	s := dbConsoleSettings{CSVSeparator: &sep, CSVNull: &null, CSVHeader: &header}
	o := s.exportOptions(dbclient.ExportOptions{})
	if o.Separator != ';' || o.NullText != "NULL" || !o.NoHeader {
		t.Fatalf("options: %+v", o)
	}
	if o := (dbConsoleSettings{}).exportOptions(dbclient.ExportOptions{}); o.Separator != 0 || o.NoHeader || o.NullText != "" {
		t.Fatalf("defaults: %+v", o)
	}
}

// db_export_mask hides the values of the named columns in exports and
// copies; NULL stays NULL.
func TestDBExportMaskColumns(t *testing.T) {
	r := dbclient.Result{
		Columns: []dbclient.Column{{Name: "id"}, {Name: "email"}, {Name: "user_password"}},
		Rows:    [][]dbclient.Cell{{{Text: "1"}, {Text: "a@x.io"}, {Text: "hunter2"}}, {{Text: "2"}, {Null: true}, {Text: "pw"}}},
	}
	got := dbMaskResult(r, []string{"EMAIL", "*password*"})
	if got.Rows[0][0].Text != "1" || got.Rows[0][1].Text != dbMaskText || got.Rows[0][2].Text != dbMaskText {
		t.Fatalf("masked: %+v", got.Rows[0])
	}
	if !got.Rows[1][1].Null {
		t.Fatal("NULL must stay NULL")
	}
	if r.Rows[0][1].Text != "a@x.io" {
		t.Fatal("the loaded result changed")
	}
}

// Copy as writes the result in the picked format.
func TestDBCopyAsFormats(t *testing.T) {
	r := dbclient.Result{Columns: []dbclient.Column{{Name: "id"}, {Name: "name"}}, Rows: [][]dbclient.Cell{{{Text: "1", Kind: "number"}, {Text: "Ann"}}}}
	for f, want := range map[dbclient.ExportFormat]string{
		dbclient.FormatYAML:      "name: Ann",
		dbclient.FormatJSONLines: `"name":"Ann"`,
		dbclient.FormatXML:       "<name>Ann</name>",
	} {
		text, err := dbCopyAsText(r, f, dbclient.ExportOptions{Driver: "postgres", Table: "users"})
		if err != nil || !strings.Contains(text, want) {
			t.Errorf("%s: %v %q", f, err, text)
		}
	}
	if _, err := dbCopyAsText(r, dbclient.FormatSQLUpdate, dbclient.ExportOptions{Driver: "postgres", Table: "users"}); err == nil {
		t.Error("SQL UPDATEs without a key must refuse")
	}
}

// Export tables to a folder writes one CSV per table and schema.sql, and
// never replaces a file that is there.
func TestDBExportTablesToFolder(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "orders.csv"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := &searchSession{}
	tgt := dbSearchTarget{conn: "pg", driver: "postgres", schema: "public", path: "pg"}
	rep, err := dbExportTables(context.Background(), sess, tgt, dir, dbclient.ExportOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.written != 1 || len(rep.skipped) != 1 || rep.skipped[0] != "orders" {
		t.Fatalf("report: %+v", rep)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "orders.csv")); string(b) != "keep" {
		t.Fatal("an existing file was replaced")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "users.csv")); err != nil || !strings.HasPrefix(string(b), "id") {
		t.Fatalf("users.csv: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(dir, "schema.sql")); err != nil {
		t.Fatalf("schema.sql: %v", err)
	}
}
