package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// API-15: import a HAR next to it as .http; export a .http file as HAR.
func TestHTTPCollectionImportExportFiles(t *testing.T) {
	dir := t.TempDir()
	har := filepath.Join(dir, "net.har")
	os.WriteFile(har, []byte(`{"log":{"version":"1.2","entries":[{"request":{"method":"GET","url":"https://x.io/a","headers":[]}}]}}`), 0o644)
	out, n, err := httpImportCollectionFile(har)
	if err != nil || n != 1 || filepath.Base(out) != "net.http" {
		t.Fatalf("import = %q %d %v", out, n, err)
	}
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "GET https://x.io/a") {
		t.Fatalf("file:\n%s", b)
	}
	// A second import does not overwrite the first.
	out2, _, err := httpImportCollectionFile(har)
	if err != nil || filepath.Base(out2) != "net-2.http" {
		t.Fatalf("second import = %q %v", out2, err)
	}
	dst := filepath.Join(dir, "out.har")
	if n, err := httpExportHARFile(out, dst); err != nil || n != 1 {
		t.Fatalf("export = %d %v", n, err)
	}
	if b, _ := os.ReadFile(dst); !strings.Contains(string(b), `"url": "https://x.io/a"`) && !strings.Contains(string(b), `"url":"https://x.io/a"`) {
		t.Fatalf("har:\n%s", b)
	}
	if _, err := httpExportHARFile(out, dst); err == nil {
		t.Fatal("export must not overwrite an existing file")
	}
}
