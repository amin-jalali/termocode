package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// The AI code actions (ai_actions.go) against a small headless nvim:
// Explain, Fix, Edit and Doc comment, and every way the one-shot answer
// can end (provider error, cancel, no code, no change, a proposal).

// covScriptedProvider streams fixed chunks: text, then an optional error.
type covScriptedProvider struct {
	text string
	err  error
	reqs chan ai.Request
}

func (covScriptedProvider) Name() string        { return "scripted" }
func (covScriptedProvider) Configured() bool    { return true }
func (covScriptedProvider) SupportsTools() bool { return true }
func (p covScriptedProvider) Stream(_ context.Context, req ai.Request) <-chan ai.Chunk {
	if p.reqs != nil {
		select {
		case p.reqs <- req:
		default:
		}
	}
	ch := make(chan ai.Chunk, 3)
	if p.text != "" {
		ch <- ai.Chunk{Delta: p.text}
	}
	ch <- ai.Chunk{Done: true, Err: p.err}
	close(ch)
	return ch
}

// covNvim starts a headless nvim for one test and closes it after.
func covNvim(t *testing.T) *nvim.Client {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	return c
}

// covCollect runs a Cmd (and the Cmds of a batch, last first: the work
// comes after the toast tick) and returns the first message of type T.
func covCollect[T any](cmd tea.Cmd) (T, bool) {
	var zero T
	if cmd == nil {
		return zero, false
	}
	switch msg := cmd().(type) {
	case T:
		return msg, true
	case tea.BatchMsg:
		for i := len(msg) - 1; i >= 0; i-- {
			if v, ok := covCollect[T](msg[i]); ok {
				return v, true
			}
		}
	}
	return zero, false
}

func covActionsModel(t *testing.T) (*Model, *nvim.Client, string) {
	t.Helper()
	m := aiTestModel(t)
	c := covNvim(t)
	m.nvim = c
	path := filepath.Join(t.TempDir(), "main.go")
	src := "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := c.Command("edit " + path); err != nil {
		t.Fatal(err)
	}
	_ = c.Command("call cursor(4, 1)")
	return &m, c, path
}

// Without a provider every action shows the configure toast and nothing
// else; without a file buffer each says "Open a file first".
func TestCovAIActionsNeedProviderAndFile(t *testing.T) {
	m := aiTestModel(t)
	for _, kind := range []string{synthKindAIExplain, synthKindAIFix, synthKindAIEdit, synthKindAIDoc} {
		m.applyAICodeAction(synthCodeAction{Kind: kind})
		if m.promptOpen || m.aiS().actionBusy != "" {
			t.Errorf("%s ran without a provider", kind)
		}
	}
	if m.applyAICodeAction(synthCodeAction{Kind: "ai.other"}) != nil {
		t.Error("unknown kind did something")
	}
	m.aiS().provider = covScriptedProvider{}
	for _, kind := range []string{synthKindAIExplain, synthKindAIFix, synthKindAIEdit, synthKindAIDoc} {
		if m.applyAICodeAction(synthCodeAction{Kind: kind}) == nil {
			t.Errorf("%s without a file: no message", kind)
		}
	}
	if m.proposeWholeFile("x", "t") == nil {
		t.Error("whole-file proposal without a file: no message")
	}
	var titles []string
	m.aiCodeActions("", 1, func(title, _ string, _ synthCodeAction) { titles = append(titles, title) })
	if len(titles) != 0 {
		t.Errorf("actions without a file: %v", titles)
	}
	if m.aiDiagnosticsAt(0) != nil || m.aiLineHasDiagnostic(1) {
		t.Error("diagnostics without an editor")
	}
}

// The picker lists Fix only on a line with a diagnostic.
func TestCovAIActionsPickerAndDiagnostics(t *testing.T) {
	m, c, path := covActionsModel(t)
	list := func() string {
		var titles []string
		m.aiCodeActions(path, 4, func(title, _ string, _ synthCodeAction) { titles = append(titles, title) })
		return strings.Join(titles, "|")
	}
	if got := list(); strings.Contains(got, "Fix with AI") || !strings.Contains(got, "Explain with AI") {
		t.Errorf("no diagnostic: %s", got)
	}
	m.aiS().provider = covScriptedProvider{}
	if m.aiFixDiagnostic() == nil || m.aiS().actionBusy != "" {
		t.Error("fix without a diagnostic must only say so")
	}
	if err := c.ExecLua(`local ns = vim.api.nvim_create_namespace('cov')
vim.diagnostic.set(ns, 0, {{ lnum = 3, col = 0, severity = 1, message = 'undefined: x' }})`); err != nil {
		t.Fatal(err)
	}
	if got := list(); !strings.Contains(got, "Fix with AI") {
		t.Errorf("with a diagnostic: %s", got)
	}
	if got := m.aiDiagnosticsAt(3); len(got) != 1 || got[0] != "line 4: [error] undefined: x" {
		t.Errorf("diagnostics = %q", got)
	}
	reqs := make(chan ai.Request, 1)
	m.aiS().provider = covScriptedProvider{text: "```go\n\tprintln(\"fixed\")\n```", reqs: reqs}
	cmd := m.aiFixDiagnostic()
	if m.aiS().actionBusy != "fix" {
		t.Fatalf("fix did not start: %q", m.aiS().actionBusy)
	}
	res, ok := covCollect[aiActionResultMsg](cmd)
	if !ok {
		t.Fatal("no result")
	}
	req := <-reqs
	if !strings.Contains(req.Messages[1].Content, "Fix these problems:\nline 4: [error] undefined: x") {
		t.Errorf("fix prompt:\n%s", req.Messages[1].Content)
	}
	m.onAIActionResult(res)
	if !m.reviewOpen || m.aiS().review == nil {
		t.Error("the fix did not open the review card")
	}
}

// Explain attaches ±10 lines around the cursor (or the selection) and
// sends the question to the chat.
func TestCovAIExplainAttachesCode(t *testing.T) {
	m, c, _ := covActionsModel(t)
	m.aiS().provider = fakeAIProvider{reply: "it prints"}
	if m.aiExplain() == nil {
		t.Fatal("explain sent nothing")
	}
	s := m.aiS()
	last := covLastUser(s.conv)
	if !strings.Contains(last.Text, "Explain the code around line 4.") || !strings.Contains(last.Context, "main.go:1-5") {
		t.Errorf("explain message: %q context %q", last.Text, last.Context)
	}
	s.streaming = false
	_ = c.Command("call cursor(3, 1)")
	_ = c.Input("Vj")
	t.Cleanup(func() { _ = c.Input("\x1b") })
	m.aiExplain()
	last = covLastUser(s.conv)
	if last.Text != "Explain this code." || !strings.Contains(last.Context, "main.go:3-4") {
		t.Errorf("selection explain: %q context %q", last.Text, last.Context)
	}
}

// Edit with AI asks for an instruction; an empty one does nothing; the
// instruction goes into the request.
func TestCovAIEditAndDocComment(t *testing.T) {
	m, _, _ := covActionsModel(t)
	reqs := make(chan ai.Request, 4)
	m.aiS().provider = covScriptedProvider{text: "```go\n\tprintln(\"bye\")\n```", reqs: reqs}
	m.aiEditSelection()
	if !m.promptOpen || m.promptKind != promptKindAIEdit || m.aiS().editTarget == nil {
		t.Fatal("the instruction prompt did not open")
	}
	if m.onAIEditInstruction("  ") != nil || m.aiS().editTarget != nil {
		t.Error("an empty instruction ran")
	}
	if m.onAIEditInstruction("say bye") != nil {
		t.Error("no target: must do nothing")
	}
	m.aiEditSelection()
	cmd := m.onAIEditInstruction("say bye")
	res, ok := covCollect[aiActionResultMsg](cmd)
	if !ok || res.title != "AI edit: say bye" {
		t.Fatalf("edit result %+v", res)
	}
	if req := <-reqs; !strings.Contains(req.Messages[1].Content, "Instruction: say bye") || !strings.Contains(req.Messages[1].Content, "Range to replace (lines 4-4)") {
		t.Errorf("edit prompt:\n%s", req.Messages[1].Content)
	}
	m.aiDocComment()
	if m.aiS().actionBusy != "doc" {
		t.Errorf("doc comment busy = %q", m.aiS().actionBusy)
	}
	// A second request cancels the first.
	first := m.aiS().actionGen
	m.aiDocComment()
	if m.aiS().actionGen != first+1 {
		t.Error("a new request did not replace the old one")
	}
}

// The one-shot answer: a stale one is dropped; cancel is silent; an
// error is a toast; no code and no change say so; otherwise the review
// card opens.
func TestCovAIActionResultEnds(t *testing.T) {
	m := aiTestModel(t)
	s := m.aiS()
	target := aiRangeTarget{path: "/w/a.go", start: 0, end: 1, full: []string{"a := 1", "b := 2"}}
	s.actionGen = 5
	if m.onAIActionResult(aiActionResultMsg{gen: 4, text: "x"}) != nil {
		t.Error("a stale answer did something")
	}
	s.actionBusy = "edit"
	if m.onAIActionResult(aiActionResultMsg{gen: 5, err: context.Canceled}) != nil || s.actionBusy != "" {
		t.Error("cancel was not silent")
	}
	if m.onAIActionResult(aiActionResultMsg{gen: 5, err: errors.New("overloaded_error: try later")}) == nil {
		t.Error("an error mid-stream did not toast")
	}
	if m.onAIActionResult(aiActionResultMsg{gen: 5, text: "```\n\n```", target: target}) == nil {
		t.Error("no code: no message")
	}
	if m.onAIActionResult(aiActionResultMsg{gen: 5, text: "a := 1", target: target}) == nil || m.reviewOpen {
		t.Error("the same code must say no changes, not open a review")
	}
	m.onAIActionResult(aiActionResultMsg{gen: 5, text: "Here:\n```go\na := 42\n```", target: target, title: "AI edit"})
	if !m.reviewOpen || len(s.review.items) != 1 {
		t.Error("a change did not open the review")
	}
	// The whole provider path: an error after some text reaches the toast.
	s.provider = covScriptedProvider{text: "```go\npartial", err: errors.New("stream broke")}
	res, ok := covCollect[aiActionResultMsg](m.runAIEdit(target, "edit", "AI edit", "x"))
	if !ok || res.err == nil || !strings.Contains(res.err.Error(), "stream broke") {
		t.Errorf("mid-stream error: %+v", res)
	}
}

// The captured range is clamped to the buffer.
func TestCovAICaptureTargetNonFileBuffer(t *testing.T) {
	m, c, path := covActionsModel(t)
	tg, ok := m.aiCaptureTarget(false)
	if !ok || tg.path != path || tg.cursor != 3 || tg.start != 3 || tg.end != 4 || len(tg.full) != 5 {
		t.Fatalf("target = %+v %v", tg, ok)
	}
	if err := c.Command("enew | setlocal buftype=nofile"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.aiCaptureTarget(false); ok {
		t.Error("a scratch buffer is a target")
	}
	if err := c.Command("enew"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.aiCaptureTarget(false); ok {
		t.Error("an unnamed buffer is a target")
	}
}

func covLastUser(c ai.Conversation) ai.StoredMessage {
	for i := len(c.Messages) - 1; i >= 0; i-- {
		if c.Messages[i].Role == "user" {
			return c.Messages[i]
		}
	}
	return ai.StoredMessage{}
}
