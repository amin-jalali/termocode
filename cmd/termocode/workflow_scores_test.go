package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	wfrun "github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/score"
	"github.com/amin-jalali/termocode/internal/workflow/wire"
)

const cliScoredWF = `{ "version": 1, "name": "graded",
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/*.md" },
    { "id": "g", "type": "score", "checks": [
      { "name": "has docs", "test": "len(steps.docs.items) > 0" },
      { "name": "two docs", "test": "len(steps.docs.items) == 2" } ] }
  ] }`

// Spec (CMP-23): a dry run prints each score step and the run's mean;
// "workflow scores" lists scored runs and compares them by their vars,
// the best first; runs without a score step are left out.
func TestWorkflowCLIScores(t *testing.T) {
	ws := cliWorkspace(t)
	p := filepath.Join(ws, ".termocode", "workflows", "graded.jsonc")
	if err := os.WriteFile(p, []byte(cliScoredWF), 0o644); err != nil {
		t.Fatal(err)
	}
	root, _ := ledger.Root(ws)
	e, _ := wire.Find(wire.Load(ws, "", root), "graded")
	if err := wire.Trust(root, e); err != nil {
		t.Fatal(err)
	}
	code, out, errs := cliRun(ws, "run", "graded", "--dry-run", "--var", "model=fast")
	if code != 0 || !strings.Contains(out, "score 0.50 ✗ (pass 0.70)") || !strings.Contains(out, "mean 0.50 · 0 of 1 passed") {
		t.Fatalf("dry run %d:\n%s\n%s", code, out, errs)
	}

	best := &wfrun.RunFile{Version: wfrun.RunFileVersion, ID: "20200101-000000.000", Workflow: "graded", State: wfrun.RunOK,
		StartedAt: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), Vars: map[string]any{"model": "deep"},
		Steps: map[string]*wfrun.StepState{"g": {State: wfrun.StateDone,
			Score: &score.Result{Score: 1, Pass: 0.7, Passed: true, Checks: []score.Check{{Name: "has docs", Weight: 1, Value: 1}}}}}}
	b, _ := json.Marshal(best)
	if err := (wire.RunFiles{Root: root}).WriteRunFile(best.ID, b); err != nil {
		t.Fatal(err)
	}

	code, out, _ = cliRun(ws, "scores", "graded")
	if code != 0 || !strings.Contains(out, "mean 1.00  1/1 passed  model=deep") || !strings.Contains(out, "mean 0.50  0/1 passed  model=fast dry") {
		t.Fatalf("scores %d:\n%s", code, out)
	}
	_, cmp, ok := strings.Cut(out, "compare (best first):\n")
	if !ok || !strings.HasPrefix(strings.TrimSpace(cmp), "mean 1.00  1 runs  1/1 passed  graded model=deep") {
		t.Fatalf("compare:\n%s", out)
	}

	code, out, _ = cliRun(ws, "scores", "graded", "--json")
	var got struct {
		Runs    []wfScoreRow  `json:"runs"`
		Compare []score.Group `json:"compare"`
	}
	if code != 0 || json.Unmarshal([]byte(out), &got) != nil || len(got.Runs) != 2 || len(got.Compare) != 2 {
		t.Fatalf("scores --json %d: %s", code, out)
	}

	if code, out, _ = cliRun(ws, "scores", "docs"); code != 0 || !strings.Contains(out, "no scored runs") {
		t.Fatalf("no scores %d: %s", code, out)
	}
}
