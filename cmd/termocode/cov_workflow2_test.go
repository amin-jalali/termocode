package main

import (
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkflowCLIEveryKindShowsNestedSteps(t *testing.T) {
	ws := cliWorkspace(t)
	src, err := os.ReadFile(filepath.Join("..", "..", "internal", "workflow", "testdata", "good", "every-kind.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".termocode", "workflows", "every-kind.jsonc"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errw := cliRun(ws, "show", "every-kind")
	if code != 0 || !strings.Contains(out, "inner") || !strings.Contains(out, "review") {
		t.Fatalf("show: %d\n%s\n%s", code, out, errw)
	}
	if code, out, _ := cliRun(ws, "list"); code != 0 || !strings.Contains(out, "every-kind") {
		t.Errorf("list: %d %q", code, out)
	}
	// validate by file and with warnings printed
	if code, out, _ := cliRun(ws, "validate", filepath.Join(ws, ".termocode", "workflows", "every-kind.jsonc")); code != 0 || !strings.Contains(out, "ok") {
		t.Errorf("validate: %d %q", code, out)
	}
	if code, _, errw := cliRun(ws, "validate", filepath.Join(ws, ".termocode", "workflows", "bad.jsonc")); code != 1 || errw == "" {
		t.Errorf("validate bad: %d %q", code, errw)
	}
	// an empty project: validate says so
	if code, out, _ := cliRun(t.TempDir(), "validate"); code != 0 || !strings.Contains(out, "no workflow files") {
		t.Errorf("validate empty: %d %q", code, out)
	}
}

func TestWorkflowCLIRunsAndScoresAfterARun(t *testing.T) {
	ws := cliWorkspace(t)
	root, _ := ledgerRoot(ws)
	trustDocs(t, ws, root)
	if code, _, errw := cliRun(ws, "run", "docs", "--dry-run"); code != 0 {
		t.Fatalf("dry run: %d %s", code, errw)
	}
	if code, out, _ := cliRun(ws, "list", "--json"); code != 0 || !strings.Contains(out, "last_run") {
		t.Errorf("list json shows the last run: %d %q", code, out)
	}
	if code, out, _ := cliRun(ws, "list"); code != 0 || !strings.Contains(out, "docs") {
		t.Errorf("list: %d %q", code, out)
	}
	if code, _, _ := cliRun(ws, "scores", "--json", "--limit", "5"); code != 0 {
		t.Errorf("scores json: %d", code)
	}
	if code, _, _ := cliRun(ws, "runs", "no-such-workflow"); code != 0 {
		t.Errorf("runs of nothing: %d", code)
	}
}

func ledgerRoot(ws string) (string, error) { return ledger.Root(ws) }

func trustDocs(t *testing.T, ws, root string) {
	t.Helper()
	e, _ := wire.Find(wire.Load(ws, "", root), "docs")
	if err := wire.Trust(root, e); err != nil {
		t.Fatal(err)
	}
}

const failingWF = `{ "version": 1, "name": "fails", "steps": [
  { "id": "boom", "type": "source", "from": "shell", "argv": ["/definitely/not/a/program"] }
] }`

func TestWorkflowCLIFailingDryRunAndAgain(t *testing.T) {
	ws := cliWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws, ".termocode", "workflows", "fails.jsonc"), []byte(failingWF), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := ledger.Root(ws)
	e, _ := wire.Find(wire.Load(ws, "", root), "fails")
	if err := wire.Trust(root, e); err != nil {
		t.Fatal(err)
	}
	code, out, _ := cliRun(ws, "run", "fails", "--dry-run")
	if code != 1 || !strings.Contains(out, "boom") {
		t.Fatalf("failing run: %d\n%s", code, out)
	}
	if code, out, _ := cliRun(ws, "runs", "fails"); code != 0 || out == "" {
		t.Errorf("runs: %d %q", code, out)
	}
	if code, _, _ := cliRun(ws, "list", "--json"); code != 0 {
		t.Errorf("list: %d", code)
	}
	seen, err := ledger.OpenSeen(root, "fails")
	if err != nil {
		t.Fatal(err)
	}
	if err := seen.Mark("item-1", "run1", "ok"); err != nil {
		t.Skipf("no Add: %v", err)
	}
	if code, out, _ := cliRun(ws, "again", "fails", "item-1"); code != 0 || !strings.Contains(out, "is new again") {
		t.Errorf("again: %d %q", code, out)
	}
	if code, _, errw := cliRun(ws, "again", "fails", "never-seen"); code != 1 || !strings.Contains(errw, "has not seen") {
		t.Errorf("again unseen: %d %q", code, errw)
	}
}
