package dbclient

import (
	"encoding/binary"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// cellNumber parses a cell as a finite number; NULL and text give ok=false.
func cellNumber(c Cell) (float64, bool) {
	if c.Null {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(c.Text), 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

func vizCell(r Result, row, col int) (Cell, bool) {
	if row < 0 || row >= len(r.Rows) || col < 0 || col >= len(r.Rows[row]) {
		return Cell{}, false
	}
	return r.Rows[row][col], true
}

// NumericColumns lists the columns whose non-NULL cells are all numbers
// (at least one), the ones a heatmap or chart can use.
func NumericColumns(r Result) []int {
	var out []int
	for col := range r.Columns {
		seen, ok := false, true
		for row := range r.Rows {
			c, in := vizCell(r, row, col)
			if !in || c.Null {
				continue
			}
			if _, isNum := cellNumber(c); !isNum {
				ok = false
				break
			}
			seen = true
		}
		if ok && seen {
			out = append(out, col)
		}
	}
	return out
}

// HeatLevels gives each row a level 0..levels-1 for its value in col, spread
// evenly from the column's smallest to its largest value; -1 marks NULL,
// text or a missing cell. All-equal values share level 0.
func HeatLevels(r Result, col, levels int) []int {
	out := make([]int, len(r.Rows))
	lo, hi, any := math.Inf(1), math.Inf(-1), false
	for i := range r.Rows {
		out[i] = -1
		if c, ok := vizCell(r, i, col); ok {
			if f, ok := cellNumber(c); ok {
				lo, hi, any = math.Min(lo, f), math.Max(hi, f), true
			}
		}
	}
	if !any || levels < 1 {
		return out
	}
	for i := range r.Rows {
		c, _ := vizCell(r, i, col)
		f, ok := cellNumber(c)
		if !ok {
			continue
		}
		if hi == lo {
			out[i] = 0
			continue
		}
		l := int(math.Round((f - lo) / (hi - lo) * float64(levels-1)))
		out[i] = max(0, min(levels-1, l))
	}
	return out
}

var barEighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉"}

// BarChart draws one line per row: the label from labelCol, a bar for the
// value in valueCol scaled so the largest magnitude fills width cells (in
// eighths), then the value. NULL or text values print "<null>"/the text with
// no bar.
func BarChart(r Result, labelCol, valueCol, width int) []string {
	labels := make([]string, len(r.Rows))
	lw, top := 0, 0.0
	for i := range r.Rows {
		c, _ := vizCell(r, i, labelCol)
		labels[i] = strings.ReplaceAll(c.String(), "\n", " ")
		lw = max(lw, utf8.RuneCountInString(labels[i]))
		if v, ok := vizCell(r, i, valueCol); ok {
			if f, ok := cellNumber(v); ok {
				top = math.Max(top, math.Abs(f))
			}
		}
	}
	width = max(1, width)
	out := make([]string, len(r.Rows))
	for i := range r.Rows {
		var b strings.Builder
		b.WriteString(labels[i])
		b.WriteString(strings.Repeat(" ", lw-utf8.RuneCountInString(labels[i])+1))
		v, _ := vizCell(r, i, valueCol)
		if f, ok := cellNumber(v); ok && top > 0 {
			e := int(math.Round(math.Abs(f) / top * float64(width*8)))
			b.WriteString(strings.Repeat("█", e/8))
			b.WriteString(barEighths[e%8])
		}
		b.WriteByte(' ')
		b.WriteString(v.String())
		out[i] = b.String()
	}
	return out
}

// braille dot bits: dotBit[y][x] for the 2x4 dots of one cell.
var dotBit = [4][2]rune{{0x01, 0x08}, {0x02, 0x10}, {0x04, 0x20}, {0x40, 0x80}}

// BrailleLine plots vals as a line over width x height braille cells
// (2x4 dots each): x spreads the values evenly, y goes from the smallest
// (bottom) to the largest (top); neighbouring points are joined vertically.
func BrailleLine(vals []float64, width, height int) []string {
	if len(vals) == 0 || width < 1 || height < 1 {
		return nil
	}
	dw, dh := width*2, height*4
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range vals {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	grid := make([][]rune, height)
	for i := range grid {
		grid[i] = []rune(strings.Repeat(string(rune(0x2800)), width))
	}
	set := func(x, y int) { grid[y/4][x/2] |= dotBit[y%4][x%2] }
	ypos := func(v float64) int {
		if hi == lo {
			return dh - 1
		}
		return dh - 1 - int(math.Round((v-lo)/(hi-lo)*float64(dh-1)))
	}
	prev := -1
	for x := 0; x < dw; x++ {
		i := 0
		if dw > 1 && len(vals) > 1 {
			i = int(math.Round(float64(x) * float64(len(vals)-1) / float64(dw-1)))
		}
		y := ypos(vals[i])
		set(x, y)
		if prev >= 0 {
			for yy := min(prev, y) + 1; yy < max(prev, y); yy++ {
				set(x, yy)
			}
		}
		prev = y
	}
	out := make([]string, height)
	for i, row := range grid {
		out[i] = string(row)
	}
	return out
}

// ColumnValues returns the numbers of one column, skipping NULL and text.
func ColumnValues(r Result, col int) []float64 {
	var out []float64
	for i := range r.Rows {
		if c, ok := vizCell(r, i, col); ok {
			if f, ok := cellNumber(c); ok {
				out = append(out, f)
			}
		}
	}
	return out
}

// geom is a decoded WKB geometry (2D view; extra Z/M values are kept).
type geom struct {
	kind  int
	srid  uint32
	dims  int
	pts   [][]float64 // Point/LineString
	rings [][][]float64
	parts []geom
}

type geoWKBReader struct {
	b     []byte
	depth int
}

func (r *geoWKBReader) take(n int) ([]byte, bool) {
	if n < 0 || len(r.b) < n {
		return nil, false
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out, true
}

func (r *geoWKBReader) geom() (geom, bool) {
	var g geom
	if r.depth++; r.depth > 8 {
		return g, false
	}
	defer func() { r.depth-- }()
	bo, ok := r.take(1)
	if !ok || bo[0] > 1 {
		return g, false
	}
	var ord binary.ByteOrder = binary.BigEndian
	if bo[0] == 1 {
		ord = binary.LittleEndian
	}
	u32 := func() (uint32, bool) {
		b, ok := r.take(4)
		if !ok {
			return 0, false
		}
		return ord.Uint32(b), true
	}
	t, ok := u32()
	if !ok {
		return g, false
	}
	g.dims = 2
	if t&0x80000000 != 0 {
		g.dims++
	}
	if t&0x40000000 != 0 {
		g.dims++
	}
	if t&0x20000000 != 0 {
		if g.srid, ok = u32(); !ok {
			return g, false
		}
	}
	t &= 0x0fffffff
	switch t / 1000 { // ISO Z / M / ZM codes
	case 1, 2:
		g.dims = 3
	case 3:
		g.dims = 4
	}
	g.kind = int(t % 1000)
	point := func() ([]float64, bool) {
		b, ok := r.take(8 * g.dims)
		if !ok {
			return nil, false
		}
		p := make([]float64, g.dims)
		for i := range p {
			p[i] = math.Float64frombits(ord.Uint64(b[8*i:]))
		}
		return p, true
	}
	points := func() ([][]float64, bool) {
		n, ok := u32()
		if !ok || uint64(n)*uint64(8*g.dims) > uint64(len(r.b)) {
			return nil, false
		}
		out := make([][]float64, n)
		for i := range out {
			if out[i], ok = point(); !ok {
				return nil, false
			}
		}
		return out, true
	}
	switch g.kind {
	case 1:
		p, ok := point()
		g.pts = [][]float64{p}
		return g, ok
	case 2:
		g.pts, ok = points()
		return g, ok
	case 3:
		n, ok := u32()
		if !ok || uint64(n)*4 > uint64(len(r.b)) {
			return g, false
		}
		g.rings = make([][][]float64, n)
		for i := range g.rings {
			if g.rings[i], ok = points(); !ok {
				return g, false
			}
		}
		return g, true
	case 4, 5, 6, 7:
		n, ok := u32()
		if !ok || uint64(n)*5 > uint64(len(r.b)) {
			return g, false
		}
		g.parts = make([]geom, n)
		for i := range g.parts {
			if g.parts[i], ok = r.geom(); !ok {
				return g, false
			}
		}
		return g, true
	}
	return g, false
}

func decodeGeo(s string) (geom, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "\\x")
	if len(s) < 10 || len(s) > 1<<22 {
		return geom{}, false
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return geom{}, false
	}
	r := &geoWKBReader{b: b}
	g, ok := r.geom()
	if !ok || len(r.b) != 0 {
		return geom{}, false
	}
	return g, true
}

var geoNames = map[int]string{1: "POINT", 2: "LINESTRING", 3: "POLYGON", 4: "MULTIPOINT", 5: "MULTILINESTRING", 6: "MULTIPOLYGON", 7: "GEOMETRYCOLLECTION"}
var geoJSONNames = map[int]string{1: "Point", 2: "LineString", 3: "Polygon", 4: "MultiPoint", 5: "MultiLineString", 6: "MultiPolygon", 7: "GeometryCollection"}

func geoNum(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func wktPts(b *strings.Builder, pts [][]float64) {
	b.WriteByte('(')
	for i, p := range pts {
		if i > 0 {
			b.WriteByte(',')
		}
		for j, f := range p {
			if j > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(geoNum(f))
		}
	}
	b.WriteByte(')')
}

func wktBody(b *strings.Builder, g geom) {
	switch g.kind {
	case 1, 2:
		wktPts(b, g.pts)
	case 3:
		b.WriteByte('(')
		for i, ring := range g.rings {
			if i > 0 {
				b.WriteByte(',')
			}
			wktPts(b, ring)
		}
		b.WriteByte(')')
	default:
		b.WriteByte('(')
		for i, p := range g.parts {
			if i > 0 {
				b.WriteByte(',')
			}
			if g.kind == 7 {
				b.WriteString(geoNames[p.kind])
			}
			wktBody(b, p)
		}
		b.WriteByte(')')
	}
}

// GeoWKT turns a hex WKB/EWKB value (as a geometry column prints, with or
// without 0x) into WKT text, with "SRID=n;" when the value carries one.
func GeoWKT(s string) (string, bool) {
	g, ok := decodeGeo(s)
	if !ok {
		return "", false
	}
	var b strings.Builder
	if g.srid != 0 {
		b.WriteString("SRID=" + strconv.FormatUint(uint64(g.srid), 10) + ";")
	}
	b.WriteString(geoNames[g.kind])
	switch g.dims {
	case 3:
		b.WriteString(" Z ")
	case 4:
		b.WriteString(" ZM ")
	}
	wktBody(&b, g)
	return b.String(), true
}

func jsonPts(b *strings.Builder, pts [][]float64) {
	b.WriteByte('[')
	for i, p := range pts {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('[')
		for j, f := range p {
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(geoNum(f))
		}
		b.WriteByte(']')
	}
	b.WriteByte(']')
}

func jsonCoords(b *strings.Builder, g geom) {
	switch g.kind {
	case 1:
		var one strings.Builder
		jsonPts(&one, g.pts)
		s := one.String()
		b.WriteString(s[1 : len(s)-1])
	case 2:
		jsonPts(b, g.pts)
	case 3:
		b.WriteByte('[')
		for i, ring := range g.rings {
			if i > 0 {
				b.WriteByte(',')
			}
			jsonPts(b, ring)
		}
		b.WriteByte(']')
	default:
		b.WriteByte('[')
		for i, p := range g.parts {
			if i > 0 {
				b.WriteByte(',')
			}
			jsonCoords(b, p)
		}
		b.WriteByte(']')
	}
}

func jsonGeom(b *strings.Builder, g geom) {
	b.WriteString(`{"type":"` + geoJSONNames[g.kind] + `",`)
	if g.kind == 7 {
		b.WriteString(`"geometries":[`)
		for i, p := range g.parts {
			if i > 0 {
				b.WriteByte(',')
			}
			jsonGeom(b, p)
		}
		b.WriteString("]}")
		return
	}
	b.WriteString(`"coordinates":`)
	jsonCoords(b, g)
	b.WriteByte('}')
}

// GeoJSON turns a hex WKB/EWKB value into a GeoJSON geometry object.
func GeoJSON(s string) (string, bool) {
	g, ok := decodeGeo(s)
	if !ok {
		return "", false
	}
	var b strings.Builder
	jsonGeom(&b, g)
	return b.String(), true
}
