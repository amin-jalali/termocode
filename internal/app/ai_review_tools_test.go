package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// Group R2: the agent's file changes go through the review card. Each
// test runs one agent turn (scripted provider, real nvim) and answers
// the card like the user would.

// reviewModel is aiSyncModel in review mode (nothing auto-approved).
func reviewModel(t *testing.T, p ai.Provider, files map[string]string) (*Model, *nvim.Client, string) {
	t.Helper()
	m, c, dir := aiSyncModel(t, p, files)
	m.ai.settings.AutoApprove = nil
	return m, c, dir
}

// driveAIReview is driveAIChat that calls onReview after every message
// while a review is waiting (onReview decides when to act). Confirm
// dialogs are denied.
func driveAIReview(t *testing.T, m *Model, cmd tea.Cmd, onReview func(m *Model)) {
	t.Helper()
	for i := 0; cmd != nil && i < 400; i++ {
		msg := nextAIMsg(t, cmd)
		if msg == nil {
			return
		}
		if _, ok := msg.(aiChatConfirmMsg); ok {
			m.handleAIMsg(msg)
			m.answerAIToolConfirm("deny")
			m.confirmOpen = false
		} else if _, ok := m.handleAIMsg(msg); !ok {
			t.Fatalf("unrouted msg %T", msg)
		}
		if _, done := msg.(aiChatDoneMsg); done {
			return
		}
		if onReview != nil && m.aiReviewPending() > 0 {
			onReview(m)
		}
		cmd = waitAIChat(m.ai.chatCh)
	}
}

const reviewOld = "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
const reviewNew = "one\nTWO\nthree\nfour\nfive\nsix\nseven\neight\nNINE\nten\n"

// Spec: write_file in review mode shows the diff; the user accepts
// change 1 and rejects change 2: disk and the open tab have exactly
// change 1 (one undo step), and the model is told 1 of 2 was accepted
// and which change was not written.
func TestAIReviewWriteFileAcceptOneRejectOne(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("write_file", map[string]any{"path": "a.txt", "content": reviewNew})}}
	m, c, dir := reviewModel(t, p, map[string]string{"a.txt": reviewOld})
	path := filepath.Join(dir, "a.txt")
	m.openFileAtLine(path, 1)
	driveAIReview(t, m, m.sendAIChat("change it"), func(m *Model) {
		if !m.reviewOpen {
			t.Fatal("review card not open")
		}
		if got := readFile(path); got != reviewOld {
			t.Fatalf("written before the review: %q", got)
		}
		reviewKey(t, m, "a")
		reviewKey(t, m, "r")
	})
	want := "one\nTWO\nthree\nfour\nfive\nsix\nseven\neight\nnine\nten\n"
	if got := readFile(path); got != want {
		t.Fatalf("disk %q", got)
	}
	if got := bufText(t, c, path); got+"\n" != want {
		t.Errorf("buffer %q", got)
	}
	r := oneResult(t, p)
	for _, s := range []string{"accepted 1 of 2 changes to a.txt", "-nine", "+NINE", "Read it again"} {
		if !strings.Contains(r, s) {
			t.Errorf("result lacks %q:\n%s", s, r)
		}
	}
	_ = c.Command("normal! u")
	if got := bufText(t, c, path); got+"\n" != reviewOld {
		t.Errorf("one undo: buffer %q", got)
	}
	if m.ai.review != nil || m.reviewOpen {
		t.Error("review still open")
	}
}

// Spec: Reject file writes nothing and tells the model not to retry.
func TestAIReviewRejectAll(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("write_file", map[string]any{"path": "a.txt", "content": reviewNew})}}
	m, _, dir := reviewModel(t, p, map[string]string{"a.txt": reviewOld})
	driveAIReview(t, m, m.sendAIChat("change it"), func(m *Model) { reviewKey(t, m, "R") })
	if got := readFile(filepath.Join(dir, "a.txt")); got != reviewOld {
		t.Fatalf("disk changed: %q", got)
	}
	if r := oneResult(t, p); !strings.Contains(r, "rejected the change to a.txt; nothing was written") {
		t.Errorf("result %q", r)
	}
}

// Spec: Stop while the card waits: the turn ends, nothing is written,
// the card shows the file as stopped and closes.
func TestAIReviewStop(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("write_file", map[string]any{"path": "a.txt", "content": reviewNew})}}
	m, _, dir := reviewModel(t, p, map[string]string{"a.txt": reviewOld})
	stopped := false
	driveAIReview(t, m, m.sendAIChat("change it"), func(m *Model) {
		if !stopped {
			stopped = true
			m.aiCancelAll()
		}
	})
	if !stopped {
		t.Fatal("the review never showed")
	}
	if got := readFile(filepath.Join(dir, "a.txt")); got != reviewOld {
		t.Fatalf("disk changed: %q", got)
	}
	if m.ai.review != nil || m.reviewOpen || m.ai.streaming {
		t.Errorf("review %v open %v streaming %v", m.ai.review != nil, m.reviewOpen, m.ai.streaming)
	}
}

// Spec: two write_file calls in one step show both files in one review
// list at once; each call gets its own answer.
func TestAIReviewTwoFilesOneStep(t *testing.T) {
	two := ai.Chunk{ToolCalls: []ai.ToolCall{
		{ID: "c1", Name: "write_file", Args: map[string]any{"path": "a.txt", "content": "A\n"}},
		{ID: "c2", Name: "write_file", Args: map[string]any{"path": "b.txt", "content": "B\n"}},
	}}
	p := &scriptedAIProvider{replies: []ai.Chunk{two}}
	m, _, dir := reviewModel(t, p, map[string]string{"a.txt": "a\n", "b.txt": "b\n"})
	seenBoth := false
	driveAIReview(t, m, m.sendAIChat("change both"), func(m *Model) {
		if m.aiReviewPending() == 2 && !seenBoth {
			seenBoth = true
			if m.ai.review.card.Files() != 2 {
				t.Fatalf("files %d", m.ai.review.card.Files())
			}
			reviewKey(t, m, "A") // a.txt accepted, moves to b.txt
			reviewKey(t, m, "R") // b.txt rejected
		}
	})
	if !seenBoth {
		t.Fatal("the two files never showed together")
	}
	if readFile(filepath.Join(dir, "a.txt")) != "A\n" || readFile(filepath.Join(dir, "b.txt")) != "b\n" {
		t.Fatalf("disk a=%q b=%q", readFile(filepath.Join(dir, "a.txt")), readFile(filepath.Join(dir, "b.txt")))
	}
	r := p.toolResults()
	if len(r) != 2 || !strings.Contains(r[0], "accepted the change to a.txt") || !strings.Contains(r[1], "rejected the change to b.txt") {
		t.Errorf("results %q", r)
	}
}

// Spec: create_file is reviewed as a new file; accepting creates it.
func TestAIReviewCreateFile(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("create_file", map[string]any{"path": "sub/new.md", "content": "# hi\n"})}}
	m, _, dir := reviewModel(t, p, nil)
	driveAIReview(t, m, m.sendAIChat("make it"), func(m *Model) {
		if !strings.Contains(m.viewFrame(), "new file") {
			t.Errorf("no new-file tag on the card")
		}
		reviewKey(t, m, "A")
	})
	if got := readFile(filepath.Join(dir, "sub", "new.md")); got != "# hi\n" {
		t.Fatalf("new file %q", got)
	}
	if r := oneResult(t, p); !strings.Contains(r, "accepted the change to sub/new.md") || !strings.Contains(r, "wrote sub/new.md") {
		t.Errorf("result %q", r)
	}
}

// Spec: with fileWrite auto-approved (or ai_edit_review = auto) nothing
// is reviewed: the old path writes at once.
func TestAIReviewOffWritesDirectly(t *testing.T) {
	for _, mode := range []string{"auto-approve", "setting"} {
		p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("write_file", map[string]any{"path": "a.txt", "content": "x\n"})}}
		m, _, dir := reviewModel(t, p, map[string]string{"a.txt": "a\n"})
		if mode == "setting" {
			m.ai.settings.EditReview = ai.EditReviewAuto
			m.ai.settings.AutoApprove = []string{ai.ClassFileWrite}
		} else {
			m.ai.settings.AutoApprove = []string{ai.ClassFileWrite}
		}
		driveAIReview(t, m, m.sendAIChat("go"), func(*Model) { t.Fatal("reviewed") })
		if got := readFile(filepath.Join(dir, "a.txt")); got != "x\n" {
			t.Errorf("%s: disk %q", mode, got)
		}
	}
}

// Spec: the wait is bounded: with nobody answering, the call returns an
// error after aiReviewTimeout and the file is not written.
func TestAIReviewTimeout(t *testing.T) {
	old := aiReviewTimeout
	aiReviewTimeout = time.Millisecond
	t.Cleanup(func() { aiReviewTimeout = old })
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("write_file", map[string]any{"path": "a.txt", "content": reviewNew})}}
	m, _, dir := reviewModel(t, p, map[string]string{"a.txt": reviewOld})
	driveAIReview(t, m, m.sendAIChat("change it"), nil)
	if got := readFile(filepath.Join(dir, "a.txt")); got != reviewOld {
		t.Fatalf("disk changed: %q", got)
	}
	if r := oneResult(t, p); !strings.Contains(r, "did not review") {
		t.Errorf("result %q", r)
	}
}

func TestApplyTextEdits(t *testing.T) {
	edit := func(l1, c1, l2, c2 int, s string) lspTextEdit {
		var e lspTextEdit
		e.Range.Start, e.Range.End, e.NewText = lspPos{l1, c1}, lspPos{l2, c2}, s
		return e
	}
	text := "a := \"é😀x\"\nfoo(bar)\n"
	// utf-16: é is 1 unit, 😀 is 2: x is at character 9 on line 0.
	got, err := applyTextEdits(text, []lspTextEdit{edit(0, 9, 0, 10, "y"), edit(1, 0, 1, 3, "baz")}, "utf-16")
	if err != nil || got != "a := \"é😀y\"\nbaz(bar)\n" {
		t.Errorf("utf-16: %q %v", got, err)
	}
	got, _ = applyTextEdits(text, []lspTextEdit{edit(0, 12, 0, 13, "y")}, "utf-8")
	if got != "a := \"é😀y\"\nfoo(bar)\n" {
		t.Errorf("utf-8: %q", got)
	}
	// Two inserts at one place keep their order.
	got, _ = applyTextEdits("ab", []lspTextEdit{edit(0, 1, 0, 1, "1"), edit(0, 1, 0, 1, "2")}, "")
	if got != "a12b" {
		t.Errorf("inserts: %q", got)
	}
	// Past the end appends.
	got, _ = applyTextEdits("a\n", []lspTextEdit{edit(5, 0, 5, 0, "z\n")}, "")
	if got != "a\nz\n" {
		t.Errorf("append: %q", got)
	}
}

// Spec (OMX-2): edit_file in review mode shows the one changed part as
// a diff; accepting writes it, and the open tab follows.
func TestAIReviewEditFile(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("edit_file", map[string]any{"path": "a.txt", "old_text": "two\n", "new_text": "TWO\n"})}}
	m, c, dir := reviewModel(t, p, map[string]string{"a.txt": reviewOld})
	path := filepath.Join(dir, "a.txt")
	m.openFileAtLine(path, 1)
	driveAIReview(t, m, m.sendAIChat("change it"), func(m *Model) {
		if got := readFile(path); got != reviewOld {
			t.Fatalf("written before the review: %q", got)
		}
		if n := len(m.ai.review.items); n != 1 {
			t.Fatalf("%d review items, want 1", n)
		}
		reviewKey(t, m, "A")
	})
	want := strings.Replace(reviewOld, "two\n", "TWO\n", 1)
	if got := readFile(path); got != want {
		t.Fatalf("disk %q", got)
	}
	if got := bufText(t, c, path); got+"\n" != want {
		t.Errorf("buffer %q", got)
	}
	if r := oneResult(t, p); !strings.Contains(r, "accepted the change to a.txt") {
		t.Errorf("result %q", r)
	}
}

// Spec (OMX-2): a stale old_text is refused before any card opens.
func TestAIReviewEditFileStale(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("edit_file", map[string]any{"path": "a.txt", "old_text": "zero\n", "new_text": "x\n"})}}
	m, _, dir := reviewModel(t, p, map[string]string{"a.txt": reviewOld})
	driveAIReview(t, m, m.sendAIChat("change it"), func(*Model) { t.Fatal("a stale edit was reviewed") })
	if got := readFile(filepath.Join(dir, "a.txt")); got != reviewOld {
		t.Fatalf("disk changed: %q", got)
	}
	if r := oneResult(t, p); !strings.Contains(r, "old_text was not found") {
		t.Errorf("result %q", r)
	}
}

// Spec (OMX-2): with review off, edit_file writes through the editor
// sync path at once.
func TestAIEditFileWithoutReview(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("edit_file", map[string]any{"path": "a.txt", "old_text": "a", "new_text": "b"})}}
	m, c, dir := reviewModel(t, p, map[string]string{"a.txt": "a\n"})
	m.ai.settings.AutoApprove = []string{ai.ClassFileWrite}
	path := filepath.Join(dir, "a.txt")
	m.openFileAtLine(path, 1)
	driveAIReview(t, m, m.sendAIChat("go"), func(*Model) { t.Fatal("reviewed") })
	if got := readFile(path); got != "b\n" {
		t.Errorf("disk %q", got)
	}
	if got := bufText(t, c, path); got != "b" {
		t.Errorf("the open tab did not follow: %q", got)
	}
	if r := oneResult(t, p); !strings.Contains(r, "a.txt") {
		t.Errorf("result %q", r)
	}
}
