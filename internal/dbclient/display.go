package dbclient

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Display is how the result grid shows values (DBX-27). It changes only the
// text on screen: the stored result, copy, export and edits keep the raw
// value. The zero value shows every cell as it came.
type Display struct {
	Number   string         // "#,##0.00"-style pattern for number cells; "" keeps the text
	Date     string         // pattern for DATE columns (yyyy MM dd …); "" keeps
	Time     string         // pattern for timestamps (yyyy MM dd HH mm ss SSS XXX …); "" keeps
	Zone     *time.Location // timestamps are shown in this zone; nil keeps their own
	Bool     string         // "" (true/false), "yes/no", "1/0" or "check"
	LOBBytes int            // >0: longer text and binary values are cut to this many bytes
	Geometry string         // "" (hex) or "wkt"
}

// BoolStyles are the values of db_bool_style.
var BoolStyles = []string{"true/false", "yes/no", "1/0", "check"}

// ValidBoolStyle reports whether s is one of BoolStyles.
func ValidBoolStyle(s string) bool {
	for _, b := range BoolStyles {
		if s == b {
			return true
		}
	}
	return false
}

// ParseDisplayZone reads db_time_zone: "" keeps each value's zone (nil),
// "local" is this machine's zone, otherwise a name like UTC or Europe/Berlin.
func ParseDisplayZone(name string) (*time.Location, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "":
		return nil, nil
	case "local":
		return time.Local, nil
	case "utc":
		return time.UTC, nil
	}
	return time.LoadLocation(strings.TrimSpace(name))
}

// Cell returns c as the grid shows it in column col.
func (d Display) Cell(col Column, c Cell) Cell {
	if c.Null {
		return c
	}
	switch {
	case c.Kind == "number":
		if d.Number != "" {
			if s, ok := formatNumber(d.Number, c.Text); ok {
				c.Text = s
			}
		}
		return c
	case c.Kind == "bool":
		if s, ok := formatBool(d.Bool, c.Text); ok {
			c.Text = s
		}
		return c
	case c.Kind == "time":
		if d.Time == "" && d.Zone == nil {
			return c
		}
		t, err := time.Parse(time.RFC3339Nano, c.Text)
		if err != nil {
			return c
		}
		if d.Zone != nil {
			t = t.In(d.Zone)
		}
		if d.Time == "" {
			c.Text = t.Format(time.RFC3339Nano)
		} else {
			c.Text = formatTime(d.Time, t)
		}
		return c
	}
	if d.Date != "" && isDateType(col.Type) {
		if t, err := time.Parse("2006-01-02", c.Text); err == nil {
			c.Text = formatTime(d.Date, t)
			return c
		}
	}
	if d.Geometry == "wkt" && isGeometryType(col.Type) {
		if s, ok := geometryWKT(c.Text); ok {
			c.Text = s
			return c
		}
	}
	if d.LOBBytes > 0 {
		c.Text = cutLOB(c, d.LOBBytes)
	}
	return c
}

func isDateType(t string) bool {
	t = strings.ToUpper(strings.TrimSpace(t))
	return t == "DATE" || strings.HasPrefix(t, "DATE ")
}

func isGeometryType(t string) bool {
	t = strings.ToUpper(t)
	for _, name := range []string{"GEOMETRY", "GEOGRAPHY", "POINT", "LINESTRING", "POLYGON"} {
		if strings.Contains(t, name) {
			return true
		}
	}
	return false
}

// cutLOB cuts a long text or binary value to max bytes and says how long
// the whole value is.
func cutLOB(c Cell, max int) string {
	if c.Kind == "binary" && strings.HasPrefix(c.Text, "0x") {
		n := (len(c.Text) - 2) / 2
		if n <= max {
			return c.Text
		}
		return c.Text[:2+2*max] + fmt.Sprintf("… (%d bytes)", n)
	}
	if c.Kind != "" && c.Kind != "json" || len(c.Text) <= max {
		return c.Text
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(c.Text[cut]) {
		cut--
	}
	return c.Text[:cut] + fmt.Sprintf("… (%d bytes)", len(c.Text))
}

func formatBool(style, text string) (string, bool) {
	var v bool
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "true", "t", "1", "yes", "y", "on":
		v = true
	case "false", "f", "0", "no", "n", "off":
	default:
		return "", false
	}
	pick := func(yes, no string) (string, bool) {
		if v {
			return yes, true
		}
		return no, true
	}
	switch style {
	case "yes/no":
		return pick("yes", "no")
	case "1/0":
		return pick("1", "0")
	case "check":
		return pick("✓", "✗")
	case "true/false":
		return pick("true", "false")
	}
	return "", false
}

// formatNumber applies a pattern like "#,##0.00": a comma in the whole part
// groups thousands; after the dot, each 0 is a digit always shown and each
// # a digit shown when not zero. Values are exact (big.Rat), rounded half
// away from zero.
func formatNumber(pattern, text string) (string, bool) {
	r, ok := new(big.Rat).SetString(strings.TrimSpace(text))
	if !ok {
		return "", false
	}
	whole, frac, _ := strings.Cut(pattern, ".")
	minFrac := strings.Count(frac, "0")
	maxFrac := minFrac + strings.Count(frac, "#")
	s := r.FloatString(maxFrac)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	ip, fp, _ := strings.Cut(s, ".")
	for len(fp) > minFrac && strings.HasSuffix(fp, "0") {
		fp = fp[:len(fp)-1]
	}
	if strings.Contains(whole, ",") && len(ip) > 3 {
		var b strings.Builder
		lead := len(ip) % 3
		if lead > 0 {
			b.WriteString(ip[:lead])
		}
		for i := lead; i < len(ip); i += 3 {
			if b.Len() > 0 {
				b.WriteByte(',')
			}
			b.WriteString(ip[i : i+3])
		}
		ip = b.String()
	}
	if fp != "" {
		ip += "." + fp
	}
	if neg && strings.Trim(ip, "0.,") != "" {
		ip = "-" + ip
	}
	return ip, true
}

// formatTime writes t with a date pattern: yyyy yy MMMM MMM MM M dd d
// EEEE EEE HH H hh h mm m ss s S… (fraction) a XXX XX Z z; text in
// 'single quotes' and other characters are kept as they are.
func formatTime(pattern string, t time.Time) string {
	var b strings.Builder
	rs := []rune(pattern)
	for i := 0; i < len(rs); {
		ch := rs[i]
		if ch == '\'' {
			j := i + 1
			for j < len(rs) && rs[j] != '\'' {
				j++
			}
			if j == i+1 && j < len(rs) {
				b.WriteRune('\'') // '' is one quote
			} else {
				b.WriteString(string(rs[i+1 : min(j, len(rs))]))
			}
			i = j + 1
			continue
		}
		n := 1
		for i+n < len(rs) && rs[i+n] == ch {
			n++
		}
		i += n
		pad := func(v, w int) { fmt.Fprintf(&b, "%0*d", w, v) }
		switch ch {
		case 'y':
			if n == 2 {
				pad(t.Year()%100, 2)
			} else {
				pad(t.Year(), n)
			}
		case 'M':
			switch {
			case n >= 4:
				b.WriteString(t.Month().String())
			case n == 3:
				b.WriteString(t.Month().String()[:3])
			default:
				pad(int(t.Month()), n)
			}
		case 'd':
			pad(t.Day(), n)
		case 'E':
			if n >= 4 {
				b.WriteString(t.Weekday().String())
			} else {
				b.WriteString(t.Weekday().String()[:3])
			}
		case 'H':
			pad(t.Hour(), n)
		case 'h':
			h := t.Hour() % 12
			if h == 0 {
				h = 12
			}
			pad(h, n)
		case 'm':
			pad(t.Minute(), n)
		case 's':
			pad(t.Second(), n)
		case 'S':
			f := fmt.Sprintf("%09d", t.Nanosecond())
			if n > 9 {
				f += strings.Repeat("0", n-9)
			}
			b.WriteString(f[:n])
		case 'a':
			if t.Hour() < 12 {
				b.WriteString("AM")
			} else {
				b.WriteString("PM")
			}
		case 'X':
			switch n {
			case 1:
				b.WriteString(t.Format("Z07"))
			case 2:
				b.WriteString(t.Format("Z0700"))
			default:
				b.WriteString(t.Format("Z07:00"))
			}
		case 'Z':
			b.WriteString(t.Format("-0700"))
		case 'z':
			b.WriteString(t.Format("MST"))
		default:
			b.WriteString(strings.Repeat(string(ch), n))
		}
	}
	return b.String()
}

// geometryWKT reads a geometry value: "0x…" binary or plain hex text
// (PostGIS), as WKB, EWKB (with SRID) or MySQL's 4-byte SRID + WKB.
func geometryWKT(text string) (string, bool) {
	h := strings.TrimPrefix(text, "0x")
	if len(h) < 10 || len(h) > 64<<20 {
		return "", false
	}
	b, err := hex.DecodeString(h)
	if err != nil {
		return "", false
	}
	if s, err := WKBToWKT(b); err == nil {
		return s, true
	}
	if len(b) > 4 {
		srid := binary.LittleEndian.Uint32(b)
		if s, err := WKBToWKT(b[4:]); err == nil && !strings.HasPrefix(s, "SRID=") {
			if srid != 0 {
				s = "SRID=" + strconv.FormatUint(uint64(srid), 10) + ";" + s
			}
			return s, true
		}
	}
	return "", false
}

var errWKB = errors.New("not WKB")

// WKBToWKT turns a whole WKB or EWKB value into WKT (EWKT with a SRID).
// Bytes left over, unknown types or counts past the data are errors.
func WKBToWKT(b []byte) (string, error) {
	r := wkbReader{b: b}
	var out strings.Builder
	if err := r.geometry(&out, 0, true); err != nil {
		return "", err
	}
	if r.at != len(r.b) {
		return "", errWKB
	}
	return out.String(), nil
}

type wkbReader struct {
	b  []byte
	at int
}

func (r *wkbReader) u32(order binary.ByteOrder) (uint32, error) {
	if len(r.b)-r.at < 4 {
		return 0, errWKB
	}
	v := order.Uint32(r.b[r.at:])
	r.at += 4
	return v, nil
}

func (r *wkbReader) f64(order binary.ByteOrder) (float64, error) {
	if len(r.b)-r.at < 8 {
		return 0, errWKB
	}
	v := math.Float64frombits(order.Uint64(r.b[r.at:]))
	r.at += 8
	return v, nil
}

var wkbNames = [...]string{1: "POINT", 2: "LINESTRING", 3: "POLYGON", 4: "MULTIPOINT", 5: "MULTILINESTRING", 6: "MULTIPOLYGON", 7: "GEOMETRYCOLLECTION"}

func (r *wkbReader) geometry(out *strings.Builder, depth int, top bool) error {
	if depth > 32 || r.at >= len(r.b) {
		return errWKB
	}
	var order binary.ByteOrder
	switch r.b[r.at] {
	case 0:
		order = binary.BigEndian
	case 1:
		order = binary.LittleEndian
	default:
		return errWKB
	}
	r.at++
	typ, err := r.u32(order)
	if err != nil {
		return err
	}
	z, m := typ&0x80000000 != 0, typ&0x40000000 != 0
	if typ&0x20000000 != 0 {
		srid, err := r.u32(order)
		if err != nil || !top {
			return errWKB
		}
		fmt.Fprintf(out, "SRID=%d;", srid)
	}
	typ &= 0x0fffffff
	switch typ / 1000 {
	case 1:
		z = true
	case 2:
		m = true
	case 3:
		z, m = true, true
	case 0:
	default:
		return errWKB
	}
	typ %= 1000
	if typ < 1 || int(typ) >= len(wkbNames) {
		return errWKB
	}
	dims := 2
	out.WriteString(wkbNames[typ])
	switch {
	case z && m:
		out.WriteString(" ZM")
		dims = 4
	case z:
		out.WriteString(" Z")
		dims = 3
	case m:
		out.WriteString(" M")
		dims = 3
	}
	if typ == 1 {
		return r.point(out, order, dims, true)
	}
	n, err := r.u32(order)
	if err != nil {
		return err
	}
	if n == 0 {
		out.WriteString(" EMPTY")
		return nil
	}
	// every item takes at least 4 bytes: a count past the data is broken
	if uint64(n)*4 > uint64(len(r.b)-r.at) {
		return errWKB
	}
	out.WriteByte('(')
	for i := uint32(0); i < n; i++ {
		if i > 0 {
			out.WriteByte(',')
		}
		var err error
		switch typ {
		case 2:
			err = r.point(out, order, dims, false)
		case 3:
			err = r.ring(out, order, dims)
		default:
			// MULTI*: each item is a full geometry; WKT drops its type name
			var sub strings.Builder
			if err = r.geometry(&sub, depth+1, false); err == nil {
				s := sub.String()
				if typ != 7 {
					if k := strings.IndexAny(s, "(E"); k >= 0 {
						s = strings.TrimSpace(s[k:])
					}
					if typ == 4 && s != "EMPTY" {
						s = "(" + strings.TrimSuffix(strings.TrimPrefix(s, "("), ")") + ")"
					}
				}
				out.WriteString(s)
			}
		}
		if err != nil {
			return err
		}
	}
	out.WriteByte(')')
	return nil
}

func (r *wkbReader) ring(out *strings.Builder, order binary.ByteOrder, dims int) error {
	n, err := r.u32(order)
	if err != nil {
		return err
	}
	if uint64(n)*uint64(dims)*8 > uint64(len(r.b)-r.at) {
		return errWKB
	}
	out.WriteByte('(')
	for i := uint32(0); i < n; i++ {
		if i > 0 {
			out.WriteByte(',')
		}
		if err := r.point(out, order, dims, false); err != nil {
			return err
		}
	}
	out.WriteByte(')')
	return nil
}

// point writes the coordinates; wrap adds the parentheses of a lone POINT
// (an empty point is all NaN).
func (r *wkbReader) point(out *strings.Builder, order binary.ByteOrder, dims int, wrap bool) error {
	vals := make([]string, dims)
	empty := true
	for i := range vals {
		f, err := r.f64(order)
		if err != nil {
			return err
		}
		if !math.IsNaN(f) {
			empty = false
		}
		vals[i] = strconv.FormatFloat(f, 'f', -1, 64)
	}
	if wrap && empty {
		out.WriteString(" EMPTY")
		return nil
	}
	if wrap {
		out.WriteByte('(')
	}
	out.WriteString(strings.Join(vals, " "))
	if wrap {
		out.WriteByte(')')
	}
	return nil
}
