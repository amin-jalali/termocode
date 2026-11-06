package wire

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/run"
	"github.com/amin-jalali/termocode/internal/workflow/source"
)

var _ run.Source = (*Sources)(nil)

type fakeTools struct {
	readOnly bool
	calls    int
	allowed  []bool
	server   string
	args     map[string]any
}

func (f *fakeTools) CallTool(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, bool, error) {
	f.calls++
	f.server, f.args = server, args
	f.allowed = append(f.allowed, source.WriteAllowed(ctx))
	return json.RawMessage(`{"structuredContent":{"issues":[{"id":"LIN-1","title":"a"},{"id":"LIN-2","title":"b"}]}}`), f.readOnly, nil
}

// Spec (WFX wiring): a file's mcp source becomes a source.MCPInput: the
// filled args, the items path and the key template; a read-only tool
// runs without the confirm mark, a tool the user said yes to runs with it.
func TestSourcesMapsMCP(t *testing.T) {
	ft := &fakeTools{readOnly: true}
	s := &Sources{Src: &source.Sources{Workspace: t.TempDir(), Tools: ft}, Eval: Eval{}}
	step := &workflow.Step{ID: "bugs", Type: workflow.TypeSource, Source: &workflow.SourceSpec{
		From: "mcp", Server: "linear", Tool: "list_issues", Items: "$.issues", Key: "{{item.id}}", MaxItems: 10}}
	res, err := s.Read(context.Background(), run.SourceCall{Step: step, Args: map[string]any{"label": "auto-fix"}, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 2 || res.Items[0].Key != "LIN-1" || res.Items[1].Key != "LIN-2" {
		t.Fatalf("items %+v", res.Items)
	}
	if m, _ := res.Items[0].Data.(map[string]any); m["title"] != "a" {
		t.Fatalf("data is decoded JSON: %#v", res.Items[0].Data)
	}
	if ft.server != "linear" || ft.args["label"] != "auto-fix" || ft.allowed[0] {
		t.Fatalf("call %q %v allowed=%v", ft.server, ft.args, ft.allowed)
	}
	ft.readOnly = false
	if _, err := s.Read(context.Background(), run.SourceCall{Step: step, ReadOnly: false}); err != nil {
		t.Fatalf("a confirmed tool runs: %v", err)
	}
	if !ft.allowed[1] {
		t.Fatal("the engine calls a not read-only source only after a yes: the call carries the mark")
	}
}

// Spec: a key template that is not a plain item path is filled per item.
func TestSourcesKeyTemplate(t *testing.T) {
	ft := &fakeTools{readOnly: true}
	s := &Sources{Src: &source.Sources{Workspace: t.TempDir(), Tools: ft}, Eval: Eval{}}
	step := &workflow.Step{ID: "bugs", Type: workflow.TypeSource, Source: &workflow.SourceSpec{
		From: "mcp", Server: "linear", Tool: "list_issues", Items: "$.issues", Key: "bug-{{item.id}}"}}
	res, err := s.Read(context.Background(), run.SourceCall{Step: step, ReadOnly: true})
	if err != nil || len(res.Items) != 2 || res.Items[0].Key != "bug-LIN-1" {
		t.Fatalf("%+v %v", res.Items, err)
	}
}

// Spec: a files source reads the glob inside the workspace; db and
// github without their dependency fail clearly; an unknown kind fails.
func TestSourcesMapsFilesAndFails(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "docs", "a.md"), []byte("# A"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Sources{Src: &source.Sources{Workspace: ws}, Eval: Eval{}}
	files := &workflow.Step{ID: "docs", Type: workflow.TypeSource, Source: &workflow.SourceSpec{From: "files", Glob: "docs/**/*.md"}}
	res, err := s.Read(context.Background(), run.SourceCall{Step: files})
	if err != nil || len(res.Items) != 1 || res.Items[0].Key != "docs/a.md" {
		t.Fatalf("files: %+v %v", res.Items, err)
	}
	if _, err := s.Read(context.Background(), run.SourceCall{Step: files, ChangedSince: "not a time"}); err == nil {
		t.Fatal("a bad changed_since is an error")
	}
	for _, from := range []string{"db", "github", "nope"} {
		st := &workflow.Step{ID: "x", Type: workflow.TypeSource, Source: &workflow.SourceSpec{From: from, Connection: "c", SQL: "select 1", What: "issues"}}
		if _, err := s.Read(context.Background(), run.SourceCall{Step: st}); err == nil {
			t.Errorf("%s: want an error", from)
		}
	}
	if _, err := s.Read(context.Background(), run.SourceCall{Step: &workflow.Step{ID: "y"}}); err == nil {
		t.Error("a step without a source spec fails")
	}
}
