package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// miniVendor writes a one-package vendor folder (pkg/LICENSE, pkg/a.js)
// whose manifest edit may change before it is saved.
func miniVendor(t *testing.T, edit func(p *Package, doc *vendorDoc)) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{"LICENSE": "MIT", "a.js": "export {}"}
	p := Package{Name: "a", Version: "1.0.0", Dir: "pkg", Imports: map[string]string{"a": "a.js"}, Files: map[string]string{}}
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, "pkg", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		p.Files[name] = SRI([]byte(body))
	}
	doc := vendorDoc{Packages: []Package{p}}
	if edit != nil {
		edit(&doc.Packages[0], &doc)
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, VendorFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestVendorRefusesBadManifests(t *testing.T) {
	if _, err := VerifyVendor(miniVendor(t, nil)); err != nil {
		t.Fatalf("the clean folder: %v", err)
	}
	for _, tc := range []struct {
		name, want string
		edit       func(p *Package, doc *vendorDoc)
	}{
		{"no packages", "no packages", func(_ *Package, doc *vendorDoc) { doc.Packages = nil }},
		{"no name", "bad package row", func(p *Package, _ *vendorDoc) { p.Name = "" }},
		{"dir with a slash", "bad package row", func(p *Package, _ *vendorDoc) { p.Dir = "x/pkg" }},
		{"LICENSE not listed", "has no LICENSE", func(p *Package, _ *vendorDoc) { delete(p.Files, "LICENSE") }},
		{"import of an unlisted file", "unlisted file", func(p *Package, _ *vendorDoc) { p.Imports["b"] = "b.js" }},
		{"serve of an unlisted file", "unlisted file", func(p *Package, _ *vendorDoc) { p.Serve = map[string]string{"x/a.js": "b.js"} }},
		{"serve outside a folder", "bad or repeated name", func(p *Package, _ *vendorDoc) { p.Serve = map[string]string{"../a.js": "a.js"} }},
		{"import listed twice", "listed twice", func(p *Package, doc *vendorDoc) {
			q := *p
			q.Name = "b"
			doc.Packages = append(doc.Packages, q)
		}},
	} {
		_, err := VerifyVendor(miniVendor(t, tc.edit))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestVendorRefusesAnUnlistedFolder(t *testing.T) {
	dir := miniVendor(t, nil)
	if err := os.Mkdir(filepath.Join(dir, "stray"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyVendor(dir); err == nil || !strings.Contains(err.Error(), "stray is not listed") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadVendorRefuses(t *testing.T) {
	if _, err := LoadVendor(t.TempDir()); err == nil {
		t.Fatal("missing vendor.json: no error")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, VendorFile), []byte(`{"packages":[],"extra":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyVendor(dir); err == nil || !strings.Contains(err.Error(), VendorFile) {
		t.Fatalf("unknown field: err = %v", err)
	}
}
