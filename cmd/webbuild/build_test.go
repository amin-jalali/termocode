package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webgw/assets/manifest"
)

const realAssets = "../../internal/webgw/assets"

func TestBuildIsDeterministic(t *testing.T) {
	a, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("two builds wrote %d and %d files", len(a), len(b))
	}
	for name, body := range a {
		if !bytes.Equal(body, b[name]) {
			t.Errorf("%s differs between two builds", name)
		}
	}
}

func TestBuildWritesThePageFiles(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"index.html", "app.js", "app.css", "licenses.txt", "icon.svg", "kit.html", "kit.js", "kit.css", manifest.LockFile} {
		if len(out[name]) == 0 {
			t.Errorf("build wrote no %s", name)
		}
	}
	html := string(out["index.html"])
	for _, f := range []string{"app.js", "app.css"} {
		if !strings.Contains(html, `integrity="`+manifest.SRI(out[f])+`"`) {
			t.Errorf("index.html has no SRI for %s:\n%s", f, html)
		}
	}
	if strings.Contains(html, "{{") {
		t.Errorf("index.html keeps a template mark:\n%s", html)
	}
	lic := string(out["licenses.txt"])
	for _, p := range []string{"preact 10.29.8", "@preact/signals 2.11.3", "@preact/signals-core 1.14.4", "MIT License"} {
		if !strings.Contains(lic, p) {
			t.Errorf("licenses.txt does not name %q", p)
		}
	}
}

func TestLockListsEveryFileWithItsHash(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := manifest.ParseLock(string(out[manifest.LockFile]))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(out)-1 {
		t.Fatalf("lock lists %d files, build wrote %d besides the lock", len(entries), len(out)-1)
	}
	for _, e := range entries {
		body, ok := out[e.Path]
		if !ok {
			t.Errorf("lock lists %s, not built", e.Path)
			continue
		}
		if e.SRI != manifest.SRI(body) || e.Size != len(body) {
			t.Errorf("lock row for %s does not match its bytes", e.Path)
		}
	}
}

func TestCommittedAssetsAreFresh(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range out {
		disk, err := os.ReadFile(filepath.Join(realAssets, name))
		if err != nil || !bytes.Equal(disk, body) {
			t.Errorf("%s is stale; run go generate ./internal/webgw/assets", name)
		}
	}
}

// fixture makes an assets folder with the real vendor files and the given
// entry source, for builds that must fail.
func fixture(t *testing.T, mainTSX string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(filepath.Join(realAssets, "vendor"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(abs, filepath.Join(dir, "vendor")); err != nil {
		t.Fatal(err)
	}
	tpl, err := os.ReadFile(filepath.Join(realAssets, "src", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{"main.tsx": `import "./x.css";` + mainTSX, "x.css": "p{margin:0}", "index.html": string(tpl)}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(src, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestBuildRefusesAnImportThatIsNotVendored(t *testing.T) {
	dir := fixture(t, `import pad from "left-pad"; pad();`)
	_, err := Build(dir)
	if err == nil || !strings.Contains(err.Error(), "left-pad") {
		t.Fatalf("err = %v, want a refusal naming left-pad", err)
	}
}

func TestBuildRefusesAURLImport(t *testing.T) {
	dir := fixture(t, `import x from "https://example.com/x.js"; x();`)
	if _, err := Build(dir); err == nil {
		t.Fatal("a URL import built")
	}
}

func TestBuildAcceptsVendoredImports(t *testing.T) {
	dir := fixture(t, `import { h, render } from "preact"; import { signal } from "@preact/signals"; render(h("p", null, String(signal(1).value)), document.body);`)
	out, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(out["app.js"]) == 0 {
		t.Fatal("no app.js")
	}
}

// writeStatic puts one file into the fixture's src/static.
func writeStatic(t *testing.T, dir, name, body string) {
	t.Helper()
	st := filepath.Join(dir, "src", "static")
	if err := os.MkdirAll(st, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The web app manifest (WEB-35) is a static file: it is copied as it is,
// but only as valid JSON that names no other site.
func TestBuildCopiesAValidManifest(t *testing.T) {
	dir := fixture(t, ``)
	writeStatic(t, dir, "manifest.webmanifest", `{"name":"termocode","start_url":"/","icons":[{"src":"/assets/icon.svg"}]}`)
	out, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out["manifest.webmanifest"]), `"start_url":"/"`) {
		t.Fatalf("manifest not copied: %q", out["manifest.webmanifest"])
	}
	for _, bad := range []string{`{"name":`, `{"start_url":"https://evil.example/"}`, `{"icons":[{"src":"//evil.example/i.svg"}]}`} {
		dir := fixture(t, ``)
		writeStatic(t, dir, "manifest.webmanifest", bad)
		if _, err := Build(dir); err == nil {
			t.Errorf("manifest %s built", bad)
		}
	}
}

// The symbol fonts (WEB-58) are copied after their hashes check out, and
// their license joins licenses.txt; a changed file stops the build.
func TestBuildChecksAndCopiesTheSymbolFonts(t *testing.T) {
	dir := fixture(t, ``)
	fonts := filepath.Join(dir, "fonts")
	if err := os.MkdirAll(fonts, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte("wOF2 not really a font")
	write := func(name string, b []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(fonts, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("nf-x.woff2", body)
	write("LICENSE.txt", []byte("glyphs under MIT\n"))
	write("fonts.json", []byte(`{"family":"termocode symbols","faces":[{"file":"nf-x.woff2","unicode_range":"U+E0A0-E0A3","sha384":"`+manifest.SRI(body)+`"}]}`))
	out, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(out["fonts/nf-x.woff2"]) != string(body) {
		t.Error("the font is not in the output")
	}
	if !strings.Contains(string(out["licenses.txt"]), "glyphs under MIT") {
		t.Error("licenses.txt lacks the fonts' license")
	}
	write("nf-x.woff2", []byte("wOF2 changed"))
	if _, err := Build(dir); err == nil || !strings.Contains(err.Error(), "nf-x.woff2") {
		t.Errorf("a changed font built: %v", err)
	}
}

// A style sheet may name the page's own served files by their /assets/
// URL (the symbol fonts); the URL stays as it is. Any other URL in a
// style sheet still fails.
func TestBuildKeepsOwnAssetURLsInCSS(t *testing.T) {
	dir := fixture(t, ``)
	css := `@font-face { font-family: "s"; src: url("/assets/fonts/nf-x.woff2") format("woff2"); }`
	if err := os.WriteFile(filepath.Join(dir, "src", "x.css"), []byte(css), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Build(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out["app.css"]), `url(/assets/fonts/nf-x.woff2)`) {
		t.Errorf("app.css lost the URL: %s", out["app.css"])
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "x.css"), []byte(`p { background: url("/elsewhere/x.png"); }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(dir); err == nil {
		t.Error("a URL outside /assets/ built")
	}
}

// Spec (WEB-39): the in-page editor is its own file, editor.js, loaded
// only when the user edits: app.js names it in its one dynamic import and
// carries none of the editor's code; editor.js carries no Preact.
func TestBuildWritesTheEditorChunk(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	app, ed := string(out["app.js"]), string(out["editor.js"])
	if ed == "" {
		t.Fatal("build wrote no editor.js")
	}
	if !strings.Contains(app, `import("/assets/editor.js")`) {
		t.Error("app.js does not load /assets/editor.js")
	}
	if strings.Contains(app, "cm-content") {
		t.Error("app.js holds editor code")
	}
	if !strings.Contains(ed, "cm-content") || strings.Contains(ed, "__H") || strings.Contains(ed, "preact") {
		t.Error("editor.js is not the editor alone")
	}
	if !strings.Contains(ed, "@codemirror/view 6.43.14") {
		t.Error("editor.js does not name what it bundles")
	}
}

func TestBuildRefusesAnyOtherDynamicImport(t *testing.T) {
	dir := fixture(t, `import("/assets/other.js").then(() => 0);`)
	if _, err := Build(dir); err == nil || !strings.Contains(err.Error(), "other.js") {
		t.Fatalf("err = %v, want a refusal naming other.js", err)
	}
}

// The desktop editor: the vendored files a package serves are copied as
// they are, its frame page gets the SRI values of files in folders too,
// and licenses.txt carries the package's third-party notices.
func TestBuildServesVendoredFilesAndTheFrame(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile(filepath.Join(realAssets, "vendor", "monaco-editor@0.57.0", "min", "vs", "editor-BdtEMBbM.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out["monaco/editor.js"], want) {
		t.Error("monaco/editor.js is not the vendored file byte for byte")
	}
	html := string(out["monaco.html"])
	if !strings.Contains(html, `src="/assets/monaco/editor.js" integrity="`+manifest.SRI(want)+`"`) {
		t.Errorf("the frame does not pin the editor's bytes:\n%s", html)
	}
	if !strings.Contains(string(out["licenses.txt"]), "third-party notices of monaco-editor") {
		t.Error("licenses.txt has no third-party notices of the desktop editor")
	}
}
