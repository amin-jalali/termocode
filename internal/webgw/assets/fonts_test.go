package assets

import (
	"encoding/json"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Size budget of the terminal's symbol fonts (WEB-58): the browser fetches
// one face only when a row shows a glyph of its range, so the budget is
// per face for the phone's link and in total for the binary.
const (
	budgetFace  = 192 << 10
	budgetFonts = 448 << 10
)

type fontFace struct {
	File         string `json:"file"`
	UnicodeRange string `json:"unicode_range"`
	License      string `json:"license"`
}

func readFontsRecord(t *testing.T) []fontFace {
	t.Helper()
	b, err := os.ReadFile("fonts/fonts.json")
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Family string     `json:"family"`
		Faces  []fontFace `json:"faces"`
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Family != "termocode symbols" || len(rec.Faces) == 0 {
		t.Fatalf("fonts.json: family %q, %d faces", rec.Family, len(rec.Faces))
	}
	return rec.Faces
}

// Each face of fonts.json is one @font-face of src/styles/fonts.css with
// the same file and range, is a woff2 within the budget, is embedded,
// and names its license; LICENSE.txt lists every file.
func TestSymbolFontFacesMatchTheRecord(t *testing.T) {
	faces := readFontsRecord(t)
	css, err := os.ReadFile("src/styles/fonts.css")
	if err != nil {
		t.Fatal(err)
	}
	lic, err := os.ReadFile("fonts/LICENSE.txt")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`@font-face \{ font-family: "termocode symbols"; src: url\("/assets/fonts/([^"]+)"\) format\("woff2"\); unicode-range: ([^;]+); font-display: swap; \}`)
	inCSS := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(string(css), -1) {
		inCSS[m[1]] = m[2]
	}
	if n := strings.Count(string(css), "@font-face"); n != len(inCSS) || n != len(faces) {
		t.Errorf("fonts.css has %d faces (%d in the usual form), fonts.json %d", n, len(inCSS), len(faces))
	}
	total := 0
	for _, f := range faces {
		if inCSS[f.File] != f.UnicodeRange {
			t.Errorf("%s: fonts.css range %q, fonts.json %q", f.File, inCSS[f.File], f.UnicodeRange)
		}
		b, err := fs.ReadFile(FS(), "fonts/"+f.File)
		if err != nil {
			t.Errorf("%s is not embedded: %v", f.File, err)
			continue
		}
		if !strings.HasPrefix(string(b), "wOF2") {
			t.Errorf("%s is not a woff2 file", f.File)
		}
		if len(b) > budgetFace {
			t.Errorf("%s is %d B, budget %d B", f.File, len(b), budgetFace)
		}
		total += len(b)
		if f.License == "" || !strings.Contains(string(lic), f.File) {
			t.Errorf("%s: no license named, or not listed in fonts/LICENSE.txt", f.File)
		}
	}
	t.Logf("symbol fonts: %d faces, %d B", len(faces), total)
	if total > budgetFonts {
		t.Errorf("symbol fonts are %d B together, budget %d B", total, budgetFonts)
	}
	b, _ := fs.ReadFile(FS(), "licenses.txt")
	if !strings.Contains(string(b), "termocode symbols") || !strings.Contains(string(b), "SIL OPEN FONT LICENSE Version 1.1") {
		t.Error("licenses.txt lacks the symbol fonts' licenses")
	}
}
