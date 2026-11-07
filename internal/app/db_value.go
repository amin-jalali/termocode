package app

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // DecodeConfig for the value page
	_ "image/jpeg" // DecodeConfig for the value page
	"io"
	"math"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
)

// DBX-10: aggregates of a column (=) and the value viewers of v.

// dbValueHexMax is how many bytes of a binary value the hex dump shows.
const dbValueHexMax = 16 << 10

// dbAggregate sums up cells: count of the non-null values, and for
// numbers sum, avg, min and max; for text min and max by text order.
func dbAggregate(cells []dbclient.Cell) string {
	var vals []string
	nulls := 0
	for _, c := range cells {
		if c.Null {
			nulls++
			continue
		}
		vals = append(vals, c.Text)
	}
	parts := []string{"count " + strconv.Itoa(len(vals))}
	if len(vals) > 0 {
		nums := make([]float64, 0, len(vals))
		for _, v := range vals {
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				nums = nil
				break
			}
			nums = append(nums, f)
		}
		if nums != nil {
			sum, lo, hi := 0.0, nums[0], nums[0]
			for _, f := range nums {
				sum += f
				lo, hi = min(lo, f), max(hi, f)
			}
			parts = append(parts, "sum "+dbNum(sum), "avg "+dbNum(sum/float64(len(nums))), "min "+dbNum(lo), "max "+dbNum(hi))
		} else {
			lo, hi := vals[0], vals[0]
			for _, v := range vals {
				lo, hi = min(lo, v), max(hi, v)
			}
			parts = append(parts, "min "+dbOneLine(lo), "max "+dbOneLine(hi))
		}
	}
	if nulls > 0 {
		parts = append(parts, strconv.Itoa(nulls)+" null")
	}
	return strings.Join(parts, " · ")
}

// dbNum prints f short: at most 6 decimals, no trailing zeros.
func dbNum(f float64) string {
	s := strconv.FormatFloat(f, 'f', 6, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		s = "0"
	}
	return s
}

func dbBytes(n int) string {
	if n < 1024 {
		return strconv.Itoa(n) + " B"
	}
	if n < 1<<20 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

// dbCellView is the text of the value page for c: a binary value as a hex
// dump (an image says its kind and size first), XML and JSON pretty, other
// text as it is.
func dbCellView(c dbclient.Cell) string {
	if c.Null {
		return c.String()
	}
	if c.Kind == "binary" && strings.HasPrefix(c.Text, "0x") {
		if b, err := hex.DecodeString(c.Text[2:]); err == nil {
			return dbBinaryView(b)
		}
	}
	text := c.Text
	if wkt, ok := dbclient.GeoWKT(text); ok { // DBX-35: a geometry value as text
		js, _ := dbclient.GeoJSON(text)
		return "Geometry\n\nWKT:\n" + wkt + "\n\nGeoJSON:\n" + js
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, []byte(text), "", "  ") == nil {
		return pretty.String()
	}
	if x, ok := dbPrettyXML(text); ok {
		return x
	}
	return text
}

func dbBinaryView(b []byte) string {
	head := "Binary · " + dbBytes(len(b))
	if kind, w, h := dbImageInfo(b); kind != "" {
		head = kind + " image"
		if w > 0 && h > 0 {
			head += fmt.Sprintf(" · %d × %d", w, h)
		}
		head += " · " + dbBytes(len(b))
	}
	shown := b
	if len(shown) > dbValueHexMax {
		shown = shown[:dbValueHexMax]
	}
	out := head + "\n\n" + strings.TrimRight(hex.Dump(shown), "\n")
	if rest := len(b) - len(shown); rest > 0 {
		out += fmt.Sprintf("\n… %d more bytes", rest)
	}
	return out
}

// dbImageInfo tells the image kind by its first bytes, and its size when
// it can be read.
func dbImageInfo(b []byte) (kind string, w, h int) {
	switch {
	case bytes.HasPrefix(b, []byte("\x89PNG\r\n\x1a\n")):
		if len(b) >= 24 && string(b[12:16]) == "IHDR" {
			return "PNG", int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24]))
		}
		return "PNG", 0, 0
	case bytes.HasPrefix(b, []byte("\xff\xd8\xff")):
		kind = "JPEG"
	case bytes.HasPrefix(b, []byte("GIF87a")), bytes.HasPrefix(b, []byte("GIF89a")):
		kind = "GIF"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "WebP", 0, 0
	default:
		return "", 0, 0
	}
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(b)); err == nil {
		w, h = cfg.Width, cfg.Height
	}
	return kind, w, h
}

// dbPrettyXML indents a well-formed XML document; ok is false for
// anything else.
func dbPrettyXML(s string) (string, bool) {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "<") || !strings.HasSuffix(t, ">") {
		return "", false
	}
	dec := xml.NewDecoder(strings.NewReader(t))
	dec.Strict = true
	var out bytes.Buffer
	enc := xml.NewEncoder(&out)
	enc.Indent("", "  ")
	depth, elems := 0, 0
	for {
		tok, err := dec.RawToken()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", false
		}
		switch v := tok.(type) {
		case xml.StartElement:
			depth++
			elems++
			v.Name = dbXMLName(v.Name)
			attrs := make([]xml.Attr, len(v.Attr))
			for i, a := range v.Attr {
				attrs[i] = xml.Attr{Name: dbXMLName(a.Name), Value: a.Value}
			}
			v.Attr = attrs
			tok = v
		case xml.EndElement:
			depth--
			v.Name = dbXMLName(v.Name)
			tok = v
		case xml.CharData:
			if len(bytes.TrimSpace(v)) == 0 {
				continue
			}
		}
		if err := enc.EncodeToken(xml.CopyToken(tok)); err != nil {
			return "", false
		}
	}
	if depth != 0 || elems == 0 || enc.Flush() != nil {
		return "", false
	}
	return out.String(), true
}

// dbXMLName keeps a prefix as written (RawToken does not resolve it).
func dbXMLName(n xml.Name) xml.Name {
	if n.Space == "" {
		return n
	}
	return xml.Name{Local: n.Space + ":" + n.Local}
}

// databaseAggregate is = on the result page: a toast with the aggregates
// of column col over the page's rows from..to (to < 0: the whole page).
func (m *Model) databaseAggregate(from, col, to int) tea.Cmd {
	s := m.dbConsole()
	if s.running != nil || s.shown == nil || s.shown.plan != nil || s.shown.redis != nil || s.shown.result.Err != nil || s.view != dbViewGrid {
		return nil
	}
	page := s.pageResult()
	if s.trans {
		col, from, to = from, 0, -1 // the cursor line is the column
	}
	if col < 0 || col >= len(page.Columns) || len(page.Rows) == 0 {
		return nil
	}
	if to < 0 {
		from, to = 0, len(page.Rows)-1
	}
	from, to = max(from, 0), min(to, len(page.Rows)-1)
	if from > to {
		return nil
	}
	cells := make([]dbclient.Cell, 0, to-from+1)
	for _, row := range page.Rows[from : to+1] {
		if col < len(row) {
			cells = append(cells, row[col])
		}
	}
	n := len(cells)
	title := fmt.Sprintf("%s · %d rows", dbOneLine(page.Columns[col].Name), n)
	if n == 1 {
		title = fmt.Sprintf("%s · 1 row", dbOneLine(page.Columns[col].Name))
	}
	return m.pushToastDetail(toast.Info, title, dbAggregate(cells))
}
