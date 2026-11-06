package wire

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

// Spec (WFX-20, plan §6.6): every run keeps runs/<id>/audit.jsonl (0600):
// one JSON line per step event and per gate answer, with the time, the
// step, the item, the target, the answer and by=workflow:<name>/<step>.
func TestStartWritesAuditFile(t *testing.T) {
	ws, root, e := sampleWorkspace(t)
	if err := Trust(root, e); err != nil {
		t.Fatal(err)
	}
	e, _ = Find(Load(ws, "", root), "docs")
	cfg := Config{Workspace: ws, Root: root, Entry: e, RunID: "aud1", Src: &source.Sources{Workspace: ws},
		Gate: func(context.Context, GateAsk) (GateDecision, error) { return GateAllowOnce, nil }}
	h, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if rf := h.Wait(); rf.State != run.RunOK {
		t.Fatalf("run %s %s", rf.State, rf.Error)
	}
	path := filepath.Join(root, "runs", "aud1", "audit.jsonl")
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("audit file %v %v", fi, err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	kinds := map[string]map[string]any{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l map[string]any
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("bad line %q: %v", sc.Text(), err)
		}
		if l["at"] == nil || l["run"] != "aud1" {
			t.Errorf("line without time or run: %v", l)
		}
		key, _ := l["kind"].(string)
		if s, _ := l["step"].(string); s != "" {
			key += "/" + s
		}
		kinds[key] = l
	}
	for _, want := range []string{"step_start/docs", "step_done/docs", "gate/rep", "step_done/rep", "run_done"} {
		if kinds[want] == nil {
			t.Errorf("no %s line in %v", want, kinds)
		}
	}
	if g := kinds["gate/rep"]; g != nil && (g["answer"] != "allow once" || g["target"] == "" || g["by"] != "workflow:docs/rep") {
		t.Errorf("gate line %v", g)
	}
}
