package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/diffview"
)

// nextMsg runs a cmd that must produce a message.
func nextMsg(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("no command")
	}
	return cmd()
}

// reviewKey sends one key to the open review card and routes the
// message it produces (FinishMsg / HideMsg) like Update does.
func reviewKey(t *testing.T, m *Model, k string) {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	nm, cmd := m.routeToReview(msg)
	*m = nm
	if cmd == nil {
		return
	}
	if _, ok := m.handleAIMsg(cmd()); !ok {
		t.Fatalf("review message not routed")
	}
}

// Spec: a one-shot AI edit opens the review card on screen (over the
// editor), and accepting one change of two puts exactly that change in
// the buffer as ONE undo step, unsaved.
func TestBufferProposalAcceptOneChange(t *testing.T) {
	m := aiTestModel(t)
	c := aiTestNvim(t, &m)
	m.w, m.h = 140, 40
	lines := []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	_ = c.Command("edit " + t.TempDir() + "/f.txt")
	if err := c.ExecLuaArgs(`vim.api.nvim_buf_set_lines(0, 0, -1, false, ...)`, lines); err != nil {
		t.Fatal(err)
	}
	tgt, ok := m.aiCaptureTarget(false)
	if !ok {
		t.Fatal("capture target")
	}
	tgt.start, tgt.end = 0, len(lines)
	repl := []string{"one", "TWO", "three", "four", "five", "six", "seven", "eight", "NINE", "ten"}
	m.openAIProposal(aiProposal{target: tgt, repl: repl, title: "AI edit"})
	if !m.reviewOpen {
		t.Fatal("review card not open")
	}
	frame := ansi.Strip(m.viewFrame())
	for _, want := range []string{"Review AI changes", "Change 1 of 2", "Accept file"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("%q not on screen:\n%s", want, frame)
		}
	}
	reviewKey(t, &m, "a") // accept change 1
	reviewKey(t, &m, "r") // reject change 2 → finishes
	if m.reviewOpen || m.ai.review != nil {
		t.Fatal("review still open")
	}
	got, _ := c.CurrentBufLines()
	want := []string{"one", "TWO", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("buffer %q", got)
	}
	_ = c.Command("silent! undo")
	got, _ = c.CurrentBufLines()
	if strings.Join(got, ",") != strings.Join(lines, ",") {
		t.Fatalf("one undo should restore: %q", got)
	}
}

// Spec: Esc hides the card while a file waits; the AI panel offers a
// Review button and the palette command brings it back.
func TestReviewHideAndReopen(t *testing.T) {
	m := aiTestModel(t)
	m.w, m.h = 140, 40
	tgt := aiRangeTarget{full: []string{"a", "b"}, start: 0, end: 2, path: "/x/f.go"}
	m.openAIProposal(aiProposal{target: tgt, repl: []string{"a", "B"}})
	reviewKey(t, &m, "esc")
	if m.reviewOpen || m.aiReviewPending() != 1 {
		t.Fatalf("open %v pending %d", m.reviewOpen, m.aiReviewPending())
	}
	if l := m.aiDividerLine(60); !strings.Contains(ansi.Strip(l.styled), "1 file to review") || len(l.buttons) != 1 {
		t.Errorf("divider %q", ansi.Strip(l.styled))
	}
	m.onAIButton("review-open")
	if !m.reviewOpen {
		t.Error("Review button did not reopen")
	}
	_ = diffview.HideMsg{}
}
