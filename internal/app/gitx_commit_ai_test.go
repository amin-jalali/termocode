package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/picker"
)

// gitxFakeAI answers every request with reply and keeps the last one.
type gitxFakeAI struct {
	mu    sync.Mutex
	reply string
	last  ai.Request
	calls int
}

func (p *gitxFakeAI) Name() string        { return "fake" }
func (p *gitxFakeAI) Configured() bool    { return true }
func (p *gitxFakeAI) SupportsTools() bool { return false }
func (p *gitxFakeAI) Stream(ctx context.Context, req ai.Request) <-chan ai.Chunk {
	p.mu.Lock()
	p.last, p.calls = req, p.calls+1
	p.mu.Unlock()
	ch := make(chan ai.Chunk, 2)
	ch <- ai.Chunk{Delta: p.reply}
	ch <- ai.Chunk{Done: true}
	close(ch)
	return ch
}

func (p *gitxFakeAI) prompt() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var b strings.Builder
	for _, m := range p.last.Messages {
		b.WriteString(m.Content + "\n")
	}
	return b.String()
}

func gitxWithAI(m *Model, reply string) *gitxFakeAI {
	p := &gitxFakeAI{reply: reply}
	m.aiS().provider = p
	return p
}

func gitxKey(m *Model, k tea.KeyType) {
	nm, _, _ := m.handleGitCommitKey(tea.KeyMsg{Type: k})
	*m = nm
}

// GITX-6 (B11): Up and Down in the message box step through the recent
// messages; Down past the newest brings the draft back.
func TestGitxCommitHistoryKeys(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "second msg")
	m.gitFocusCommitBox()
	m.gitCommitInsert("draft")
	gitxKey(m, tea.KeyUp)
	if m.gitCommitMsg != "second msg" {
		t.Fatalf("up: %q", m.gitCommitMsg)
	}
	gitxKey(m, tea.KeyUp)
	gitxKey(m, tea.KeyUp)
	if m.gitCommitMsg != "first" || m.gitCommitCaret != len("first") {
		t.Errorf("up up up: %q caret %d", m.gitCommitMsg, m.gitCommitCaret)
	}
	gitxKey(m, tea.KeyDown)
	gitxKey(m, tea.KeyDown)
	if m.gitCommitMsg != "draft" {
		t.Errorf("down down: %q", m.gitCommitMsg)
	}
}

// GITX-6 (B11): Pick Recent Commit Message… puts the picked one in the
// box.
func TestGitxCommitHistoryPicker(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "second msg")
	m.dispatchPaletteAction("git-commit-history")
	if !m.pickerOpen || m.pickerKind != pickerKindCommitHistory {
		t.Fatal("no picker")
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "hist-1"})
	if m.gitCommitMsg != "first" || !m.gitCommitFocused {
		t.Errorf("box %q focused %v", m.gitCommitMsg, m.gitCommitFocused)
	}
}

// GITX-6 (B12): focusing the empty box fills in commit.template once.
func TestGitxCommitTemplate(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, ".gitmessage", "feat: \n# say why\n")
	covGit(t, dir, "config", "commit.template", ".gitmessage")
	m.gitFocusCommitBox()
	if m.gitCommitMsg != "feat: " || m.gitCommitCaret != len("feat: ") {
		t.Fatalf("box %q caret %d", m.gitCommitMsg, m.gitCommitCaret)
	}
	m.gitCommitMsg = ""
	m.gitBlurCommitBox()
	m.gitFocusCommitBox()
	if m.gitCommitMsg != "" {
		t.Errorf("template filled in again after the user cleared it: %q", m.gitCommitMsg)
	}
}

// GITX-6 (B13): the ruler counts the subject; past 50 it warns, past 72
// it is over; a second line that is not blank warns too.
func TestGitxCommitRuler(t *testing.T) {
	for _, c := range []struct {
		msg   string
		text  string
		level int
	}{
		{strings.Repeat("a", 50), "50", 0},
		{strings.Repeat("a", 51), "51", 1},
		{strings.Repeat("a", 73), "73", 2},
		{"fix\nbody", "3", 1},
		{"fix\n\nbody", "3", 0},
	} {
		text, level := gitCommitRuler(c.msg)
		if text != c.text || level != c.level {
			t.Errorf("%q: %q %d want %q %d", c.msg, text, level, c.text, c.level)
		}
	}
	m, _ := covGitRepo(t)
	m.gitFocusCommitBox()
	m.gitCommitInsert(strings.Repeat("a", 51) + "\nb")
	v := ansi.Strip(m.renderGitCommitField(40))
	if !strings.Contains(v, "51") || !strings.Contains(v, "⏎") {
		t.Errorf("field %q", v)
	}
}

// GITX-6 (B15): Edit Commit Message in Editor writes the message to a
// file in .git; saving the file puts its text (no comments) in the box.
func TestGitxCommitEditInBuffer(t *testing.T) {
	m, dir := covGitRepo(t)
	m.gitCommitMsg = "feat: a"
	m.dispatchPaletteAction("git-commit-edit")
	f := filepath.Join(dir, ".git", "TERMOCODE_EDITMSG")
	b, err := os.ReadFile(f)
	if err != nil || !strings.HasPrefix(string(b), "feat: a\n") || !strings.Contains(string(b), "\n# ") {
		t.Fatalf("file %q %v", b, err)
	}
	if m.gitCommitEditFile != f {
		t.Fatalf("not tracked: %q", m.gitCommitEditFile)
	}
	if err := os.WriteFile(f, []byte("feat: b\n\nbody\n# comment\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(f, later, later)
	m.syncCommitEditFile()
	if m.gitCommitMsg != "feat: b\n\nbody" {
		t.Errorf("box %q", m.gitCommitMsg)
	}
}

// GITX-6 (B16): Add Co-Author… lists the other authors and adds a
// Co-authored-by trailer.
func TestGitxCoAuthor(t *testing.T) {
	m, dir := covGitRepo(t)
	covWrite(t, dir, "b.txt", "b\n")
	covGit(t, dir, "add", "b.txt")
	covGit(t, dir, "commit", "-q", "--author", "Bo <bo@x.io>", "-m", "by bo")
	covGit(t, dir, "config", "user.email", "tess@example.com")
	m.gitCommitMsg = "feat: pair"
	m.dispatchPaletteAction("git-co-author")
	if !m.pickerOpen || m.pickerKind != pickerKindCoAuthor {
		t.Fatal("no picker")
	}
	if v := ansi.Strip(m.picker.View()); strings.Contains(v, "tess@example.com") || !strings.Contains(v, "Bo <bo@x.io>") {
		t.Errorf("authors:\n%s", v)
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "coauthor-Bo <bo@x.io>"})
	if m.gitCommitMsg != "feat: pair\n\nCo-authored-by: Bo <bo@x.io>" {
		t.Errorf("box %q", m.gitCommitMsg)
	}
}

// GITX-8 (B24, Q1): Generate Commit Message with AI sends the staged diff
// and puts the answer (no code fence) in the box; the draft stays in the
// history.
func TestGitxAIGenerateCommitMessage(t *testing.T) {
	m, dir := covGitRepo(t)
	if cmd := m.dispatchPaletteAction("git-ai-commit-msg"); cmd == nil || !toastHas(m, "AI") {
		t.Fatal("no note without an AI provider")
	}
	p := gitxWithAI(m, "```\nfeat: add the b file\n```")
	covWrite(t, dir, "b.txt", "bee\n")
	covGit(t, dir, "add", "b.txt")
	covRefresh(m)
	m.gitCommitMsg = "my draft"
	reply := covMsg[gitAIReplyMsg](t, m.dispatchPaletteAction("git-ai-commit-msg"))
	m.onGitAIReply(reply)
	if m.gitCommitMsg != "feat: add the b file" || !m.gitCommitFocused {
		t.Errorf("box %q", m.gitCommitMsg)
	}
	if !strings.Contains(p.prompt(), "+bee") {
		t.Errorf("the diff was not sent:\n%s", p.prompt())
	}
	gitxKey(m, tea.KeyUp)
	if m.gitCommitMsg != "my draft" {
		t.Errorf("draft lost: %q", m.gitCommitMsg)
	}
}

// GITX-8 (L17): Stash with AI Message asks with the AI's message filled
// in; Enter stashes with it.
func TestGitxAIStashMessage(t *testing.T) {
	m, dir := covGitRepo(t)
	gitxWithAI(m, "wip: rename one")
	covWrite(t, dir, "a.txt", "ONE\ntwo\nthree\n")
	covRefresh(m)
	m.onGitAIReply(covMsg[gitAIReplyMsg](t, m.dispatchPaletteAction("git-ai-stash")))
	if !m.promptOpen || m.promptKind != promptKindGitAIStash || m.prompt.Value() != "wip: rename one" {
		t.Fatalf("prompt %v %q", m.promptOpen, m.prompt.Value())
	}
	m.promptOpen = false
	m.handlePromptSubmit("wip: rename one")
	if got := covGit(t, dir, "stash", "list"); !strings.Contains(got, "wip: rename one") {
		t.Errorf("stash list %q", got)
	}
}

// GITX-8 (Q3, Q2, D18, Q5): the explain and PR helpers fill in the chat
// box with the changes; nothing is sent until the user presses Enter.
func TestGitxAIChatHelpers(t *testing.T) {
	m, dir := covGitRepo(t)
	gitxWithAI(m, "")
	covWrite(t, dir, "a.txt", "ONE\ntwo\nthree\n")
	covRefresh(m)
	m.dispatchPaletteAction("git-ai-explain-changes")
	in := string(m.aiS().input)
	if !strings.Contains(in, "+ONE") || !strings.Contains(in, "Explain") {
		t.Errorf("explain changes %q", in)
	}
	if m.aiS().streaming {
		t.Error("sent without Enter")
	}

	head := covGit(t, dir, "rev-parse", "--short", "HEAD")
	var ids []string
	for _, it := range gitCommitMenuItems() {
		ids = append(ids, it.ID)
	}
	if !strings.Contains(strings.Join(ids, ","), gitMenuExplainAI) {
		t.Fatalf("no Explain with AI in the GRAPH menu: %v", ids)
	}
	m.aiS().input = nil
	m.gitAIExplainCommit(head)
	if in := string(m.aiS().input); !strings.Contains(in, head) || !strings.Contains(in, "first") {
		t.Errorf("explain commit %q", in)
	}

	m.editor.SetMeta(filepath.Join(dir, "a.txt"), false, "text")
	m.cursorLine = 2
	m.aiS().input = nil
	m.dispatchPaletteAction("git-ai-explain-line")
	if in := string(m.aiS().input); !strings.Contains(in, head) {
		t.Errorf("explain line's commit %q", in)
	}

	covGit(t, dir, "checkout", "-q", "-b", "feature")
	covCommit(t, dir, "feat: upper one")
	covRefresh(m)
	m.aiS().input = nil
	m.dispatchPaletteAction("git-ai-pr-text")
	if in := string(m.aiS().input); !strings.Contains(in, "feat: upper one") || !strings.Contains(in, "main") {
		t.Errorf("PR text %q", in)
	}
}
