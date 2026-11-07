package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Merge view tools (GITX-1 I10, GITX-34, GITX-28): take one side for the
// whole file, settle the simple conflicts, ask the AI for one conflict
// (its answer waits on a Cancel-first card), and AI reviews of the
// changes or the branch (the request waits in the chat box).

const (
	gitxAITimeout   = 90 * time.Second
	gitxAIMaxTokens = 2000
	gitxReviewMax   = 32 << 10 // bytes of diff put in the chat box
)

const aiMergeOnly = "resolves the user's merge conflict in the merge view; user only"

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-merge-take-all-ours", Title: "Git: Merge View: Take Ours for the Whole File", Hint: "O"},
			{ID: "git-merge-take-all-theirs", Title: "Git: Merge View: Take Theirs for the Whole File", Hint: "T"},
			{ID: "git-merge-resolve-simple", Title: "Git: Merge View: Resolve Simple Conflicts", Hint: "s"},
			{ID: "git-merge-ai-resolve", Title: "Git: Merge View: AI Resolve Conflict…", Hint: "i"},
			{ID: "git-ai-review-changes", Title: "Git: AI Review My Changes"},
			{ID: "git-ai-review-branch", Title: "Git: AI Review Branch (Pull Request)"},
		},
		policies: map[string]aiActionPolicy{
			"git-merge-take-all-ours":   aiNever(aiMergeOnly),
			"git-merge-take-all-theirs": aiNever(aiMergeOnly),
			"git-merge-resolve-simple":  aiNever(aiMergeOnly),
			"git-merge-ai-resolve":      aiNever(aiMergeOnly),
			"git-ai-review-changes":     aiNever("fills the user's chat box; the user reads it and sends it"),
			"git-ai-review-branch":      aiNever("fills the user's chat box; the user reads it and sends it"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-ai-review-changes":
				return m.gitxAIReview(false), true
			case "git-ai-review-branch":
				return m.gitxAIReview(true), true
			}
			if !strings.HasPrefix(id, "git-merge-") {
				return nil, false
			}
			switch id {
			case "git-merge-take-all-ours", "git-merge-take-all-theirs", "git-merge-resolve-simple", "git-merge-ai-resolve":
			default:
				return nil, false
			}
			if !m.mergeViewShown() {
				var c tea.Cmd
				m.toast, c = m.toast.PushDetail(toast.Info, "The merge view is not open", "Open it first: Git: Open Merge View")
				return c, true
			}
			return m.mergeToolKey(map[string]string{
				"git-merge-take-all-ours": "O", "git-merge-take-all-theirs": "T",
				"git-merge-resolve-simple": "s", "git-merge-ai-resolve": "i"}[id]), true
		},
	})
}

// mergeToolKey runs a merge view key of this file (O, T, s, i).
func (m *Model) mergeToolKey(k string) tea.Cmd {
	switch k {
	case "O":
		return m.mergeTakeAll(git.AcceptOurs)
	case "T":
		return m.mergeTakeAll(git.AcceptTheirs)
	case "s":
		return m.mergeResolveSimple()
	case "i":
		return m.mergeAIResolve()
	}
	return nil
}

// mergeSetResult puts lines in the result as one undo step (u takes it
// back) and says what happened.
func (m *Model) mergeSetResult(lines []string, what string) tea.Cmd {
	if !m.mergeEditor() {
		return m.gitxInfo("Open the file in the editor first")
	}
	var out string
	if err := m.nvim.ExecLuaResult(mergeTakeLua, &out, m.merge.abs, 0, -1, lines); err != nil {
		return m.gitxFail("Merge", err)
	}
	m.mergeSync()
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, what, "u undoes it. Mark as Resolved (r) stages the file.")
	return c
}

// mergeTakeAll takes one side for every conflict of the file.
func (m *Model) mergeTakeAll(r git.Resolution) tea.Cmd {
	m.mergeSync()
	lines := m.merge.view.ResolveAll(r)
	if lines == nil {
		return m.gitxInfo("No conflicts left")
	}
	side := "Ours"
	if r == git.AcceptTheirs {
		side = "Theirs"
	}
	return m.mergeSetResult(lines, "Took "+side+" for "+conflictWord(len(m.merge.view.Blocks())))
}

// mergeResolveSimple settles the conflicts where one side kept the base
// or both made the same change (spaces and tabs ignored).
func (m *Model) mergeResolveSimple() tea.Cmd {
	m.mergeSync()
	lines, n := m.merge.view.ResolveSimple(true)
	if n == 0 {
		return m.gitxInfo("No simple conflicts: both sides changed each one")
	}
	return m.mergeSetResult(lines, "Resolved "+conflictWord(n))
}

// mergeAIResolve asks the AI for the current conflict. Its answer shows
// on a Cancel-first card; Use AI Result puts it in the result (u undoes).
func (m *Model) mergeAIResolve() tea.Cmd {
	m.mergeSync()
	if !m.aiConfigured() {
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Set up AI first", "Run AI: Configure Provider…, then try again")
		return c
	}
	v := m.merge.view
	b, ok := v.CurrentBlock()
	if !ok {
		return m.gitxInfo("No conflicts left")
	}
	tk, _ := v.Take(git.AcceptBase)
	c := b.Conflict
	msgs := gitxConflictPrompt(m.merge.rel, c, tk.Lines)
	s := m.aiS()
	prov, model := s.provider, s.cfg.InlineModel
	start, end := c.Start, c.End
	var tc tea.Cmd
	m.toast, tc = m.toast.Push(toast.Info, "Asking the AI…")
	return tea.Batch(tc, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), gitxAITimeout)
		defer cancel()
		text, last := ai.Collect(prov.Stream(ctx, ai.Request{Messages: msgs, Model: model, MaxTokens: gitxAIMaxTokens}))
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd { return m.onMergeAIAnswer(start, end, text, last.Err) }}
	})
}

// gitxConflictPrompt asks for one conflict's resolved lines.
func gitxConflictPrompt(path string, c git.ConflictBlock, base []string) []ai.Message {
	sys := "You resolve one git merge conflict. Keep what both sides meant to do. " +
		"Answer with the resolved lines only: no conflict markers, no code fence, no comments about the answer."
	var b strings.Builder
	b.WriteString("File: " + path + "\n\n")
	sec := func(title string, lines []string) {
		b.WriteString(title + ":\n```\n" + strings.Join(lines, "\n") + "\n```\n\n")
	}
	sec("The common base", base)
	sec("Ours ("+c.OursLabel+")", c.Ours)
	sec("Theirs ("+c.TheirsLabel+")", c.Theirs)
	return []ai.Message{{Role: ai.RoleSystem, Content: sys}, {Role: ai.RoleUser, Content: b.String()}}
}

// gitxCleanLines drops a code fence around an answer.
func gitxCleanLines(s string) []string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		} else {
			s = ""
		}
		s = strings.TrimSuffix(strings.TrimRight(s, "\n "), "```")
	}
	s = strings.TrimRight(s, "\n ")
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "\n")
}

// onMergeAIAnswer shows the answer on a Cancel-first card.
func (m *Model) onMergeAIAnswer(start, end int, text string, err error) tea.Cmd {
	if err != nil {
		return m.gitxFail("The AI did not answer", err)
	}
	lines := gitxCleanLines(text)
	if git.HasConflicts(strings.Join(lines, "\n")) || len(lines) == 0 && strings.TrimSpace(text) == "" {
		return m.gitxInfo("The AI gave no usable answer")
	}
	preview := lines
	if len(preview) > 12 {
		preview = append(append([]string{}, preview[:12]...), "…")
	}
	m.gitxCard("Use the AI's resolution?", "Check it first: it replaces the conflict, u undoes it.\n\n"+strings.Join(preview, "\n"),
		func(m *Model, id string) tea.Cmd {
			if id != "use-ai" || !m.mergeViewShown() {
				return nil
			}
			m.mergeSync()
			if b, ok := m.merge.view.CurrentBlock(); !ok || b.Conflict.Start != start || b.Conflict.End != end {
				return m.gitxInfo("The conflict changed: ask the AI again")
			}
			if !m.mergeEditor() {
				return m.gitxInfo("Open the file in the editor first")
			}
			var out string
			if err := m.nvim.ExecLuaResult(mergeTakeLua, &out, m.merge.abs, start, end+1, lines); err != nil {
				return m.gitxFail("Merge", err)
			}
			m.mergeSync()
			return nil
		}, confirm.Button{ID: "use-ai", Title: "Use AI Result"})
	return nil
}

// gitxAIReview puts a review request with the diff (the changes, or the
// branch since it left its base) in the chat box, unsent.
func (m *Model) gitxAIReview(branch bool) tea.Cmd {
	d, c, ok := m.gitxReviewDiff(branch)
	if !ok {
		return c
	}
	ask := "Review my uncommitted changes: bugs, risks, missing tests and unclear names. List the findings by file, most important first."
	if branch {
		ask = "Review this branch as a pull request into " + d.base + ": bugs, risks, missing tests and unclear names. List the findings by file, most important first."
	}
	diff := d.diff
	if d.cut {
		diff += "\n[cut here: the rest is longer]"
	}
	return m.gitxFillChat(ask + "\n\n```diff\n" + strings.TrimRight(diff, "\n") + "\n```")
}

// gitxDiff is the diff a review covers.
type gitxDiff struct {
	root, base, diff string
	cut              bool
}

// gitxReviewDiff reads the diff of the changes or of the branch; ok is
// false when there is nothing to review (c says why).
func (m *Model) gitxReviewDiff(branch bool) (d gitxDiff, c tea.Cmd, ok bool) {
	if !m.gitEnsureLoaded() {
		return d, nil, false
	}
	if !m.aiConfigured() {
		m.toast, c = m.toast.PushDetail(toast.Warn, "Set up AI first", "Run AI: Configure Provider…, then try again")
		return d, c, false
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return d, nil, false
	}
	d.root = cwd
	if branch {
		base, err := git.DefaultBase(cwd)
		if err != nil {
			return d, m.gitxFail("No base branch", err), false
		}
		d.base = base
		d.diff, d.cut, err = git.BranchDiff(cwd, base, gitxReviewMax)
		if err != nil {
			return d, m.gitxFail("Could not read the branch", err), false
		}
	} else {
		d.diff, d.cut, err = git.WorkDiff(cwd, gitxReviewMax)
		if err != nil {
			return d, m.gitxFail("Could not read the changes", err), false
		}
	}
	if strings.TrimSpace(d.diff) == "" {
		return d, m.gitxInfo("No changes to review"), false
	}
	return d, nil, true
}

// gitxFillChat puts text in the chat box of a fresh chat, unsent.
func (m *Model) gitxFillChat(text string) tea.Cmd {
	s := m.aiS()
	if s.streaming || s.chatCancel != nil {
		return m.gitxInfo("The AI is still answering: try again when it is done")
	}
	var open tea.Cmd
	if len(s.conv.Messages) > 0 {
		open = m.newAIChat() // the old chat stays in Chat History
	} else {
		open = m.openAIPanel(true)
	}
	s.input = []rune(text)
	s.caret = len(s.input)
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "Check the request in the chat and press Enter to send it")
	return tea.Batch(open, c)
}
