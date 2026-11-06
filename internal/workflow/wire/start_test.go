package wire

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

func sampleWorkspace(t *testing.T) (ws, root string, e Entry) {
	t.Helper()
	ws, root = t.TempDir(), t.TempDir()
	writeWF(t, filepath.Join(ws, ".termocode", "workflows"), "docs", sprintf(sampleWF, "docs"))
	if err := os.MkdirAll(filepath.Join(ws, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(ws, "docs", n), []byte("# "+n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e, _ = Find(Load(ws, "", root), "docs")
	return ws, root, e
}

// Spec (WFX wiring, end to end): a dry run of a trusted sample workflow
// reads its files source for real, lists the write with its full text
// and writes nothing; the run file is saved under the state root.
func TestStartDryRunEndToEnd(t *testing.T) {
	ws, root, e := sampleWorkspace(t)
	cfg := Config{Workspace: ws, Root: root, Entry: e, RunID: "dry1", Dry: true, Src: &source.Sources{Workspace: ws}}
	if _, err := Start(context.Background(), cfg); err == nil {
		t.Fatal("an untrusted repo file does not run")
	}
	if err := Trust(root, e); err != nil {
		t.Fatal(err)
	}
	cfg.Entry, _ = Find(Load(ws, "", root), "docs")
	h, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the run did not end")
	}
	rf := h.Snapshot()
	if rf.State != run.RunOK || !rf.Dry {
		t.Fatalf("state %s dry %v err %s", rf.State, rf.Dry, rf.Error)
	}
	if n := len(rf.Steps["docs"].Items); n != 2 {
		t.Fatalf("read %d items", n)
	}
	if len(rf.Writes) != 1 || rf.Writes[0].State != run.StateRecorded || rf.Writes[0].Spec.Content != "2 docs" {
		t.Fatalf("writes %+v", rf.Writes)
	}
	if _, err := os.Stat(filepath.Join(ws, "reports", "docs.md")); !os.IsNotExist(err) {
		t.Fatal("a dry run wrote the file")
	}
	if saved, err := (RunFiles{Root: root}).Load("dry1"); err != nil || saved.State != run.RunOK {
		t.Fatalf("run file %v %v", saved, err)
	}
}

// Spec: a real run sends the write through the gate; the seen ledger
// marks the source items of a run that ended well.
func TestStartRealRunWritesAfterTheGate(t *testing.T) {
	ws, root, e := sampleWorkspace(t)
	if err := Trust(root, e); err != nil {
		t.Fatal(err)
	}
	e, _ = Find(Load(ws, "", root), "docs")
	asked := 0
	cfg := Config{Workspace: ws, Root: root, Entry: e, RunID: "real1", Src: &source.Sources{Workspace: ws},
		Gate: func(context.Context, GateAsk) (GateDecision, error) { asked++; return GateAllowOnce, nil }}
	h, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	rf := h.Wait()
	if rf.State != run.RunOK || asked != 1 {
		t.Fatalf("state %s asked %d err %s", rf.State, asked, rf.Error)
	}
	b, err := os.ReadFile(filepath.Join(ws, "reports", "docs.md"))
	if err != nil || string(b) != "2 docs" {
		t.Fatalf("%q %v", b, err)
	}
	seen, err := ledger.OpenSeen(root, "docs")
	if err != nil || !seen.Has("docs/a.md") {
		t.Fatalf("seen %v", err)
	}
}
