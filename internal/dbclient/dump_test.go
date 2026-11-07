package dbclient

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// DBX-19: dump / restore command lines, the Excel export and JSON import.

func TestDumpCommandKeepsPasswordInEnv(t *testing.T) {
	c := Conn{Driver: "postgres", Host: "db", Port: 5433, User: "app", Database: "shop"}
	cmd, err := DumpCommand(c, CLIOptions{Password: "s3cret"}, "/tmp/shop.dump")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, " ")
	if cmd.Tool != "pg_dump" || !strings.Contains(args, "-Fc") || !strings.Contains(args, "-f /tmp/shop.dump") || !strings.Contains(args, "-d shop") {
		t.Errorf("pg_dump = %s %s", cmd.Tool, args)
	}
	if strings.Contains(args, "s3cret") || !strings.Contains(strings.Join(cmd.Env, " "), "PGPASSWORD=s3cret") {
		t.Errorf("password: args=%q env=%q", args, cmd.Env)
	}
	plain, _ := DumpCommand(c, CLIOptions{}, "/tmp/shop.sql")
	if !strings.Contains(strings.Join(plain.Args, " "), "-Fp") {
		t.Errorf(".sql dump is not plain: %v", plain.Args)
	}

	my, err := DumpCommand(Conn{Driver: "mysql", Host: "h", User: "u", Database: "shop"}, CLIOptions{Password: "p"}, "/tmp/shop.sql")
	if err != nil {
		t.Fatal(err)
	}
	margs := strings.Join(my.Args, " ")
	if my.Tool != "mysqldump" || !strings.Contains(margs, "--result-file=/tmp/shop.sql") || !strings.HasSuffix(margs, "shop") || strings.Contains(margs, " p ") {
		t.Errorf("mysqldump = %s", margs)
	}
	if _, err := DumpCommand(Conn{Driver: "postgres"}, CLIOptions{}, "/tmp/x.dump"); err == nil {
		t.Error("no database: no error")
	}
	if _, err := DumpCommand(Conn{Driver: "redis", Database: "x"}, CLIOptions{}, "/tmp/x"); err == nil {
		t.Error("redis dump: no error")
	}
}

func TestRestoreCommand(t *testing.T) {
	c := Conn{Driver: "postgres", Host: "db", User: "app", Database: "shop"}
	r, err := RestoreCommand(c, CLIOptions{}, "/tmp/shop.dump")
	if err != nil || r.Tool != "pg_restore" || !strings.HasSuffix(strings.Join(r.Args, " "), "/tmp/shop.dump") {
		t.Errorf("pg_restore = %+v %v", r, err)
	}
	r, err = RestoreCommand(c, CLIOptions{}, "/tmp/shop.sql")
	if err != nil || r.Tool != "psql" || !strings.Contains(strings.Join(r.Args, " "), "ON_ERROR_STOP=1") {
		t.Errorf("psql restore = %+v %v", r, err)
	}
	my, err := RestoreCommand(Conn{Driver: "mysql", Database: "shop"}, CLIOptions{}, "/tmp/shop.sql")
	if err != nil || my.Tool != "mysql" || my.Stdin != "/tmp/shop.sql" {
		t.Errorf("mysql restore = %+v %v", my, err)
	}
	if _, err := RestoreCommand(Conn{Driver: "mysql", Database: "shop"}, CLIOptions{}, "/tmp/shop.dump"); err == nil {
		t.Error("mysql restore of a custom dump: no error")
	}
	ro := c
	ro.ReadOnly = true
	if _, err := RestoreCommand(ro, CLIOptions{}, "/tmp/shop.sql"); err == nil {
		t.Error("restore into a read-only connection: no error")
	}
}

func TestXLSXExport(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewRowWriter(FormatXLSX, &buf, ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Columns([]Column{{Name: "id"}, {Name: "name"}})
	_ = w.Row([]Cell{{Text: "1", Kind: "number"}, {Text: "a<b"}})
	_ = w.Row([]Cell{{Text: "2", Kind: "number"}, {Null: true}})
	if err := w.End(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	names := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		names[f.Name] = string(b)
	}
	for _, n := range []string{"[Content_Types].xml", "_rels/.rels", "xl/workbook.xml", "xl/_rels/workbook.xml.rels", "xl/worksheets/sheet1.xml"} {
		if _, ok := names[n]; !ok {
			t.Errorf("missing %s", n)
		}
	}
	sheet := names["xl/worksheets/sheet1.xml"]
	for _, want := range []string{">id</t>", ">a&lt;b</t>", "<v>2</v>"} {
		if !strings.Contains(sheet, want) {
			t.Errorf("sheet has no %q: %s", want, sheet)
		}
	}
	found := false
	for _, f := range ExportFormats {
		found = found || f.Format == FormatXLSX
	}
	if !found {
		t.Error("Excel is not in the export picker")
	}
}

func TestJSONToCSV(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.json")
	_ = os.WriteFile(src, []byte(`[{"id":1,"name":"a","tags":["x"]},{"id":2,"extra":true,"name":null}]`), 0o600)
	dst := filepath.Join(dir, "out.csv")
	n, err := JSONToCSV(src, dst)
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	b, _ := os.ReadFile(dst)
	want := "id,name,tags,extra\n1,a,\"[\"\"x\"\"]\",\n2,,,true\n"
	if string(b) != want {
		t.Errorf("csv = %q, want %q", b, want)
	}

	lines := filepath.Join(dir, "in.jsonl")
	_ = os.WriteFile(lines, []byte("{\"a\":1}\n\n{\"a\":2}\n"), 0o600)
	if n, err := JSONToCSV(lines, dst); err != nil || n != 2 {
		t.Errorf("jsonl n=%d err=%v", n, err)
	}
	bad := filepath.Join(dir, "bad.json")
	_ = os.WriteFile(bad, []byte(`[1,2]`), 0o600)
	if _, err := JSONToCSV(bad, dst); err == nil {
		t.Error("array of numbers: no error")
	}
}
