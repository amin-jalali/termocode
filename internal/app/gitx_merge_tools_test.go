package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
)

// GITX-1 (I10), GITX-34: the merge view takes O (all ours), T (all
// theirs) and s (settle the simple conflicts); each has a palette row
// with an AI policy.
func TestGitxMergeToolKeys(t *testing.T) {
	m := mergeTestModel(t, 80, 24)
	m.merge.view.SetFocused(true)
	m.focus = FocusEditor
	for _, k := range []string{"O", "T", "s", "i"} {
		nm, _, ok := m.routeToMergeView(mergeKeyMsg(k))
		m = nm
		if !ok {
			t.Errorf("%s was not taken", k)
		}
	}
	for _, id := range []string{"git-merge-take-all-ours", "git-merge-take-all-theirs", "git-merge-resolve-simple",
		"git-merge-ai-resolve", "git-ai-review-changes", "git-ai-review-branch"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s: no AI policy", id)
		}
	}
	mm := &m
	mm.closeMergeView()
	mm.dispatchPaletteAction("git-merge-take-all-ours")
	if !toastHas(mm, "merge view is not open") {
		t.Error("all ours without a merge view")
	}
}

// GITX-28 (I12): AI Resolve Conflict sends the current conflict (base,
// ours, theirs) to the AI and shows its answer on a Cancel-first card;
// nothing changes until Use AI Result.
func TestGitxMergeAIResolve(t *testing.T) {
	m := mergeTestModel(t, 100, 30)
	p := &recordingAIProvider{replies: []ai.Chunk{{Delta: "```\nours and theirs\n```"}}}
	m.aiS().provider = p
	cmd := m.dispatchPaletteAction("git-merge-ai-resolve")
	done := gitxRunDone(t, &m, cmd)
	if !done {
		t.Fatal("no AI answer came back")
	}
	if len(p.reqs) != 1 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	sent := p.reqs[0].Messages[len(p.reqs[0].Messages)-1].Content
	for _, s := range []string{"ours", "theirs", "base", "f.txt"} {
		if !strings.Contains(sent, s) {
			t.Errorf("the prompt misses %q:\n%s", s, sent)
		}
	}
	if !m.confirmOpen || m.confirm.SafeID() != "cancel" || gitxCardIDList(&m) != "cancel,use-ai" {
		t.Fatalf("card %q", gitxCardIDList(&m))
	}
	if !strings.Contains(m.confirm.View(), "ours and theirs") {
		t.Errorf("the card does not show the answer:\n%s", m.confirm.View())
	}
	// No AI set up: a toast says how.
	m2 := mergeTestModel(t, 100, 30)
	m2.aiS().provider = nil
	m2.dispatchPaletteAction("git-merge-ai-resolve")
	if !toastHas(&m2, "Set up AI first") {
		t.Error("no setup hint")
	}
}

// GITX-28 (Q6, Q7): AI Review My Changes and AI Review Branch put a review
// request with the diff in the chat box, unsent.
func TestGitxAIReview(t *testing.T) {
	m, dir := covGitRepo(t)
	m.aiS().provider = &recordingAIProvider{}
	covWrite(t, dir, "a.txt", "one\nTWO\nthree\n")
	covRefresh(m)
	m.dispatchPaletteAction("git-ai-review-changes")
	in := string(m.aiS().input)
	if !strings.Contains(in, "Review") || !strings.Contains(in, "+TWO") {
		t.Fatalf("chat box %q", in)
	}
	covGit(t, dir, "checkout", "-q", "-b", "feature")
	covCommit(t, dir, "feat: two")
	covRefresh(m)
	m.aiS().input = nil
	m.dispatchPaletteAction("git-ai-review-branch")
	in = string(m.aiS().input)
	if !strings.Contains(in, "main") || !strings.Contains(in, "+TWO") {
		t.Fatalf("branch review %q", in)
	}
}

// gitxRunDone runs cmd (and batches) until a gitxDoneMsg comes, and
// applies it.
func gitxRunDone(t *testing.T, m *Model, cmd tea.Cmd) bool {
	t.Helper()
	var walk func(c tea.Cmd) bool
	walk = func(c tea.Cmd) bool {
		if c == nil {
			return false
		}
		switch msg := c().(type) {
		case gitxDoneMsg:
			msg.apply(m)
			return true
		case tea.BatchMsg:
			for _, sub := range msg {
				if walk(sub) {
					return true
				}
			}
		}
		return false
	}
	return walk(cmd)
}
