package app

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Display formats of the SQL result grid (DBX-27). The file-only db_*
// keys become one dbclient.Display; a bad value falls back to the default
// so the grid never breaks. Only the screen changes: copy, export and
// edits keep the raw values.

// dbDefaultLOBBytes is the db_lob_bytes default: longer cells are cut.
const dbDefaultLOBBytes = 65536

func strPtrOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return strings.TrimSpace(*p)
}

func (s dbConsoleSettings) display() dbclient.Display {
	d := dbclient.Display{
		Number: strPtrOr(s.NumberFormat, ""),
		Date:   strPtrOr(s.DateFormat, ""),
		Time:   strPtrOr(s.TimeFormat, ""),
	}
	if z, err := dbclient.ParseDisplayZone(strPtrOr(s.TimeZone, "")); err == nil {
		d.Zone = z
	}
	if b := strPtrOr(s.BoolStyle, ""); dbclient.ValidBoolStyle(b) && b != "true/false" {
		d.Bool = b
	}
	d.LOBBytes = intOr(s.LOBBytes, dbDefaultLOBBytes)
	if d.LOBBytes < 0 {
		d.LOBBytes = dbDefaultLOBBytes
	}
	d.LOBBytes = min(d.LOBBytes, 16<<20)
	if g := strings.ToLower(strPtrOr(s.GeometryFmt, "")); g == "wkt" {
		d.Geometry = g
	}
	return d
}

func (s dbConsoleSettings) alternateRows() bool { return s.AltRows != nil && *s.AltRows }

// dbDisplayRow returns row as shown; skip marks cells to keep as they are
// (pending edits). The row is copied only when a cell changes.
func dbDisplayRow(d dbclient.Display, cols []dbclient.Column, row []dbclient.Cell, skip map[int]bool) []dbclient.Cell {
	out := row
	for i, c := range row {
		if skip[i] {
			continue
		}
		var col dbclient.Column
		if i < len(cols) {
			col = cols[i]
		}
		if f := d.Cell(col, c); f != c {
			if &out[0] == &row[0] {
				out = append([]dbclient.Cell(nil), row...)
			}
			out[i] = f
		}
	}
	return out
}
