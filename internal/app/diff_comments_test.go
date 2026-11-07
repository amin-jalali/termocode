package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/diffview"
)

// diffCommentsModel opens a read-only changes card (an agent worktree's
// changes) with one file.
func diffCommentsModel(t *testing.T) Model {
	t.Helper()
	m := aiTestModel(t)
	files := []diffview.File{{Name: "main.go", Old: "a\nb\nc\n", New: "a\nB\nc\n"}}
	m.aiS().review = &aiReview{card: diffview.New("Changes of Claude Code · agent/fix", files, false)}
	m.showAIReview()
	return m
}

func diffCommentsKey(t *testing.T, m *Model, k string) {
	t.Helper()
	mm, cmd := m.routeToReview(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	*m = mm
	if cmd == nil {
		return
	}
	if _, ok := m.handleAIMsg(cmd()); !ok {
		t.Fatalf("%q: the card's message was not routed", k)
	}
}

// Spec (CMP-51): c on a change opens a prompt; its text becomes the
// change's comment on the card.
func TestDiffCommentPromptAddsComment(t *testing.T) {
	m := diffCommentsModel(t)
	diffCommentsKey(t, &m, "c")
	if !m.promptOpen || m.promptKind != promptKindDiffComment {
		t.Fatalf("prompt open=%v kind=%v", m.promptOpen, m.promptKind)
	}
	m.promptOpen = false
	if _, ok := m.handleAIPromptSubmit("add a test for B"); !ok {
		t.Fatal("prompt kind not routed")
	}
	if n := m.aiS().review.card.CommentCount(); n != 1 {
		t.Fatalf("%d comments, want 1", n)
	}
}

// Spec: S opens a Cancel-first card that offers the AI chat, each agent
// terminal tab and Copy; Cancel keeps the comments.
func TestDiffCommentsSendCardCancelFirst(t *testing.T) {
	m := diffCommentsModel(t)
	m.terminalTabs = []terminalTab{{BufID: 7, Name: "claude", Agent: "claude"}, {BufID: 8, Name: "zsh"}}
	m.aiS().review.card.SetComment(0, 0, "why upper case?")
	diffCommentsKey(t, &m, "S")
	if !m.confirmOpen || m.confirmKind != confirmKindDiffComments {
		t.Fatalf("card open=%v kind=%v", m.confirmOpen, m.confirmKind)
	}
	b := m.confirm.Buttons()
	if b[0].ID != "cancel" {
		t.Fatalf("first button %q, want cancel", b[0].ID)
	}
	ids := map[string]bool{}
	for _, x := range b {
		ids[x.ID] = true
	}
	if !ids["chat"] || !ids["copy"] || !ids["tab:7"] || ids["tab:8"] {
		t.Fatalf("buttons %v: want chat, copy and only the agent tab", ids)
	}
	m.confirmOpen = false
	if _, ok := m.handleAIConfirm("cancel"); !ok {
		t.Fatal("confirm kind not routed")
	}
	if n := m.aiS().review.card.CommentCount(); n != 1 {
		t.Fatalf("Cancel dropped the comments (%d left)", n)
	}
}

// Spec: hiding the card keeps its comments; Agent: Send Diff Comments…
// opens the send card for them later.
func TestDiffCommentsKeptAfterHide(t *testing.T) {
	m := diffCommentsModel(t)
	m.aiS().review.card.SetComment(0, 0, "rename it")
	if _, ok := m.handleAIMsg(diffview.HideMsg{}); !ok {
		t.Fatal("HideMsg not routed")
	}
	if m.aiS().review != nil {
		t.Fatal("a read-only card goes away on Esc")
	}
	if len(m.aiS().diffNotes.kept) != 1 {
		t.Fatal("the comments were lost on Esc")
	}
	if _, ok := m.dispatchAgentPalette(diffCommentsPaletteID); !ok {
		t.Fatal("palette id not routed")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindDiffComments || !strings.Contains(ansi.Strip(m.confirm.Box()), "rename it") {
		t.Fatalf("send card not open for the kept comments: open=%v", m.confirmOpen)
	}
}

// Spec: the AI may not send the user's review comments on its own.
func TestDiffCommentsPolicyNever(t *testing.T) {
	if p := aiActionPolicies[diffCommentsPaletteID]; p.Gate != aiGateNever {
		t.Fatalf("policy %+v, want never", p)
	}
}

// Spec: a paste into an agent tab is one bracketed paste with no Enter,
// so the agent's own input box gets the whole batch.
func TestDiffCommentsPasteText(t *testing.T) {
	got := diffCommentsPaste("a\nb")
	if got != "\x1b[200~a\nb\x1b[201~" {
		t.Fatalf("paste %q", got)
	}
}
