package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const realVendor = "../vendor"

func TestVendorFilesMatchTheirChecksums(t *testing.T) {
	pkgs, err := VerifyVendor(realVendor)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, p := range pkgs {
		names[p.Name] = true
	}
	for _, want := range []string{"preact", "@preact/signals", "@preact/signals-core"} {
		if !names[want] {
			t.Errorf("vendor.json has no %s", want)
		}
	}
}

func TestVendorPinsExactVersions(t *testing.T) {
	pkgs, err := LoadVendor(realVendor)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pkgs {
		if strings.ContainsAny(p.Version, "^~*xX<>= ") || strings.Count(p.Version, ".") != 2 {
			t.Errorf("%s: version %q is not exact", p.Name, p.Version)
		}
		if p.Dir != strings.TrimPrefix(strings.ReplaceAll(p.Name, "/", "-"), "@")+"@"+p.Version {
			t.Errorf("%s: dir %q does not name the package and version", p.Name, p.Dir)
		}
		if !strings.HasPrefix(p.Integrity, "sha512-") || !strings.HasPrefix(p.Tarball, "https://registry.npmjs.org/") {
			t.Errorf("%s: tarball %q / integrity %q", p.Name, p.Tarball, p.Integrity)
		}
	}
}

// copyVendor copies the real vendor folder to a temp dir to tamper with.
func copyVendor(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(realVendor, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(realVendor, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func TestVendorRefusesAChangedFile(t *testing.T) {
	dir := copyVendor(t)
	f := filepath.Join(dir, "preact@10.29.8", "preact.module.js")
	b, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(b, ';'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyVendor(dir); err == nil || !strings.Contains(err.Error(), "preact.module.js") {
		t.Fatalf("err = %v, want a checksum error naming the file", err)
	}
}

func TestVendorRefusesAnUnlistedFile(t *testing.T) {
	dir := copyVendor(t)
	if err := os.WriteFile(filepath.Join(dir, "preact@10.29.8", "extra.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyVendor(dir); err == nil || !strings.Contains(err.Error(), "extra.js") {
		t.Fatalf("err = %v, want an unlisted-file error", err)
	}
}

func TestVendorNeedsALicense(t *testing.T) {
	dir := copyVendor(t)
	if err := os.Remove(filepath.Join(dir, "preact-signals@2.11.3", "LICENSE")); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyVendor(dir); err == nil {
		t.Fatal("a package without its LICENSE file passed")
	}
}

func TestSRI(t *testing.T) {
	// sha384 of the empty string, from the SRI spec examples.
	if got, want := SRI(nil), "sha384-OLBgp1GsljhM2TJ+sbHjaiH9txEUvgdDTAzHv2P24donTt6/529l+9Ua0vFImLlb"; got != want {
		t.Fatalf("SRI(\"\") = %s, want %s", got, want)
	}
}

func TestVendorRefusesAnUnlistedNestedFile(t *testing.T) {
	dir := copyVendor(t)
	nested := filepath.Join(dir, "preact@10.29.8", "src")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "extra.d.ts"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyVendor(dir); err == nil || !strings.Contains(err.Error(), "src/extra.d.ts") {
		t.Fatalf("err = %v, want an unlisted nested file error", err)
	}
}

func TestVendorShipsTypeDeclarations(t *testing.T) {
	pkgs, err := LoadVendor(realVendor)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"preact":               {"src/index.d.ts", "src/jsx.d.ts", "src/dom.d.ts", "hooks/src/index.d.ts", "jsx-runtime/src/index.d.ts"},
		"@preact/signals":      {"dist/signals.d.ts"},
		"@preact/signals-core": {"dist/signals-core.d.ts"},
		"@codemirror/state":    {"dist/index.d.ts"},
		"@codemirror/view":     {"dist/index.d.ts"},
		"style-mod":            {"src/style-mod.d.ts"},
	}
	for _, p := range pkgs {
		for _, f := range want[p.Name] {
			if _, ok := p.Files[f]; !ok {
				t.Errorf("%s: no type declaration %s", p.Name, f)
			}
		}
	}
}
