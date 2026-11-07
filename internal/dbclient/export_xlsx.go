package dbclient

import (
	"archive/zip"
	"bytes"
	"io"
	"strconv"
	"strings"
)

// Excel export (DBX-19): a minimal .xlsx workbook written with the
// standard library (a zip of a few XML parts). Rows stream into the sheet
// part as they come; numbers stay numbers, NULL is an empty cell, every
// other value is an inline string (never a formula).

// FormatXLSX is the Excel workbook export.
const FormatXLSX ExportFormat = "xlsx"

const (
	xlsxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`
	xlsxRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`
	xlsxWorkbook = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Export" sheetId="1" r:id="rId1"/></sheets></workbook>`
	xlsxWorkbookRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`
)

type xlsxWriter struct {
	zw    *zip.Writer
	sheet io.Writer
	cols  int
	row   int
	buf   bytes.Buffer
}

func newXLSXWriter(w io.Writer) *xlsxWriter { return &xlsxWriter{zw: zip.NewWriter(w)} }

func (x *xlsxWriter) part(name, body string) error {
	f, err := x.zw.Create(name)
	if err != nil {
		return err
	}
	_, err = io.WriteString(f, body)
	return err
}

func (x *xlsxWriter) Columns(cols []Column) error {
	for _, p := range [][2]string{
		{"[Content_Types].xml", xlsxContentTypes},
		{"_rels/.rels", xlsxRels},
		{"xl/workbook.xml", xlsxWorkbook},
		{"xl/_rels/workbook.xml.rels", xlsxWorkbookRels},
	} {
		if err := x.part(p[0], p[1]); err != nil {
			return err
		}
	}
	f, err := x.zw.Create("xl/worksheets/sheet1.xml")
	if err != nil {
		return err
	}
	x.sheet = f
	if _, err := io.WriteString(f, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+"\n"+`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`); err != nil {
		return err
	}
	x.cols = len(cols)
	cells := make([]Cell, len(cols))
	for i, c := range cols {
		cells[i] = Cell{Text: c.Name}
	}
	return x.Row(cells)
}

// xlsxNumber: a number cell only for a plain decimal the sheet reads back
// the same (no leading zeros, no huge integers that would lose digits).
func xlsxNumber(c Cell) bool {
	if c.Kind != "number" || len(c.Text) > 15 {
		return false
	}
	t := strings.TrimPrefix(c.Text, "-")
	if len(t) > 1 && t[0] == '0' && t[1] != '.' {
		return false
	}
	_, err := strconv.ParseFloat(c.Text, 64)
	return err == nil
}

func (x *xlsxWriter) Row(row []Cell) error {
	x.row++
	x.buf.Reset()
	x.buf.WriteString(`<row r="` + strconv.Itoa(x.row) + `">`)
	for i := 0; i < x.cols; i++ {
		if i >= len(row) || row[i].Null {
			continue
		}
		switch {
		case xlsxNumber(row[i]):
			x.buf.WriteString(`<c><v>` + row[i].Text + `</v></c>`)
		default:
			x.buf.WriteString(`<c t="inlineStr"><is><t xml:space="preserve">`)
			xmlEscape(&x.buf, row[i].Text)
			x.buf.WriteString(`</t></is></c>`)
		}
	}
	x.buf.WriteString(`</row>`)
	_, err := x.sheet.Write(x.buf.Bytes())
	return err
}

func (x *xlsxWriter) End() error {
	if x.sheet == nil {
		if err := x.Columns(nil); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(x.sheet, `</sheetData></worksheet>`); err != nil {
		return err
	}
	return x.zw.Close()
}
