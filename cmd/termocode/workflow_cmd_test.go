package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

const cliWF = `{ "version": 1, "name": "docs", "title": "Docs report",
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/*.md" },
    { "id": "rep", "type": "write", "to": "file", "path": "reports/docs.md", "content": "{{len(steps.docs.items)}} docs" }
  ] }`

func cliWorkspace(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ws := t.TempDir()
	for p, body := range map[string]string{
		".termocode/workflows/docs.jsonc": cliWF,
		".termocode/workflows/bad.jsonc":  `{ "version": 1, "name": "bad", "steps": [ { "type": "nope" } ] }`,
		"docs/a.md":                       "# a",
	} {
		full := filepath.Join(ws, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return ws
}

func cliRun(ws string, args ...string) (int, string, string) {
	var out, errw bytes.Buffer
	code := runWorkflowIn(args, ws, &out, &errw)
	return code, out.String(), errw.String()
}

// Spec (WFX-8, plan §5.9): list, show and validate work without a
// session; exit codes 0 ok, 1 failed, 2 usage, 3 no session.
func TestWorkflowCLIListShowValidate(t *testing.T) {
	ws := cliWorkspace(t)
	code, out, _ := cliRun(ws, "list")
	if code != 0 || !strings.Contains(out, "docs") || !strings.Contains(out, "not trusted") || !strings.Contains(out, "file error") {
		t.Fatalf("list %d:\n%s", code, out)
	}
	code, out, _ = cliRun(ws, "list", "--json")
	var rows []map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 2 {
		t.Fatalf("list --json %d: %s", code, out)
	}
	if code, out, _ = cliRun(ws, "show", "docs"); code != 0 || !strings.Contains(out, "rep") || !strings.Contains(out, "file reports/docs.md") {
		t.Fatalf("show %d:\n%s", code, out)
	}
	if code, _, _ = cliRun(ws, "validate"); code != 1 {
		t.Fatalf("validate with a bad file: %d", code)
	}
	if code, out, _ = cliRun(ws, "validate", filepath.Join(ws, ".termocode/workflows/docs.jsonc")); code != 0 || !strings.Contains(out, "ok") {
		t.Fatalf("validate one good file %d: %s", code, out)
	}
	if code, _, errs := cliRun(ws, "validate", filepath.Join(ws, ".termocode/workflows/bad.jsonc")); code != 1 || !strings.Contains(errs, "line 1") {
		t.Fatalf("validate bad %d: %s", code, errs)
	}
	if code, _, _ = cliRun(ws, "nope"); code != 2 {
		t.Fatalf("usage: %d", code)
	}
	if code, _, _ = cliRun(ws, "stop", "x"); code != 3 {
		t.Fatalf("stop needs the session: %d", code)
	}
}

// Spec: run --dry-run needs a trusted file, reads for real, prints what
// it would write and writes nothing; runs and logs show the saved run; a
// real run from the CLI needs the session.
func TestWorkflowCLIDryRunRunsLogs(t *testing.T) {
	ws := cliWorkspace(t)
	if code, _, errs := cliRun(ws, "run", "docs", "--dry-run"); code != 1 || !strings.Contains(errs, "not trusted") {
		t.Fatalf("untrusted %d: %s", code, errs)
	}
	root, _ := ledger.Root(ws)
	e, _ := wire.Find(wire.Load(ws, "", root), "docs")
	if err := wire.Trust(root, e); err != nil {
		t.Fatal(err)
	}
	code, out, errs := cliRun(ws, "run", "docs", "--dry-run")
	if code != 0 || !strings.Contains(out, "would write") || !strings.Contains(out, "1 docs") {
		t.Fatalf("dry run %d:\n%s\n%s", code, out, errs)
	}
	if _, err := os.Stat(filepath.Join(ws, "reports")); !os.IsNotExist(err) {
		t.Fatal("a dry run wrote")
	}
	code, out, _ = cliRun(ws, "runs", "docs")
	if code != 0 || !strings.Contains(out, "dry") {
		t.Fatalf("runs %d:\n%s", code, out)
	}
	id := strings.Fields(strings.Split(strings.TrimSpace(out), "\n")[0])[0]
	if code, out, _ = cliRun(ws, "logs", id); code != 0 || !strings.Contains(out, "docs") || !strings.Contains(out, "rep") {
		t.Fatalf("logs %d:\n%s", code, out)
	}
	if code, _, _ = cliRun(ws, "run", "docs"); code != 3 {
		t.Fatalf("a real run needs the session: %d", code)
	}
}
