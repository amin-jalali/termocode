package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/textdiff"
)

// The review of AI file changes (ai_review_tools.go): refusals before a
// card, "nothing changed", reject, a file changed during the review,
// partial accepts, LSP edits and the setting toggle.

func covReviewModel(t *testing.T) (*Model, string) {
	t.Helper()
	m := aiTestModel(t)
	root := t.TempDir()
	t.Chdir(root)
	return &m, root
}

// Proposals refuse a folder and creating a file that exists; the same
// content is a note, not a card.
func TestCovAIProposeRefusals(t *testing.T) {
	m, root := covReviewModel(t)
	_ = os.MkdirAll(filepath.Join(root, "dir"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "a.txt"), []byte("same\n"), 0o644)
	if p := m.aiProposeChanges([]aiFileChange{{abs: filepath.Join(root, "dir"), rel: "dir", text: "x"}}); p.err == nil || !strings.Contains(p.err.Error(), "is a folder") {
		t.Errorf("folder: %v", p.err)
	}
	if p := m.aiProposeChanges([]aiFileChange{{abs: filepath.Join(root, "a.txt"), rel: "a.txt", text: "x", create: true}}); p.err == nil || !strings.Contains(p.err.Error(), "already exists") {
		t.Errorf("create existing: %v", p.err)
	}
	p := m.aiProposeChanges([]aiFileChange{{abs: filepath.Join(root, "a.txt"), rel: "a.txt", text: "same\n"}})
	if p.err != nil || len(p.chans) != 0 || len(p.notes) != 1 || !strings.Contains(p.notes[0], "already has this content") {
		t.Errorf("same content: %+v", p)
	}
	out, err := aiReviewChanges(context.Background(), covSyncCall(m), []aiFileChange{{abs: filepath.Join(root, "a.txt"), rel: "a.txt", text: "same\n"}})
	if err != nil || !strings.Contains(out, "already has this content") {
		t.Errorf("review with nothing to do: %q %v", out, err)
	}
	if _, err := aiReviewChanges(context.Background(), covSyncCall(m), []aiFileChange{{abs: filepath.Join(root, "dir"), rel: "dir"}}); err == nil {
		t.Error("a folder was proposed")
	}
	stopped := func(context.Context, aiIDEFunc) (string, error) { return "", errAIChatStopped }
	if _, err := aiReviewChanges(context.Background(), stopped, nil); err != errAIChatStopped {
		t.Errorf("stopped chat: %v", err)
	}
}

// The finish of one disk change: reject (new file and changed file), the
// file changed on disk meanwhile, accept all, and a partial accept with
// "edit by hand".
func TestCovAIDiskReviewFinish(t *testing.T) {
	m, root := covReviewModel(t)
	abs := filepath.Join(root, "f.go")
	base := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"
	_ = os.WriteFile(abs, []byte(base), 0o644)
	next := "A\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nL\n"
	d := textdiff.Compute(base, next)
	if len(d.Hunks) != 2 {
		t.Fatalf("want 2 hunks, got %d", len(d.Hunks))
	}
	ch := make(chan string, 1)
	it := m.diskReviewItem(aiFileChange{abs: abs, rel: "f.go", text: next}, base, true, "", 0, ch)
	it.finish(m, d, map[int]bool{}, false)
	if got := <-ch; !strings.Contains(got, "rejected the change to f.go") {
		t.Errorf("reject: %q", got)
	}
	newItem := m.diskReviewItem(aiFileChange{abs: filepath.Join(root, "new.go"), rel: "new.go", text: "x\n"}, "", false, "new file", 0, ch)
	newItem.finish(m, textdiff.Compute("", "x\n"), map[int]bool{}, false)
	if got := <-ch; !strings.Contains(got, "rejected creating new.go") {
		t.Errorf("reject new: %q", got)
	}
	_ = os.WriteFile(abs, []byte(base+"user line\n"), 0o644)
	it.finish(m, d, map[int]bool{0: true}, false)
	if got := <-ch; !strings.Contains(got, "changed on disk while the user reviewed") {
		t.Errorf("changed meanwhile: %q", got)
	}
	_ = os.WriteFile(abs, []byte(base), 0o644)
	it.finish(m, d, map[int]bool{0: true}, true)
	got := <-ch
	if !strings.Contains(got, "accepted 1 of 2 changes to f.go") || !strings.Contains(got, "+L") || !strings.Contains(got, "to edit it by hand") {
		t.Errorf("partial: %q", got)
	}
	if b, _ := os.ReadFile(abs); !strings.HasPrefix(string(b), "A\n") || !strings.HasSuffix(string(b), "l\n") {
		t.Errorf("file after partial accept:\n%s", b)
	}
	if m.focus != FocusEditor {
		t.Error("edit by hand did not move the focus")
	}
	// Long hunks list only their first lines.
	long := textdiff.Compute(strings.Repeat("o\n", 8), strings.Repeat("n\n", 8))
	if txt := aiReviewAcceptedText("x", long, map[int]bool{}, ""); !strings.Contains(txt, "… 4 more removed") || !strings.Contains(txt, "… 4 more added") {
		t.Errorf("long hunk text:\n%s", txt)
	}
	if txt := aiReviewAcceptedText("x", d, map[int]bool{0: true, 1: true}, ""); txt != "The user accepted all 2 changes to x." {
		t.Errorf("all: %q", txt)
	}
}

// LSP edits: utf-8 / utf-16 / utf-32 columns, a bad range, edits past
// the end, and the preview of a file with unsaved changes.
func TestCovAILSPEdits(t *testing.T) {
	text := "héllo 𝄞 world\nline2\n"
	at := func(l, c int) lspPos { return lspPos{Line: l, Character: c} }
	edit := func(s, e lspPos, nt string) lspTextEdit {
		var x lspTextEdit
		x.Range.Start, x.Range.End, x.NewText = s, e, nt
		return x
	}
	for enc, col := range map[string]int{"utf-16": 9, "utf-8": 12, "utf-32": 8} {
		out, err := applyTextEdits(text, []lspTextEdit{edit(at(0, col), at(0, col+5), "WORLD")}, enc)
		if err != nil || !strings.HasPrefix(out, "héllo 𝄞 WORLD\n") {
			t.Errorf("%s: %q %v", enc, out, err)
		}
	}
	if _, err := applyTextEdits(text, []lspTextEdit{edit(at(1, 3), at(1, 1), "x")}, ""); err == nil {
		t.Error("an end before the start was applied")
	}
	if _, err := applyTextEdits(text, []lspTextEdit{edit(at(-1, 0), at(0, 0), "x")}, ""); err == nil {
		t.Error("a negative position was applied")
	}
	if out, _ := applyTextEdits(text, []lspTextEdit{edit(at(9, 0), at(9, 0), "tail")}, ""); !strings.HasSuffix(out, "tail") {
		t.Errorf("past the end: %q", out)
	}
	root := t.TempDir()
	p := filepath.Join(root, "a.go")
	_ = os.WriteFile(p, []byte("x := 1\n"), 0o644)
	rel := aiRel(root)
	if _, err := aiLSPPreviewChanges("NOPE", rel); err == nil {
		t.Error("an unknown answer was read")
	}
	if _, err := aiLSPPreviewChanges("PREVIEW\t{bad", rel); err == nil {
		t.Error("bad JSON was read")
	}
	if _, err := aiLSPPreviewChanges(`PREVIEW	{"files":[{"path":"`+p+`","modified":true}]}`, rel); err == nil || !strings.Contains(err.Error(), "unsaved changes") {
		t.Errorf("modified file: %v", err)
	}
	if _, err := aiLSPPreviewChanges(`PREVIEW	{"files":[{"path":"`+filepath.Join(root, "gone.go")+`"}]}`, rel); err == nil {
		t.Error("a missing file was read")
	}
	if _, err := aiLSPPreviewChanges(`PREVIEW	{"files":[{"path":"`+p+`","edits":[{"range":{"start":{"line":0,"character":5},"end":{"line":0,"character":1}},"newText":"x"}]}]}`, rel); err == nil {
		t.Error("a bad edit was applied")
	}
	ch, err := aiLSPPreviewChanges(`PREVIEW	{"files":[{"path":"`+p+`","edits":[{"range":{"start":{"line":0,"character":5},"end":{"line":0,"character":6}},"newText":"2"}]}]}`, rel)
	if err != nil || len(ch) != 1 || ch[0].text != "x := 2\n" || ch[0].rel != "a.go" {
		t.Errorf("preview: %+v %v", ch, err)
	}
}

// AI: Toggle Edit Review flips the saved setting and says what it means.
func TestCovAIToggleEditReview(t *testing.T) {
	m := aiTestModel(t)
	m.aiS().settings.EditReview = ai.EditReviewOn
	m.toggleAIEditReview()
	if m.aiS().settings.EditReview != ai.EditReviewAuto || !strings.Contains(covToastText(&m), "no diff review") {
		t.Errorf("to auto: %q %q", m.aiS().settings.EditReview, covToastText(&m))
	}
	m.aiS().settings.AutoApprove = []string{ai.ClassFileWrite}
	m.toggleAIEditReview()
	if !m.aiS().settings.ReviewEdits() || !strings.Contains(covToastText(&m), "fileWrite is auto-approved") {
		t.Errorf("to review: %q", covToastText(&m))
	}
	p, _ := ai.ConfigPath()
	if ai.LoadSettings(p).EditReview != ai.EditReviewOn {
		t.Error("the setting was not saved")
	}
}

// Without the editor the language-server tools say so; a bad path is
// refused; two writes in one step are both on the review list at once,
// and each call then waits for its own file.
func TestCovAIReviewToolsWrapping(t *testing.T) {
	m, root := covReviewModel(t)
	call := covSyncCall(m)
	base := []ai.Tool{
		{Name: "write_file", Run: func(context.Context, map[string]any) (string, error) { return "raw", nil }},
		{Name: "format"}, {Name: "lsp_rename"},
		{Name: "lsp_code_actions", Gate: func(a map[string]any) (bool, string) { return ai.ArgString(a, "op") == "apply", "" },
			Run: func(context.Context, map[string]any) (string, error) { return "listed", nil }},
	}
	tools := map[string]ai.Tool{}
	for _, tl := range m.aiReviewTools(base, root, call) {
		tools[tl.Name] = tl
	}
	ctx := context.Background()
	for _, n := range []string{"format", "lsp_rename"} {
		if _, err := tools[n].Run(ctx, map[string]any{"path": "a.go"}); err == nil || err.Error() != "the editor is not running" {
			t.Errorf("%s without the editor: %v", n, err)
		}
		if _, err := tools[n].Run(ctx, map[string]any{}); err == nil || err.Error() != "path is required" {
			t.Errorf("%s without path: %v", n, err)
		}
	}
	if out, _ := tools["lsp_code_actions"].Run(ctx, map[string]any{"op": "list"}); out != "listed" {
		t.Errorf("list op = %q", out)
	}
	if _, err := tools["lsp_code_actions"].Run(ctx, map[string]any{"op": "apply", "path": "a.go"}); err == nil {
		t.Error("apply without the editor ran")
	}
	if _, err := tools["lsp_code_actions"].Run(ctx, map[string]any{"op": "apply"}); err == nil {
		t.Error("apply without path ran")
	}
	if mut, class := tools["lsp_code_actions"].Gate(map[string]any{"op": "list"}); mut || class != "" {
		t.Errorf("list gate = %v %q", mut, class)
	}
	if mut, class := tools["lsp_code_actions"].Gate(map[string]any{"op": "apply"}); !mut || class != ai.ClassReview {
		t.Errorf("apply gate = %v %q", mut, class)
	}
	if _, err := tools["write_file"].Run(ctx, map[string]any{"path": "../outside.txt", "content": "x"}); err == nil {
		t.Error("a write outside the workspace was proposed")
	}

	before := aiBeforeCalls(root, call)
	before(ctx, []ai.ToolCall{
		{ID: "c1", Name: "write_file", Args: map[string]any{"path": "one.txt", "content": "1\n"}},
		{ID: "c2", Name: "create_file", Args: map[string]any{"path": "two.txt", "content": "2\n"}},
		{ID: "c3", Name: "write_file", Args: map[string]any{"path": "one.txt", "content": "again\n"}},
		{ID: "c4", Name: "read_file", Args: map[string]any{"path": "x"}},
	})
	if len(m.aiS().preReview) != 2 || m.aiS().review == nil || len(m.aiS().review.items) != 2 {
		t.Fatalf("pre-review: %d proposals, review %+v", len(m.aiS().preReview), m.aiS().review)
	}
	// Reject the first: the proposal kept for call c1 gets the answer.
	pre := m.aiS().preReview["c1"]
	m.aiS().review.items[0].finish(m, textdiff.Compute("", "1\n"), map[int]bool{}, false)
	if got := <-pre.chans[0]; !strings.Contains(got, "rejected creating one.txt") {
		t.Errorf("c1 answer = %q", got)
	}
	// One call alone proposes nothing ahead.
	before(ctx, []ai.ToolCall{{ID: "c9", Name: "write_file", Args: map[string]any{"path": "x.txt"}}})
	if _, ok := m.aiS().preReview["c9"]; ok {
		t.Error("a single call was proposed ahead")
	}
}

// A code action that can not be previewed asks with Deny first; the
// answer comes back to the tool; a stopped chat answers no.
func TestCovAIAskConfirm(t *testing.T) {
	m := aiTestModel(t)
	opened := make(chan struct{})
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(&m)
		close(opened)
		return out, err
	}
	got := make(chan bool, 1)
	go func() {
		got <- aiAskConfirm(context.Background(), call, ai.Tool{Name: "lsp_code_actions", Class: ai.ClassReview}, "Apply fix")
	}()
	<-opened
	if !m.confirmOpen || len(m.confirm.Buttons()) != 2 || m.confirm.Buttons()[0].ID != "deny" {
		t.Fatalf("card buttons: %+v", m.confirm.Buttons())
	}
	m.aiS().confirm.reply <- true
	if !<-got {
		t.Error("allow was not passed back")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := func(context.Context, aiIDEFunc) (string, error) { return "", errAIChatStopped }
	if aiAskConfirm(ctx, stopped, ai.Tool{Name: "x"}, "x") {
		t.Error("a stopped chat said yes")
	}
}
