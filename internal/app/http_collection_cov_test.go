package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Import Collection and Export as HAR from their prompts: a new file
// next to the source (never over one), errors say why.
func TestCovHTTPCollectionPrompts(t *testing.T) {
	m, root, _ := covHTTP(t)
	coll := `{"info":{"name":"Shop","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"list","request":{"method":"GET","url":"http://shop.local/items"}}]}`
	if err := os.WriteFile(filepath.Join(root, "shop.json"), []byte(coll), 0o644); err != nil {
		t.Fatal(err)
	}
	m.promptKind = promptKindHTTPImportCollection
	if c, ok := m.handleHTTPPromptSubmit(""); !ok || c != nil {
		t.Fatal("empty name did something")
	}
	m.handleHTTPPromptSubmit("shop.json")
	if !strings.Contains(covHTTPToast(m), "Imported 1 requests into") {
		t.Fatalf("toast %q", covHTTPToast(m))
	}
	m.handleHTTPPromptSubmit(filepath.Join(root, "shop.json"))
	matches, _ := filepath.Glob(filepath.Join(root, "*.http"))
	if len(matches) != 3 { // api.http and two imports
		t.Errorf("files %v", matches)
	}
	m.handleHTTPPromptSubmit("missing.json")
	if !strings.Contains(covHTTPToast(m), "Import Collection:") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	if err := os.WriteFile(filepath.Join(root, "junk.json"), []byte("[1,2"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.handleHTTPPromptSubmit("junk.json")
	if !strings.Contains(covHTTPToast(m), "Import Collection:") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	// Export: the prompt needs an open .http file.
	file := filepath.Join(root, "api.http")
	covHTTPEdit(t, m, file, 1)
	covHTTPDispatch(t, m, "http-export-har")
	if !m.promptOpen || m.promptKind != promptKindHTTPExportHAR || !strings.HasSuffix(m.prompt.Value(), ".har") {
		t.Fatalf("export prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.handleHTTPPromptSubmit("out.har")
	if b, err := os.ReadFile(filepath.Join(root, "out.har")); err != nil || !strings.Contains(string(b), `"log"`) {
		t.Fatalf("har: %v %s", err, b)
	}
	if !strings.Contains(covHTTPToast(m), "Exported 2 requests to out.har") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.handleHTTPPromptSubmit("out.har")
	if !strings.Contains(covHTTPToast(m), "already exists") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	m.handleHTTPPromptSubmit(filepath.Join(root, "nodir", "x.har"))
	if !strings.Contains(covHTTPToast(m), "Export as HAR:") {
		t.Errorf("toast %q", covHTTPToast(m))
	}
	if c, _ := m.handleHTTPPromptSubmit(""); c != nil {
		t.Error("empty export name did something")
	}
	// A file with no requests, a file that cannot be read.
	empty := filepath.Join(root, "empty.http")
	if err := os.WriteFile(empty, []byte("# nothing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := httpExportHARFile(empty, filepath.Join(root, "e.har")); err == nil || !strings.Contains(err.Error(), "no requests") {
		t.Errorf("empty: %v", err)
	}
	if _, err := httpExportHARFile(filepath.Join(root, "gone.http"), filepath.Join(root, "g.har")); err == nil {
		t.Error("a missing file exported")
	}
	// httpWriteNew: a folder that is not there.
	if _, err := httpWriteNew(filepath.Join(root, "nodir"), "a.http", nil); err == nil {
		t.Error("wrote into a missing folder")
	}
	// The prompt kinds are not ours otherwise.
	m.promptKind = promptKindHTTPQuick
	if _, ok := m.httpCollectionPrompt("x"); ok {
		t.Error("another prompt taken")
	}
}
