package assets

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"testing"
)

// Bundle budgets of the page (web design 4.1), gzipped: a phone on a weak
// link loads the whole UI in one short burst.
const (
	budgetJS    = 120 << 10
	budgetCSS   = 20 << 10
	budgetHTML  = 4 << 10
	budgetTotal = 150 << 10
)

func gzSize(t *testing.T, name string) int {
	t.Helper()
	b, err := fs.ReadFile(FS(), name)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = zw.Write(b)
	_ = zw.Close()
	return buf.Len()
}

func TestPerfGuardBundleSize(t *testing.T) {
	js, css, html := gzSize(t, "app.js"), gzSize(t, "app.css"), gzSize(t, "index.html")
	t.Logf("gzipped: app.js %d B, app.css %d B, index.html %d B, total %d B", js, css, html, js+css+html)
	for _, c := range []struct {
		name      string
		got, want int
	}{{"app.js", js, budgetJS}, {"app.css", css, budgetCSS}, {"index.html", html, budgetHTML}, {"page total", js + css + html, budgetTotal}} {
		if c.got > c.want {
			t.Errorf("%s is %d B gzipped, budget %d B", c.name, c.got, c.want)
		}
	}
}
