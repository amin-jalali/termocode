//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// WFX-9: build a workflow by chat, end to end in the real binary. The
// scripted model drafts a workflow (card: Create), validates it, dry runs
// it (card: Dry run), and runs it (card: Run); the run's write asks on
// its gate card and Enter (Deny, first and focused) is what the run file
// records. Nothing is written to the workspace.

const wfxE2EFile = `{
  // made by the AI in the e2e test
  "version": 1, "name": "docs-count", "title": "Docs count",
  "steps": [
    { "id": "docs", "type": "source", "from": "files", "glob": "docs/*.md" },
    { "id": "rep", "type": "write", "to": "file", "path": "reports/docs.md", "content": "{{len(steps.docs.items)}} docs" }
  ]
}
`

// wfxRunFile is one run file of the workspace (the newest real run).
type wfxRunFile struct {
	ID     string `json:"id"`
	Dry    bool   `json:"dry"`
	State  string `json:"state"`
	Writes []struct {
		Step  string `json:"step"`
		State string `json:"state"`
	} `json:"writes"`
}

func wfxRunFiles(t *testing.T, home string) []wfxRunFile {
	t.Helper()
	paths, _ := filepath.Glob(filepath.Join(home, ".local", "state", "termocode", "workflows", "*", "runs", "*.json"))
	var out []wfxRunFile
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var rf wfxRunFile
		if json.Unmarshal(b, &rf) == nil {
			out = append(out, rf)
		}
	}
	return out
}

func TestWorkflowBuildByChat(t *testing.T) {
	r := newSessionRig(t)
	if err := os.MkdirAll(filepath.Join(r.ws, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"a.md", "b.md"} {
		if err := os.WriteFile(filepath.Join(r.ws, "docs", n), []byte("# "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	calls := []scnCall{
		{"workflow_draft", map[string]any{"name": "docs-count", "content": wfxE2EFile}},
		{"workflow_validate", map[string]any{"name": "docs-count"}},
		{"workflow_dry_run", map[string]any{"name": "docs-count"}},
		{"workflow_run", map[string]any{"name": "docs-count"}},
	}
	model, srv := newSCNChatModel(t, calls)
	r.env = append(r.env, "TERMOCODE_AI_PROVIDER=openai", "TERMOCODE_AI_MODEL=fake", "TERMOCODE_AI_BASE_URL="+srv.URL+"/v1",
		"TERMOCODE_AI_API_KEY="+"sk-"+"e2e-fake-key")

	tm := Start(t, StartOpts{Home: r.home, Workspace: r.ws, Env: r.env, Cols: 200, Rows: 48})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	tm.settle()

	// AI: New Workflow… opens the chat with the starter prompt.
	tm.Palette("AI: New Workflow")
	tm.WaitText("AI CHAT", uiTimeout)
	tm.WaitText("Make a workflow that", uiTimeout)
	tm.settle()
	tm.Type("counts the docs and writes a report")
	tm.Send(KeyEnter)

	// 1. The draft card: the file and its write; Cancel has the focus.
	path := filepath.Join(r.ws, ".termocode", "workflows", "docs-count.jsonc")
	tm.WaitText("docs-count.jsonc (new, 8 lines)", 30*time.Second)
	tm.WaitText("rep → file reports/docs.md", uiTimeout)
	tm.WaitText("Cancel", uiTimeout)
	if exists(path) {
		t.Fatal("the file was written before Create")
	}
	tm.settle()
	tm.Send("\t") // Cancel → Create
	tm.Send(KeyEnter)
	tm.WaitFor("the file", 30*time.Second, func([]string) bool { return exists(path) })
	if b, _ := os.ReadFile(path); string(b) != wfxE2EFile {
		t.Errorf("file %q", b)
	}

	// 2. The dry-run card (plan cost), then the dry run itself.
	tm.WaitText("Dry run docs-count", 30*time.Second)
	tm.WaitText("Plan cost: none", uiTimeout)
	tm.settle()
	tm.Send("\t")
	tm.Send(KeyEnter)

	// 3. The run card lists the write; Run starts it.
	tm.WaitText("Run docs-count now", 60*time.Second)
	tm.settle()
	tm.Send("\t")
	tm.Send(KeyEnter)

	// 4. The write asks on its gate card; Enter is Deny.
	tm.WaitText("docs-count wants to write", 30*time.Second)
	tm.settle()
	tm.Send(KeyEnter)
	var real wfxRunFile
	tm.WaitFor("the run file says denied", 30*time.Second, func([]string) bool {
		for _, rf := range wfxRunFiles(t, r.home) {
			if !rf.Dry && rf.State != "running" && len(rf.Writes) == 1 && rf.Writes[0].State == "denied" {
				real = rf
				return true
			}
		}
		return false
	})
	if exists(filepath.Join(r.ws, "reports", "docs.md")) {
		t.Error("the denied write was written")
	}

	// The model saw: created, valid, the dry run's recorded write, started.
	res := model.toolResults()
	want := []string{"Created .termocode/workflows/docs-count.jsonc", "valid: docs-count", "rep → file reports/docs.md (create): recorded · 2 docs", "Run " + real.ID + " started"}
	if len(res) != len(want) {
		t.Fatalf("%d tool results: %q", len(res), res)
	}
	for i, w := range want {
		if !strings.Contains(res[i], w) {
			t.Errorf("result %d misses %q: %s", i, w, res[i])
		}
	}
	dry := 0
	for _, rf := range wfxRunFiles(t, r.home) {
		if rf.Dry && rf.State == "done" && len(rf.Writes) == 1 && rf.Writes[0].State == "recorded" {
			dry++
		}
	}
	if dry != 1 {
		t.Errorf("%d finished dry runs with a recorded write", dry)
	}
	tm.Quit()
}
