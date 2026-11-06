package main

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow/ledger"
)

// Spec (WFX-16, plan §5.9): workflow again <name> <key> forgets one item
// of the seen ledger, so the next run handles it again; an unknown key
// or workflow fails, a missing argument is usage. No session needed.
func TestWorkflowCLIAgain(t *testing.T) {
	ws := cliWorkspace(t)
	root, err := ledger.Root(ws)
	if err != nil {
		t.Fatal(err)
	}
	seen, err := ledger.OpenSeen(root, "docs")
	if err != nil {
		t.Fatal(err)
	}
	if err := seen.Mark("LIN-228", "run-1", "done"); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := cliRun(ws, "again", "docs"); code != 2 {
		t.Fatalf("missing key: %d", code)
	}
	if code, _, errw := cliRun(ws, "again", "docs", "LIN-999"); code != 1 || !strings.Contains(errw, "LIN-999") {
		t.Fatalf("unknown key: %d %q", code, errw)
	}
	if code, _, _ := cliRun(ws, "again", "nope", "LIN-228"); code != 1 {
		t.Fatalf("unknown workflow: %d", code)
	}
	code, out, errw := cliRun(ws, "again", "docs", "LIN-228")
	if code != 0 || !strings.Contains(out, "LIN-228") {
		t.Fatalf("again: %d %q %q", code, out, errw)
	}
	seen, _ = ledger.OpenSeen(root, "docs")
	if seen.Has("LIN-228") {
		t.Fatal("the key is still seen")
	}
}
