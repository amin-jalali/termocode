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
