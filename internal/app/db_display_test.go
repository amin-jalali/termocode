package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func writeDBDisplayConfig(t *testing.T, json string) {
	t.Helper()
	dir := withConfigDir(t)
	if err := os.MkdirAll(filepath.Join(dir, "termocode"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "termocode", "config.json"), []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
}

// DBX-27: the db_* display keys become one dbclient.Display; bad values fall
// back to the default instead of breaking the grid.
func TestDBDisplaySettings(t *testing.T) {
	writeDBDisplayConfig(t, `{"db_number_format":"#,##0.00","db_date_format":"dd/MM/yyyy","db_time_format":"yyyy-MM-dd HH:mm","db_time_zone":"UTC","db_bool_style":"yes/no","db_lob_bytes":16,"db_geometry_format":"wkt","db_alternate_rows":true}`)
	s := loadDBConsoleSettings()
	d := s.display()
	if d.Number != "#,##0.00" || d.Date != "dd/MM/yyyy" || d.Time != "yyyy-MM-dd HH:mm" || d.Zone == nil || d.Bool != "yes/no" || d.LOBBytes != 16 || d.Geometry != "wkt" {
		t.Fatalf("display = %+v", d)
	}
	if !s.alternateRows() {
		t.Fatal("db_alternate_rows lost")
	}

	writeDBDisplayConfig(t, `{"db_time_zone":"Mars/Olympus","db_bool_style":"on/off","db_lob_bytes":-5,"db_geometry_format":"svg"}`)
	d = loadDBConsoleSettings().display()
	if d.Zone != nil || d.Bool != "" || d.LOBBytes != dbDefaultLOBBytes || d.Geometry != "" {
		t.Fatalf("bad values not dropped: %+v", d)
	}

	withConfigDir(t)
	s = loadDBConsoleSettings()
	if d := s.display(); d.LOBBytes != dbDefaultLOBBytes || d.Number != "" || d.Zone != nil {
		t.Fatalf("defaults = %+v", d)
	}
	if s.alternateRows() {
		t.Fatal("alternate rows on by default")
	}
}

func dbDisplayResult() dbclient.Result {
	return dbclient.Result{
		Columns: []dbclient.Column{{Name: "n", Type: "NUMERIC"}, {Name: "ok", Type: "BOOLEAN"}, {Name: "d", Type: "DATE"}},
		Rows: [][]dbclient.Cell{
			{{Text: "1234.5", Kind: "number"}, {Text: "true", Kind: "bool"}, {Text: "2026-11-07"}},
			{{Text: "7", Kind: "number"}, {Text: "false", Kind: "bool"}, {Text: "2026-01-02"}},
			{{Text: "8", Kind: "number"}, {Null: true}, {Text: "2026-01-03"}},
		},
	}
}

// The grid and the one-row view show the formatted values.
func TestDBDisplayGridShowsFormats(t *testing.T) {
	writeDBDisplayConfig(t, `{"db_number_format":"#,##0.00","db_bool_style":"yes/no","db_date_format":"dd/MM/yyyy"}`)
	m := dbGridModel(dbDisplayResult())
	p := m.databasePage()
	all := strings.Join(p.lines, "\n")
	for _, want := range []string{"1,234.50", "yes", "no", "07/11/2026", "<null>"} {
		if !strings.Contains(all, want) {
			t.Fatalf("grid lost %q:\n%s", want, all)
		}
	}
	if strings.Contains(all, "1234.5 ") || strings.Contains(all, "true") {
		t.Fatalf("grid shows raw values:\n%s", all)
	}
	// the stored result keeps the raw text (copy, export and edits use it)
	if got := m.dbConsole().shown.result.Rows[0][0].Text; got != "1234.5" {
		t.Fatalf("result changed to %q", got)
	}
	s := m.dbConsole()
	s.trans, s.trow = true, 0
	p = m.databasePage()
	all = strings.Join(p.lines, "\n")
	if !strings.Contains(all, "1,234.50") || !strings.Contains(all, "yes") {
		t.Fatalf("one-row view lost formats:\n%s", all)
	}
}

// db_alternate_rows shades every second data row; off by default.
func TestDBDisplayAlternateRows(t *testing.T) {
	count := func(p dbPage) (lines []int) {
		for _, h := range p.hls {
			if h["g"] == "TermocodeDbAltRow" {
				lines = append(lines, h["l"].(int))
			}
		}
		return lines
	}
	withConfigDir(t)
	if got := count(dbGridModel(dbDisplayResult()).databasePage()); len(got) != 0 {
		t.Fatalf("alt rows on by default: %v", got)
	}
	writeDBDisplayConfig(t, `{"db_alternate_rows":true}`)
	p := dbGridModel(dbDisplayResult()).databasePage()
	got := count(p)
	if len(got) != 1 || got[0] != p.first+1 {
		t.Fatalf("alt rows at %v, want only line %d (the second data row)", got, p.first+1)
	}
	if !strings.Contains(themeLuaBody, "TermocodeDbAltRow") {
		t.Fatal("no highlight group for TermocodeDbAltRow")
	}
}
