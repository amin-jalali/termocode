package wire

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

const sampleWF = `{
  // a small sample
  "version": 1, "name": "%s", "title": "Docs report",
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/**/*.md" },
    { "id": "rep", "type": "write", "to": "file", "path": "reports/docs.md", "content": "{{len(steps.docs.items)}} docs" }
  ]
}`

func writeWF(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".jsonc"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Spec (plan §3.1, §6.1): workflows load from .termocode/workflows in the
// repo and from the private folder; the project file wins on the same
// name; a repo file is untrusted until the user trusts that hash; a
// changed file asks again; a broken file is listed with its problems.
func TestLoadWorkflowsAndTrust(t *testing.T) {
	ws, priv, root := t.TempDir(), t.TempDir(), t.TempDir()
	proj := filepath.Join(ws, ".termocode", "workflows")
	writeWF(t, proj, "docs", sprintf(sampleWF, "docs"))
	writeWF(t, proj, "broken", `{ "version": 1, "name": "broken", "steps": [ { "type": "nope" } ] }`)
	writeWF(t, priv, "docs", sprintf(sampleWF, "docs"))
	writeWF(t, priv, "mine", sprintf(sampleWF, "mine"))

	list := Load(ws, priv, root)
	if len(list) != 3 {
		t.Fatalf("want broken, docs, mine; got %+v", list)
	}
	if list[0].Name != "broken" || list[0].File != nil || len(list[0].Problems) == 0 || list[0].Problems[0].Line == 0 {
		t.Fatalf("broken: %+v", list[0])
	}
	docs := list[1]
	if docs.Name != "docs" || docs.Private || docs.Trusted || docs.File == nil {
		t.Fatalf("project docs untrusted: %+v", docs)
	}
	if mine := list[2]; !mine.Private || !mine.Trusted {
		t.Fatalf("a private file is the user's own: %+v", mine)
	}
	if err := Trust(root, docs); err != nil {
		t.Fatal(err)
	}
	if d := Load(ws, priv, root)[1]; !d.Trusted {
		t.Fatal("trusted after Trust")
	}
	writeWF(t, proj, "docs", sprintf(sampleWF, "docs")+"\n// changed")
	if d := Load(ws, priv, root)[1]; d.Trusted {
		t.Fatal("a changed file asks again")
	}
	if e, ok := Find(Load(ws, priv, root), "mine"); !ok || e.Name != "mine" {
		t.Fatal("Find")
	}
}

func sprintf(f, name string) string { return fmt.Sprintf(f, name) }
