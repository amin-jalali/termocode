package dbclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"
)

func writeCSV(t *testing.T, body []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f.csv")
	if err := os.WriteFile(p, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCSVOptionsValidEveryRule(t *testing.T) {
	for name, c := range map[string]struct {
		o     CSVOptions
		delim rune
		want  string
	}{
		"ok":               {CSVOptions{}, ',', ""},
		"skip negative":    {CSVOptions{SkipLines: -1}, ',', "negative"},
		"quote invisible":  {CSVOptions{Quote: ' '}, ',', "quote must be one visible"},
		"escape invisible": {CSVOptions{Escape: '\n'}, ',', "escape must be one visible"},
		"quote is delim":   {CSVOptions{Quote: ';'}, ';', "differ from the delimiter"},
		"escape is delim":  {CSVOptions{Escape: '|'}, '|', "differ from the delimiter"},
		"escape is quote":  {CSVOptions{Escape: '"'}, ',', "same as the quote"},
		"no quote":         {CSVOptions{Quote: QuoteNone}, ',', ""},
		"single quote":     {CSVOptions{Quote: '\''}, ',', ""},
	} {
		err := c.o.Valid(c.delim)
		if (c.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), c.want)) {
			t.Errorf("%s = %v, want %q", name, err, c.want)
		}
	}
}

func TestSniffCSVFilesOptionsAndEncodings(t *testing.T) {
	if _, err := SniffCSV(filepath.Join(t.TempDir(), "none.csv")); err == nil || !strings.Contains(err.Error(), "cannot open") {
		t.Errorf("missing file = %v", err)
	}
	if _, err := SniffCSV(t.TempDir()); err == nil {
		t.Error("a folder: want error")
	}
	if _, err := SniffCSV(writeCSV(t, []byte("  \n"))); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("blank file = %v", err)
	}
	if _, err := SniffCSVWith(writeCSV(t, []byte("a,b\n1,2\n")), CSVOptions{SkipLines: 5}); err == nil || !strings.Contains(err.Error(), "nothing is left") {
		t.Errorf("skipping everything = %v", err)
	}
	if _, err := SniffCSVWith(writeCSV(t, []byte("a,b\n")), CSVOptions{SkipLines: -1}); err == nil {
		t.Error("negative skip: want error")
	}
	sn, err := SniffCSVWith(writeCSV(t, []byte("junk line\nname;note\nann;'a;b'\nbo;it's\n")), CSVOptions{SkipLines: 1, Quote: '\''})
	if err != nil || sn.Delimiter != ';' || sn.Fields != 2 || sn.Columns[0] != "name" || sn.Sample[0][1] != "a;b" {
		t.Errorf("skip and quote = %+v, %v", sn, err)
	}
	sn, err = SniffCSVWith(writeCSV(t, []byte("a,b\n\"x \\\" y\",2\n")), CSVOptions{Escape: '\\'})
	if err != nil || sn.Sample[0][0] != `x " y` {
		t.Errorf("escape = %+v, %v", sn, err)
	}
	sn, err = SniffCSVWith(writeCSV(t, []byte("a,b\nx\"y,2\n")), CSVOptions{Quote: QuoteNone})
	if err != nil || sn.Sample[0][0] != `x"y` {
		t.Errorf("no quote = %+v, %v", sn, err)
	}
	// UTF-16 with a surrogate pair, and a UTF-8 BOM.
	u := utf16.Encode([]rune("name,emoji\nann,😀\n"))
	b := []byte{0xFF, 0xFE}
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	if sn, err := SniffCSV(writeCSV(t, b)); err != nil || sn.Encoding != "UTF-16LE" || sn.Sample[0][1] != "😀" {
		t.Errorf("utf-16 = %+v, %v", sn, err)
	}
	bom := append([]byte{0xEF, 0xBB, 0xBF}, "a,b\n1,2\n"...)
	if sn, err := SniffCSV(writeCSV(t, bom)); err != nil || sn.Columns[0] != "a" {
		t.Errorf("bom = %+v, %v", sn, err)
	}
	// A big file is cut at the sniff size without breaking a character.
	big := "a,b\n" + strings.Repeat("é,ü\n", 100000)
	if sn, err := SniffCSV(writeCSV(t, []byte(big))); err != nil || sn.Fields != 2 {
		t.Errorf("big file = %+v, %v", sn, err)
	}
}

func TestColumnFormatsApplyAndDescribe(t *testing.T) {
	cases := []struct {
		f    ColumnFormat
		in   string
		want any
		bad  string
	}{
		{ColumnFormat{Trim: true}, "  x ", "x", ""},
		{ColumnFormat{NullText: `\N`}, `\N`, nil, ""},
		{ColumnFormat{}, "", nil, ""},
		{ColumnFormat{DecimalComma: true}, "1.234,5", "1234.5", ""},
		{ColumnFormat{DecimalComma: true}, "abc", nil, "decimal comma"},
		{ColumnFormat{DateLayout: "DD/MM/YYYY"}, "09/03/2024", "2024-03-09", ""},
		{ColumnFormat{DateLayout: "DD/MM/YYYY HH:mm:ss"}, "09/03/2024 15:04:05", "2024-03-09 15:04:05", ""},
		{ColumnFormat{DateLayout: "DD/MM/YYYY"}, "2024-03-09", nil, "does not match"},
		{ColumnFormat{DateLayout: "DD/MM"}, "09/03", nil, "needs a year"},
	}
	for _, c := range cases {
		got, err := c.f.Apply(c.in)
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("%+v %q = %v, want %q", c.f, c.in, err, c.bad)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%+v %q = %v, %v, want %v", c.f, c.in, got, err, c.want)
		}
	}
	for layout, why := range map[string]string{"": "type a date format", "DD/MM/2024": "not digits", "DD/YYYY": "needs a year"} {
		if err := CheckDateLayout(layout); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("CheckDateLayout(%q) = %v, want %q", layout, err, why)
		}
	}
	if CheckDateLayout("YY-MM-DD HH:mm") != nil {
		t.Error("a short year layout with a time is fine")
	}
	f := ColumnFormat{Trim: true, NullText: "NULL", DateLayout: "DD/MM/YYYY", DecimalComma: true}
	if d := f.Describe(); !strings.Contains(d, "trim") || !strings.Contains(d, `NULL = "NULL"`) || !strings.Contains(d, "date DD/MM/YYYY") || !strings.Contains(d, "decimal comma") {
		t.Errorf("Describe = %q", d)
	}
	if !(ColumnFormat{}).IsZero() || f.IsZero() || (ColumnFormat{}).Describe() != "" {
		t.Error("IsZero and Describe of nothing")
	}
}
