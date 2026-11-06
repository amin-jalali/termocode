package wire

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/workflow"
	"github.com/amin-jalali/termocode/internal/workflow/ledger"
	"github.com/amin-jalali/termocode/internal/workflow/run"
)

var _ run.Writer = (*Writer)(nil)

type sentCall struct {
	server, tool string
	args         map[string]any
}

type writeTools struct{ sent []sentCall }

func (w *writeTools) CallTool(ctx context.Context, server, tool string, args map[string]any) (json.RawMessage, bool, error) {
	w.sent = append(w.sent, sentCall{server, tool, args})
	return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), false, nil
}

func newWriter(t *testing.T, ask func(GateAsk) GateDecision) (*Writer, *writeTools, string) {
	t.Helper()
	root := t.TempDir()
	l, err := ledger.Open(root, "bugs", "r1")
	if err != nil {
		t.Fatal(err)
	}
	wt := &writeTools{}
	w := &Writer{Root: root, Workflow: "bugs", Hash: "h1", Ledger: l, Workspace: t.TempDir(), Tools: wt,
		Ask: func(_ context.Context, a GateAsk) (GateDecision, error) { return ask(a), nil }}
	return w, wt, root
}

func mcpWrite(gate string) run.WriteCall {
	st := &workflow.Step{ID: "note", Type: workflow.TypeWrite}
	return run.WriteCall{Step: st, Item: &run.Item{Key: "LIN-1"}, Marker: ledger.Marker("r1", "note", "LIN-1"),
		Spec: workflow.WriteSpec{To: "mcp", Gate: gate, Server: "linear", Tool: "create_comment",
			Args: map[string]any{"issueId": "LIN-1", "body": "Draft PR: x"}}}
}

// Spec (WFX wiring, plan §6.3): a write asks on the gate card first; Deny
// sends nothing and the error wraps run.ErrDenied; Allow once sends with
// the marker at the end of the body.
func TestWriterAsksThenSends(t *testing.T) {
	var asked []GateAsk
	w, wt, _ := newWriter(t, func(a GateAsk) GateDecision { asked = append(asked, a); return GateDeny })
	waited := 0
	c := mcpWrite(workflow.GateAsk)
	c.Wait = func() { waited++ }
	if _, err := w.Write(context.Background(), c); !errors.Is(err, run.ErrDenied) {
		t.Fatalf("deny: %v", err)
	}
	if len(wt.sent) != 0 || waited != 1 || len(asked) != 1 {
		t.Fatalf("sent %d waited %d asked %d", len(wt.sent), waited, len(asked))
	}
	if a := asked[0]; a.Step != "note" || a.Item != "LIN-1" || !strings.Contains(a.Target, "create_comment") || !strings.Contains(a.Preview, "Draft PR: x") {
		t.Fatalf("ask %+v", a)
	}
	w2, wt2, _ := newWriter(t, func(GateAsk) GateDecision { return GateAllowOnce })
	if _, err := w2.Write(context.Background(), mcpWrite(workflow.GateAsk)); err != nil {
		t.Fatal(err)
	}
	if len(wt2.sent) != 1 || !strings.HasSuffix(wt2.sent[0].args["body"].(string), "termocode-run:r1/note/LIN-1") {
		t.Fatalf("sent %+v", wt2.sent)
	}
	e, ok := w2.Ledger.Get("note", "LIN-1")
	if !ok || e.State != ledger.Done {
		t.Fatalf("ledger %+v %v", e, ok)
	}
}

// Spec: gate "allow" skips the card only when the allow-list outside the
// repo has the step for this file hash.
func TestWriterAllowListBoundToHash(t *testing.T) {
	asks := 0
	w, wt, root := newWriter(t, func(GateAsk) GateDecision { asks++; return GateDeny })
	if _, err := w.Write(context.Background(), mcpWrite(workflow.GateAllow)); !errors.Is(err, run.ErrDenied) || asks != 1 {
		t.Fatalf("not on the list: asks %d err %v", asks, err)
	}
	if err := ledger.AllowStep(root, "bugs", "h1", "note"); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(context.Background(), mcpWrite(workflow.GateAllow)); err != nil || asks != 1 || len(wt.sent) != 1 {
		t.Fatalf("on the list: asks %d sent %d err %v", asks, len(wt.sent), err)
	}
	if _, err := w.Write(context.Background(), mcpWrite(workflow.GateAsk)); err != nil || asks != 1 || len(wt.sent) != 1 {
		t.Fatalf("a done write is not sent or asked again: asks %d sent %d err %v", asks, len(wt.sent), err)
	}
}

// Spec (plan §4.3): a resumed write that was "sending" is looked up by
// its marker first: found → recorded done, nothing sent, no card; a
// target that cannot look up sends nothing (it may have arrived).
func TestWriterResumedChecksMarker(t *testing.T) {
	asks := 0
	w, wt, _ := newWriter(t, func(GateAsk) GateDecision { asks++; return GateAllowOnce })
	// a crash between "sending" and "done": the ledger has sending.
	_, _, err := w.Ledger.Do(context.Background(), ledger.Write{Step: "note", Item: "LIN-1"},
		func(context.Context, string) (ledger.Result, error) { return ledger.Result{}, errors.New("crash") }, nil)
	if err == nil {
		t.Fatal("setup")
	}
	c := mcpWrite(workflow.GateAsk)
	c.Resumed = true
	if _, err := w.Write(context.Background(), c); !errors.Is(err, ledger.ErrMaybeSent) || len(wt.sent) != 0 || asks != 0 {
		t.Fatalf("mcp has no lookup: err %v sent %d asks %d", err, len(wt.sent), asks)
	}

	found := 0
	w.Find = func(_ context.Context, spec workflow.WriteSpec, marker string) (ledger.Result, bool, error) {
		found++
		return ledger.Result{ExternalID: "comment-9"}, marker == "termocode-run:r1/note/LIN-1", nil
	}
	out, err := w.Write(context.Background(), c)
	if err != nil || found != 1 || len(wt.sent) != 0 || asks != 0 || out.URL != "comment-9" {
		t.Fatalf("found: out %+v err %v find %d sent %d asks %d", out, err, found, len(wt.sent), asks)
	}
}

// Spec: a file write stays inside the workspace, create refuses an
// existing file, and the text written ends with nothing but the content.
func TestWriterFile(t *testing.T) {
	w, _, _ := newWriter(t, func(GateAsk) GateDecision { return GateAllowOnce })
	st := &workflow.Step{ID: "rep", Type: workflow.TypeWrite}
	c := run.WriteCall{Step: st, Marker: "m", Spec: workflow.WriteSpec{To: "file", Path: "reports/a.md", Content: "hi"}}
	if _, err := w.Write(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(w.Workspace, "reports", "a.md"))
	if err != nil || string(b) != "hi" {
		t.Fatalf("%q %v", b, err)
	}
	st2 := &workflow.Step{ID: "rep2", Type: workflow.TypeWrite}
	if _, err := w.Write(context.Background(), run.WriteCall{Step: st2, Spec: workflow.WriteSpec{To: "file", Path: "reports/a.md", Content: "x"}}); err == nil {
		t.Fatal("create over an existing file")
	}
	st3 := &workflow.Step{ID: "rep3", Type: workflow.TypeWrite}
	if _, err := w.Write(context.Background(), run.WriteCall{Step: st3, Spec: workflow.WriteSpec{To: "file", Path: "../out.md", Content: "x"}}); err == nil {
		t.Fatal("a path outside the workspace")
	}
	st4 := &workflow.Step{ID: "pr", Type: workflow.TypeWrite}
	if _, err := w.Write(context.Background(), run.WriteCall{Step: st4, Spec: workflow.WriteSpec{To: "pr", Title: "x"}}); err == nil {
		t.Fatal("pr writes come later; a clear error now")
	}
}
