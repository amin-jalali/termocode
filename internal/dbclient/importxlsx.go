package dbclient

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
)

// Excel import (DBX-37): the first sheet of an .xlsx workbook becomes a CSV
// file with the sheet's first row as the header, so it goes through the CSV
// import's preview, mapping and one-transaction insert (as JSON does). Read
// with the standard library only. Dates become ISO text, formulas give their
// last stored value, a missing cell is empty.

// xlsxPartMax caps one unzipped part (a zip can inflate without limit).
var xlsxPartMax int64 = 256 << 20

const (
	xlsxMaxCols = 16384
	xlsxMaxRows = 1048576
)

func xlsxPart(zr *zip.ReadCloser, name string) ([]byte, bool, error) {
	name = strings.TrimPrefix(name, "/")
	for _, f := range zr.File {
		if !strings.EqualFold(f.Name, name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, true, err
		}
		defer rc.Close()
		b, err := io.ReadAll(io.LimitReader(rc, xlsxPartMax+1))
		if err != nil {
			return nil, true, err
		}
		if int64(len(b)) > xlsxPartMax {
			return nil, true, errors.New("the Excel sheet is too large")
		}
		return b, true, nil
	}
	return nil, false, nil
}

// XLSXToCSV converts the first sheet of the workbook src into the CSV file
// dst (0600) and returns the number of data rows (the first row is the header).
func XLSXToCSV(src, dst string) (int, error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		if errors.Is(err, zip.ErrFormat) {
			return 0, errors.New("that is not an Excel (.xlsx) file")
		}
		return 0, errors.New(pathErr(err))
	}
	defer zr.Close()
	sheetName, err := xlsxFirstSheet(zr)
	if err != nil {
		return 0, err
	}
	sheet, ok, err := xlsxPart(zr, sheetName)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("that is not an Excel (.xlsx) file")
	}
	var shared []string
	if b, ok, err := xlsxPart(zr, "xl/sharedStrings.xml"); err != nil {
		return 0, err
	} else if ok {
		shared = xlsxSharedStrings(b)
	}
	var dates []bool
	if b, ok, err := xlsxPart(zr, "xl/styles.xml"); err != nil {
		return 0, err
	} else if ok {
		dates = xlsxDateStyles(b)
	}
	rows, err := xlsxRows(sheet, shared, dates)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, errors.New("the sheet has no rows")
	}
	width := 0
	for _, r := range rows {
		width = max(width, len(r))
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	w := csv.NewWriter(out)
	for _, r := range rows {
		for len(r) < width {
			r = append(r, "")
		}
		_ = w.Write(r)
	}
	w.Flush()
	if err := w.Error(); err != nil {
		out.Close()
		return 0, err
	}
	return len(rows) - 1, out.Close()
}

// xlsxFirstSheet is the part name of the first sheet of the workbook.
func xlsxFirstSheet(zr *zip.ReadCloser) (string, error) {
	const fallback = "xl/worksheets/sheet1.xml"
	wb, ok, err := xlsxPart(zr, "xl/workbook.xml")
	if err != nil {
		return "", err
	}
	if !ok {
		return fallback, nil
	}
	var rid string
	dec := xml.NewDecoder(bytes.NewReader(wb))
	for rid == "" {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "sheet" {
			for _, a := range se.Attr {
				if a.Name.Local == "id" {
					rid = a.Value
				}
			}
			break
		}
	}
	rels, ok, err := xlsxPart(zr, "xl/_rels/workbook.xml.rels")
	if err != nil {
		return "", err
	}
	if rid == "" || !ok {
		return fallback, nil
	}
	dec = xml.NewDecoder(bytes.NewReader(rels))
	for {
		tok, err := dec.Token()
		if err != nil {
			return fallback, nil
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "Relationship" {
			continue
		}
		var id, target string
		for _, a := range se.Attr {
			switch a.Name.Local {
			case "Id":
				id = a.Value
			case "Target":
				target = a.Value
			}
		}
		if id != rid {
			continue
		}
		if strings.HasPrefix(target, "/") {
			return strings.TrimPrefix(target, "/"), nil
		}
		return path.Join("xl", target), nil
	}
}

// xlsxSharedStrings lists the shared strings: all text of an <si>, except
// the phonetic hints.
func xlsxSharedStrings(b []byte) []string {
	var out []string
	var cur strings.Builder
	inSI, inT, inPh := false, false, false
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inSI = true
				cur.Reset()
			case "rPh":
				inPh = true
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				out = append(out, cur.String())
				inSI = false
			case "rPh":
				inPh = false
			case "t":
				inT = false
			}
		case xml.CharData:
			if inSI && inT && !inPh {
				cur.Write(t)
			}
		}
	}
}

// xlsxDateStyles says, per cell style index, whether it shows a date or time.
func xlsxDateStyles(b []byte) []bool {
	custom := map[int]string{}
	var xfs []int
	inXfs := false
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "numFmt":
				id, code := -1, ""
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "numFmtId":
						id, _ = strconv.Atoi(a.Value)
					case "formatCode":
						code = a.Value
					}
				}
				custom[id] = code
			case "cellXfs":
				inXfs = true
			case "xf":
				if inXfs {
					id := 0
					for _, a := range t.Attr {
						if a.Name.Local == "numFmtId" {
							id, _ = strconv.Atoi(a.Value)
						}
					}
					xfs = append(xfs, id)
				}
			}
		case xml.EndElement:
			if t.Name.Local == "cellXfs" {
				inXfs = false
			}
		}
	}
	out := make([]bool, len(xfs))
	for i, id := range xfs {
		out[i] = xlsxIsDateFormat(id, custom[id])
	}
	return out
}

func xlsxIsDateFormat(id int, code string) bool {
	switch {
	case id >= 14 && id <= 22, id >= 27 && id <= 36, id >= 45 && id <= 47, id >= 50 && id <= 58:
		return true
	case id < 164 || code == "":
		return false
	}
	var b strings.Builder
	inQuote, inBracket, esc := false, false, false
	for _, r := range code {
		switch {
		case esc:
			esc = false
		case r == '\\' || r == '_' || r == '*':
			esc = true
		case r == '"':
			inQuote = !inQuote
		case inQuote:
		case r == '[':
			inBracket = true
		case r == ']':
			inBracket = false
		case !inBracket:
			b.WriteRune(r)
		}
	}
	return strings.ContainsAny(strings.ToLower(b.String()), "ymdhs")
}

// xlsxDate shows an Excel serial as ISO text (a time alone when below 1).
func xlsxDate(v string) (string, bool) {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 2958465 {
		return "", false
	}
	days := math.Floor(f)
	secs := int64(math.Round((f - days) * 86400))
	t := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(days)).Add(time.Duration(secs) * time.Second)
	switch {
	case f < 1:
		return t.Format("15:04:05"), true
	case secs == 0:
		return t.Format("2006-01-02"), true
	}
	return t.Format("2006-01-02 15:04:05"), true
}

// xlsxColumn is the 0-based column of a reference like "AB12".
func xlsxColumn(ref string) int {
	n := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			if r >= 'a' && r <= 'z' {
				r -= 32
			} else {
				break
			}
		}
		n = n*26 + int(r-'A'+1)
	}
	return n - 1
}

// xlsxRows reads the sheetData into rows of text, keeping empty rows between.
func xlsxRows(b []byte, shared []string, dates []bool) ([][]string, error) {
	var rows [][]string
	var cur []string
	rowNo, col, last := 0, -1, -1
	var typ string
	style := -1
	var val strings.Builder
	var hasVal, inV, inT, inIS bool
	flush := func() {
		if col < 0 {
			return
		}
		text := ""
		if hasVal {
			v := val.String()
			switch typ {
			case "s":
				if i, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && i >= 0 && i < len(shared) {
					text = shared[i]
				}
			case "b":
				text = "false"
				if strings.TrimSpace(v) == "1" {
					text = "true"
				}
			case "str", "inlineStr", "e":
				text = v
			default:
				text = strings.TrimSpace(v)
				if style >= 0 && style < len(dates) && dates[style] {
					if d, ok := xlsxDate(text); ok {
						text = d
					}
				}
			}
		}
		for len(cur) <= col {
			cur = append(cur, "")
		}
		cur[col] = text
		col = -1
	}
	dec := xml.NewDecoder(bytes.NewReader(b))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("the sheet cannot be read: %v", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "row":
				next := rowNo + 1
				for _, a := range t.Attr {
					if a.Name.Local == "r" {
						if n, err := strconv.Atoi(a.Value); err == nil && n > 0 {
							next = n
						}
					}
				}
				if next > xlsxMaxRows {
					return nil, errors.New("the sheet has too many rows")
				}
				for rowNo+1 < next && len(rows) > 0 {
					rows = append(rows, nil)
					rowNo++
				}
				rowNo = next
				cur, last = nil, -1
			case "c":
				ref := ""
				typ, style = "", -1
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "r":
						ref = a.Value
					case "t":
						typ = a.Value
					case "s":
						if n, err := strconv.Atoi(a.Value); err == nil {
							style = n
						}
					}
				}
				next := last + 1
				if ref != "" {
					next = xlsxColumn(ref)
				}
				if next < 0 || next >= xlsxMaxCols {
					return nil, errors.New("the sheet has too many columns")
				}
				col, last = next, next
				val.Reset()
				hasVal = false
			case "v":
				inV = true
			case "is":
				inIS = true
			case "t":
				inT = true
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "c":
				flush()
			case "v":
				inV = false
			case "is":
				inIS = false
			case "t":
				inT = false
			case "row":
				rows = append(rows, cur)
			}
		case xml.CharData:
			if col >= 0 && (inV || (inIS && inT)) {
				val.Write(t)
				hasVal = true
			}
		}
	}
	for len(rows) > 0 && len(rows[len(rows)-1]) == 0 {
		rows = rows[:len(rows)-1]
	}
	return rows, nil
}
