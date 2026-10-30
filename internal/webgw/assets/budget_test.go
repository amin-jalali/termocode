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
	// budgetDesk is the desktop workbench's stylesheet (desk.css), which
	// only screens 1200 px and wider wait for.
	budgetDesk = 24 << 10
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
	}{{"desk.css", gzSize(t, "desk.css"), budgetDesk}, {"app.js", js, budgetJS}, {"app.css", css, budgetCSS}, {"index.html", html, budgetHTML}, {"page total", js + css + html, budgetTotal}} {
		if c.got > c.want {
			t.Errorf("%s is %d B gzipped, budget %d B", c.name, c.got, c.want)
		}
	}
}

// budgetDesktopEditor is the desktop editor's frame (the workbench), gzipped.
// It loads only on a desktop (1200 px and wider), the first time a file
// opens there; a phone never loads it. About 1.2 MiB today: the editor's
// core, and its features (find, hover, completion, go to definition).
const budgetDesktopEditor = 1300 << 10

func TestPerfGuardDesktopEditorSize(t *testing.T) {
	total := 0
	for _, name := range []string{"monaco.html", "monaco-amd.js", "monaco-frame.js", "monaco-frame.css", "monaco/editor.js", "monaco/editor-host.js", "monaco/editor-contrib.js", "monaco/editor.css"} {
		total += gzSize(t, name)
	}
	t.Logf("desktop editor gzipped: %d B", total)
	if total > budgetDesktopEditor {
		t.Errorf("the desktop editor is %d B gzipped, budget %d B", total, budgetDesktopEditor)
	}
	// The phone's page never names it: only the workbench frames it.
	for _, name := range []string{"app.js", "index.html"} {
		b, err := fs.ReadFile(FS(), name)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(b, []byte("monaco/editor.js")) {
			t.Errorf("%s loads the desktop editor itself", name)
		}
	}
}
