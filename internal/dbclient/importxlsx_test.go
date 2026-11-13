package dbclient

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZip(t *testing.T, path string, parts map[string]string) {
	t.Helper()
	var b bytes.Buffer
	zw := zip.NewWriter(&b)
	for name, body := range parts {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

const xlsxHead = `<?xml version="1.0" encoding="UTF-8"?>`

// An Excel-made workbook: shared strings, a date style, empty cells that
// shift the columns, a boolean, a formula with its cached value, and a
// second sheet that is not read.
func TestXLSXToCSVReadsTheFirstSheet(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.xlsx")
	writeZip(t, src, map[string]string{
		"xl/workbook.xml":            xlsxHead + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Data" sheetId="1" r:id="rId7"/><sheet name="Other" sheetId="2" r:id="rId8"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": xlsxHead + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId7" Type="x/worksheet" Target="worksheets/data.xml"/><Relationship Id="rId8" Type="x/worksheet" Target="/xl/worksheets/other.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       xlsxHead + `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>id</t></si><si><t>name</t></si><si><t>born</t></si><si><t>ok</t></si><si><r><t>Ann</t></r><r><t xml:space="preserve"> "A", Lee</t></r></si></sst>`,
		"xl/styles.xml":              xlsxHead + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><numFmts count="1"><numFmt numFmtId="164" formatCode="yyyy\-mm\-dd"/></numFmts><cellXfs count="3"><xf numFmtId="0"/><xf numFmtId="14"/><xf numFmtId="164"/></cellXfs></styleSheet>`,
		"xl/worksheets/data.xml": xlsxHead + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>
<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c><c r="C1" t="s"><v>2</v></c><c r="D1" t="s"><v>3</v></c></row>
<row r="2"><c r="A2"><v>1</v></c><c r="B2" t="s"><v>4</v></c><c r="C2" s="1"><v>45000</v></c><c r="D2" t="b"><v>1</v></c></row>
<row r="4"><c r="A4"><f>A2+1</f><v>2.5</v></c><c r="C4" s="2"><v>36526.5</v></c><c r="D4" t="inlineStr"><is><t>x</t></is></c></row>
</sheetData></worksheet>`,
		"xl/worksheets/other.xml": xlsxHead + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>nope</t></is></c></row></sheetData></worksheet>`,
	})
	dst := filepath.Join(dir, "out.csv")
	n, err := XLSXToCSV(src, dst)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	b, _ := os.ReadFile(dst)
	// row 3 is empty (kept as an empty record); dates are ISO; time kept when not midnight
	want := "id,name,born,ok\n1,\"Ann \"\"A\"\", Lee\",2023-03-15,true\n,,,\n2.5,,2000-01-01 12:00:00,x\n"
	if string(b) != want {
		t.Fatalf("csv:\n%q\nwant:\n%q", b, want)
	}
	if st, _ := os.Stat(dst); st.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", st.Mode())
	}
}

// The workbook our own export writes reads back.
func TestXLSXToCSVReadsOurOwnExport(t *testing.T) {
	var b bytes.Buffer
	if err := WriteResult(newXLSXWriter(&b), exportResult()); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "x.xlsx")
	if err := os.WriteFile(src, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "x.csv")
	n, err := XLSXToCSV(src, dst)
	if err != nil || n != 3 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	got, _ := os.ReadFile(dst)
	if !strings.HasPrefix(string(got), "id,name,note\n1,O'Hara,\n") {
		t.Fatalf("csv %q", got)
	}
}

func TestXLSXToCSVRefusesWhatIsNotAWorkbook(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "o.csv")
	txt := filepath.Join(dir, "a.xlsx")
	_ = os.WriteFile(txt, []byte("a,b\n1,2\n"), 0o600)
	if _, err := XLSXToCSV(txt, dst); err == nil || !strings.Contains(err.Error(), "Excel") {
		t.Errorf("text file: %v", err)
	}
	empty := filepath.Join(dir, "e.xlsx")
	writeZip(t, empty, map[string]string{"xl/worksheets/sheet1.xml": xlsxHead + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData></sheetData></worksheet>`})
	if _, err := XLSXToCSV(empty, dst); err == nil || !strings.Contains(err.Error(), "no rows") {
		t.Errorf("empty sheet: %v", err)
	}
	if _, err := XLSXToCSV(filepath.Join(dir, "missing.xlsx"), dst); err == nil {
		t.Error("missing file")
	}
}

// A zip entry that inflates past the cap is refused, not read into memory.
func TestXLSXToCSVStopsAtAHugeSheet(t *testing.T) {
	old := xlsxPartMax
	xlsxPartMax = 1 << 10
	defer func() { xlsxPartMax = old }()
	dir := t.TempDir()
	src := filepath.Join(dir, "big.xlsx")
	writeZip(t, src, map[string]string{"xl/worksheets/sheet1.xml": xlsxHead + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>` + strings.Repeat("a", 4096) + `</t></is></c></row></sheetData></worksheet>`})
	if _, err := XLSXToCSV(src, filepath.Join(dir, "o.csv")); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err = %v", err)
	}
}
