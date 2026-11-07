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
	if _, ok := out["desk.js"]; ok {
		t.Error("the desktop stylesheet's entry left a desk.js")
	}
	if !strings.Contains(string(out["index.html"]), `href="/assets/desk.css" media="(min-width: 1200px)"`) {
		t.Errorf("index.html does not link desk.css for wide screens only:\n%s", out["index.html"])
	}
	for _, name := range []string{"index.html", "app.js", "app-chunk.js", "workbench.js", "app.css", "desk.css", "licenses.txt", "icon.svg", "kit.html", "kit.js", "kit.css", manifest.LockFile} {
		if len(out[name]) == 0 {
			t.Errorf("build wrote no %s", name)
		}
	}
	html := string(out["index.html"])
	for _, f := range []string{"app.js", "app-chunk.js", "workbench.js", "app.css", "desk.css"} {
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
	// The workbench shares a module with the page, as the real one does,
	// so the split build writes app-chunk.js (index.html preloads it).
	files := map[string]string{"main.tsx": `import "./x.css"; import { shared } from "./shared"; shared.push(2);` + mainTSX, "x.css": "p{margin:0}", "desk.ts": `import "./y.css";`, "y.css": "b{margin:0}", "index.html": string(tpl),
		"shared.ts": "export const shared = [1];", "workbench.tsx": `import { shared } from "./shared"; export const n = shared.length;`}
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
	app, ed := string(out["app.js"])+string(out["app-chunk.js"]), string(out["editor.js"])
	if ed == "" {
		t.Fatal("build wrote no editor.js")
	}
	if !strings.Contains(app, `import("/assets/editor.js")`) {
		t.Error("the page does not load /assets/editor.js")
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

// The desktop workbench is its own file (workbench.js), loaded by the
// page on a wide screen only; it shares the page's modules through
// app-chunk.js, which both import, so Preact and the session have one
// copy. index.html preloads the chunk with its SRI, and the workbench
// with its SRI for wide screens only.
func TestBuildSplitsTheWorkbench(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	app, chunk, wb := string(out["app.js"]), string(out["app-chunk.js"]), string(out["workbench.js"])
	if chunk == "" || wb == "" {
		t.Fatal("build wrote no app-chunk.js or workbench.js")
	}
	for name, code := range map[string]string{"app.js": app, "workbench.js": wb} {
		if !strings.Contains(code, `from"./app-chunk.js"`) {
			t.Errorf("%s does not import the shared chunk", name)
		}
	}
	if !strings.Contains(app+chunk, `import("/assets/workbench.js")`) {
		t.Error("the page does not load /assets/workbench.js")
	}
	if strings.Contains(app+chunk, "wb-panel-tabs") {
		t.Error("the phone's files hold the workbench's layout")
	}
	if !strings.Contains(wb, "wb-panel-tabs") || strings.Contains(wb, "__H") {
		t.Error("workbench.js is not the workbench alone (it has no layout, or its own Preact)")
	}
	html := string(out["index.html"])
	for _, want := range []string{
		`<link rel="modulepreload" href="/assets/app-chunk.js" integrity="` + manifest.SRI(out["app-chunk.js"]) + `">`,
		`<link rel="modulepreload" href="/assets/workbench.js" media="(min-width: 1200px)" integrity="` + manifest.SRI(out["workbench.js"]) + `">`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("index.html has no %s:\n%s", want, html)
		}
	}
}

// Spec (MOBUX-20, mobile plan Q4): the phone's own views are their own
// file (home.js), loaded by the page with a dynamic import; they share
// Preact and the session through app-chunk.js, and app.js carries none of
// their code or texts (This phone's strings live in home.js).
func TestBuildWritesTheHomeChunk(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	page, hm := string(out["app.js"])+string(out["app-chunk.js"]), string(out["home.js"])
	if hm == "" {
		t.Fatal("build wrote no home.js")
	}
	if !strings.Contains(page, `import("/assets/home.js")`) {
		t.Error("the page does not load /assets/home.js")
	}
	const forget = "Forget this phone" // strings.phone.ts
	if strings.Contains(page, forget) || strings.Contains(page, `"phone.forget"`) {
		t.Error("the phone's first files hold This phone")
	}
	if !strings.Contains(hm, forget) || !strings.Contains(hm, `from"./app-chunk.js"`) || strings.Contains(hm, "__H") {
		t.Error("home.js is not This phone alone (no screen, or its own Preact)")
	}
	if strings.Contains(string(out["index.html"]), "/assets/home.js") {
		t.Error("index.html preloads home.js; only a phone loads it, on use")
	}
}

// The All tools menu (MOBUX-5) is in the one phone chunk (home.js; it was
// its own phone.js before the merge): the page loads it when the user
// opens it; it shares the page's modules through app-chunk.js, holds the
// menu, and the page's own files do not.
func TestBuildPutsAllToolsInTheHomeChunk(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	page, ph := string(out["app.js"])+string(out["app-chunk.js"]), string(out["home.js"])
	if ph == "" {
		t.Fatal("build wrote no home.js")
	}
	if _, ok := out["home.css"]; ok {
		t.Error("home.js brought its own style sheet; the phone's styles are in app.css")
	}
	if !strings.Contains(ph, `from"./app-chunk.js"`) || strings.Contains(ph, "__H") {
		t.Error("home.js does not share the page's modules (no app-chunk.js import, or its own Preact)")
	}
	if !strings.Contains(ph, "tools-server") || strings.Contains(page, "tools-server") {
		t.Error("the All tools menu is not in home.js alone")
	}
	if !strings.Contains(page, `import("/assets/home.js")`) {
		t.Error("the page does not load /assets/home.js")
	}
}

// The phone's live board (MOB-18) is in the one phone chunk (home.js; it
// was its own board.js before the merge), loaded by the page when the
// board opens; it shares the page's modules through app-chunk.js (one
// Preact), and the page's first files do not hold the board's view.
func TestBuildPutsTheBoardInTheHomeChunk(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	app, chunk, board := string(out["app.js"]), string(out["app-chunk.js"]), string(out["home.js"])
	if board == "" {
		t.Fatal("build wrote no home.js")
	}
	if !strings.Contains(board, `from"./app-chunk.js"`) || strings.Contains(board, "__H") {
		t.Error("home.js does not share the page's chunk (or has its own Preact)")
	}
	if !strings.Contains(app+chunk, `import("/assets/home.js")`) {
		t.Error("the page does not load /assets/home.js")
	}
	if strings.Contains(app+chunk, "board-cols") {
		t.Error("the page's first files hold the board's view")
	}
}

// The Persian strings are their own file (strings-fa.js), loaded only
// when the UI language is Persian: the phone's first files carry one
// language, English. strings-fa.js is the strings alone (no Preact).
func TestBuildWritesThePersianChunk(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	page, fa := string(out["app.js"])+string(out["app-chunk.js"]), string(out["strings-fa.js"])
	if fa == "" {
		t.Fatal("build wrote no strings-fa.js")
	}
	const persian = "باز کردن فضای کار" // connect.open
	if !strings.Contains(fa, persian) || strings.Contains(fa, "__H") {
		t.Error("strings-fa.js is not the Persian strings alone")
	}
	if strings.Contains(page, persian) {
		t.Error("the phone's first files hold the Persian strings")
	}
	if !strings.Contains(page, `import("/assets/strings-fa.js")`) {
		t.Error("the page does not load /assets/strings-fa.js")
	}
}

func TestBuildRefusesAnyOtherDynamicImport(t *testing.T) {
	dir := fixture(t, `import("/assets/other.js").then(() => 0);`)
	if _, err := Build(dir); err == nil || !strings.Contains(err.Error(), "other.js") {
		t.Fatalf("err = %v, want a refusal naming other.js", err)
	}
}

// The phone's Home (MOB-4) is in the one phone chunk (home.js): the page
// loads it with a dynamic import on a phone only and hands it its signals
// and views (src/home/api.ts), so home.js holds no Preact of its own and
// the phone's first files (app.js, app-chunk.js) do not grow with it. The
// desktop never names it. (Merge: home.js now shares app-chunk.js, like
// This phone and All tools; MOB-4 had it built alone.)
func TestBuildHomeChunkHoldsTheHome(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	page, home := string(out["app.js"])+string(out["app-chunk.js"]), string(out["home.js"])
	if home == "" {
		t.Fatal("build wrote no home.js")
	}
	if !strings.Contains(page, `import("/assets/home.js")`) {
		t.Error("the page does not load /assets/home.js")
	}
	if strings.Contains(page, "home-chips") {
		t.Error("the phone's first files hold the Home's views")
	}
	if !strings.Contains(home, "home-chips") || strings.Contains(home, "__H") || strings.Contains(home, "preact") {
		t.Error("home.js is not the Home alone (it has no chips, or its own Preact)")
	}
	for _, name := range []string{"index.html", "workbench.js"} {
		if strings.Contains(string(out[name]), "home.js") {
			t.Errorf("%s names home.js: only a phone loads it", name)
		}
	}
	if _, ok := out["home.css"]; ok {
		t.Error("home.js imports a style sheet; the phone's styles are in app.css")
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

// Vim mode of the desktop editor (WEBP-1) is its own file,
// monaco-vim.js: the editor's frame loads it with a dynamic import only
// when Vim mode is on, so a browser without Vim mode never loads it.
// It is the keymap alone (no Preact, no editor).
func TestBuildWritesTheVimChunk(t *testing.T) {
	out, err := Build(realAssets)
	if err != nil {
		t.Fatal(err)
	}
	vim, frame := string(out["monaco-vim.js"]), string(out["monaco-frame.js"])
	if vim == "" {
		t.Fatal("build wrote no monaco-vim.js")
	}
	if !strings.Contains(vim, "parseMotion") || strings.Contains(vim, "__H") {
		t.Error("monaco-vim.js is not the Vim keymap alone")
	}
	if strings.Contains(frame, "parseMotion") {
		t.Error("monaco-frame.js holds the Vim keymap itself")
	}
	if !strings.Contains(frame, `import("/assets/monaco-vim.js")`) {
		t.Error("the frame does not load /assets/monaco-vim.js")
	}
}
