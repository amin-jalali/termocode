package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Bisect (GITX-19) and branch compare (GITX-24). Bisect is a picker that
// comes back after each answer: Good, Bad, Skip, Run a Task to Decide…,
// End Bisect. Compare opens the commit view on a range, or a read-only
// diff card for the working tree.

const gitxCompareMaxFiles = 200 // files the working-tree card reads

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-bisect", Title: "Git: Bisect (Find the Commit That Broke It)…"},
			{ID: "git-compare-branches", Title: "Git: Compare Branches…"},
		},
		policies: map[string]aiActionPolicy{
			"git-bisect":           aiNever("checks out commits one by one and asks the user which work; user only"),
			"git-compare-branches": aiNever(aiNoPicker),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-bisect":
				return m.openBisect(), true
			case "git-compare-branches":
				return m.openCompareBranches(), true
			}
			return nil, false
		},
	})
}

// ── bisect ──────────────────────────────────────────────────────────────

// openBisect shows the step picker of a running bisect, or starts one:
// HEAD is bad, the user picks a commit that was still good.
func (m *Model) openBisect() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	if st := git.BisectStatus(cwd); st.Active {
		return m.openBisectSteps(st)
	}
	var items []picker.Item
	for i, c := range m.gitCommits {
		if i == 0 || c.IsHead {
			continue // HEAD is the bad end
		}
		items = append(items, picker.Item{ID: "good-" + c.FullHash, Title: c.Hash + "  " + c.Subject, Hint: c.Author})
	}
	if len(items) == 0 {
		return m.gitxInfo("Bisect needs at least two commits")
	}
	m.gitxPick(" Bisect: which commit was still good? ", items, func(m *Model, id string) tea.Cmd {
		good, ok := strings.CutPrefix(id, "good-")
		if !ok {
			return nil
		}
		st, err := git.BisectStart(cwd, "HEAD", good)
		if err != nil {
			return m.gitxFail("Bisect failed", err)
		}
		return m.onBisectState(st)
	})
	return nil
}

// onBisectState shows the result card when git found the commit, else
// the step picker.
func (m *Model) onBisectState(st git.BisectState) tea.Cmd {
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	refresh := fetchGitCmd()
	if st.Done {
		return tea.Batch(refresh, m.openBisectResult(st))
	}
	return tea.Batch(refresh, m.openBisectSteps(st))
}

func (m *Model) openBisectSteps(st git.BisectState) tea.Cmd {
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	left := ""
	if st.StepsLeft >= 0 {
		left = fmt.Sprintf(" · ~%d steps left", st.StepsLeft)
	}
	title := " Bisect · testing " + st.Current + left + " "
	m.gitxPick(title, []picker.Item{
		{ID: "bisect-good", Title: "Good: this commit works", Hint: st.CurrentSubject},
		{ID: "bisect-bad", Title: "Bad: the problem is here", Hint: "git bisect bad"},
		{ID: "bisect-skip", Title: "Skip: can't test this one", Hint: "git bisect skip"},
		{ID: "bisect-run", Title: "Run a Task to Decide…", Hint: "git bisect run: exit 0 good, else bad"},
		{ID: "bisect-reset", Title: "End Bisect", Hint: "back to where you started"},
	}, func(m *Model, id string) tea.Cmd {
		switch id {
		case "bisect-good", "bisect-bad", "bisect-skip":
			st, err := git.BisectMark(cwd, strings.TrimPrefix(id, "bisect-"))
			if err != nil {
				return m.gitxFail("Bisect failed", err)
			}
			return m.onBisectState(st)
		case "bisect-run":
			return m.openBisectTaskPicker()
		case "bisect-reset":
			return m.gitxBisectReset()
		}
		return nil
	})
	return nil
}

func (m *Model) gitxBisectReset() tea.Cmd {
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	if err := git.BisectReset(cwd); err != nil {
		return m.gitxFail("End Bisect failed", err)
	}
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	return tea.Batch(fetchGitCmd(), m.gitxInfo("Bisect ended: back where you started"))
}

// openBisectTaskPicker picks the task that decides each commit.
func (m *Model) openBisectTaskPicker() tea.Cmd {
	list, err := m.allTasks()
	errCmd := m.toastTasksError(err)
	if len(list) == 0 {
		return tea.Batch(errCmd, m.gitxInfo("No tasks: add one with Tasks: Configure Tasks"))
	}
	v := m.taskVars()
	items := make([]picker.Item, 0, len(list))
	for _, t := range list {
		items = append(items, picker.Item{ID: "task-" + t.Label, Title: t.Label, Hint: t.CommandLine(v)})
	}
	m.gitxPick(" Bisect: which task decides? (exit 0 = good) ", items, func(m *Model, id string) tea.Cmd {
		label, _ := strings.CutPrefix(id, "task-")
		for _, t := range list {
			if t.Label == label {
				line, dir := t.CommandLine(v), t.Dir(v)
				m.gitxCard("Run "+t.Label+" on each commit?", "git checks out commits one by one and runs:\n  "+line+
					"\nExit 0 means good, 125 skip, anything else bad. It stops at the first bad commit.",
					func(m *Model, id string) tea.Cmd {
						if id != "run" {
							return nil
						}
						cwd, err := m.gitRootDir()
						if err != nil {
							return nil
						}
						return tea.Batch(m.gitxInfo("Bisect is running "+t.Label+"…"), func() tea.Msg {
							st, err := git.BisectRun(cwd, dir, line)
							return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
								if err != nil {
									return tea.Batch(fetchGitCmd(), m.gitxFail("Bisect run failed", err))
								}
								return m.onBisectState(st)
							}}
						})
					}, confirm.Button{ID: "run", Title: "Run"})
				return nil
			}
		}
		return nil
	})
	return tea.Batch(errCmd)
}

func (m *Model) openBisectResult(st git.BisectState) tea.Cmd {
	short := st.FirstBad[:min(7, len(st.FirstBad))]
	m.gitxCard("First bad commit: "+short, short+"  "+st.FirstBadSubject+
		"\n\nEnd Bisect goes back to where you started. Stay Here keeps the bisect open (Bisect… shows it again).",
		func(m *Model, id string) tea.Cmd {
			switch id {
			case "open":
				return m.gitOpenCommitViewCmd(st.FirstBad)
			case "reset":
				return m.gitxBisectReset()
			}
			return nil
		}, confirm.Button{ID: "open", Title: "Open Commit"}, confirm.Button{ID: "reset", Title: "End Bisect"})
	return nil
}

// ── compare ─────────────────────────────────────────────────────────────

func (m *Model) openCompareBranches() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	refs, err := git.RefNames(cwd)
	if err != nil {
		return m.gitxFail("Compare failed", err)
	}
	cur := m.gitBranch.Name
	var items []picker.Item
	for _, r := range refs {
		if r != cur {
			items = append(items, picker.Item{ID: "ref-" + r, Title: r})
		}
	}
	if len(items) == 0 {
		return m.gitxInfo("There is no other branch or tag")
	}
	m.gitxPick(" Compare "+cur+" with… ", items, func(m *Model, id string) tea.Cmd {
		other, ok := strings.CutPrefix(id, "ref-")
		if !ok {
			return nil
		}
		return m.openCompareModes(cwd, cur, other)
	})
	return nil
}

func (m *Model) openCompareModes(cwd, cur, other string) tea.Cmd {
	here := cur
	if strings.HasPrefix(cur, "(detached ") {
		here = "HEAD"
	}
	m.gitxPick(" Compare "+here+" and "+other+" ", []picker.Item{
		{ID: "mode-theirs", Title: "Changes on " + other + " since it split from " + here},
		{ID: "mode-ours", Title: "Changes on " + here + " since it split from " + other},
		{ID: "mode-tips", Title: here + " → " + other + " (every difference)"},
		{ID: "mode-worktree", Title: "Working tree → " + other},
	}, func(m *Model, id string) tea.Cmd {
		resolve := func(ref string) (string, bool) {
			full, err := git.CommitOf(cwd, ref)
			return full, err == nil
		}
		a, okA := resolve("HEAD")
		b, okB := resolve(other)
		if !okA || !okB {
			return m.gitxInfo("No commit " + other)
		}
		short := func(h string) string { return h[:min(7, len(h))] }
		switch id {
		case "mode-theirs", "mode-ours":
			mb, err := git.MergeBase(cwd, a, b)
			if err != nil {
				return m.gitxFail("Compare failed", err)
			}
			to, label := b, other
			if id == "mode-ours" {
				to, label = a, here
			}
			return m.gitOpenCompareCmd(commitRange{a: mb, b: to, sa: short(mb), sb: label})
		case "mode-tips":
			return m.gitOpenCompareCmd(commitRange{a: a, b: b, sa: here, sb: other})
		case "mode-worktree":
			return m.gitxWorktreeCompareCmd(cwd, other)
		}
		return nil
	})
	return nil
}

// gitxWorktreeCompareCmd reads ref's side and the working tree's side of
// every file that differs, off the UI loop, and opens a read-only diff
// card.
func (m *Model) gitxWorktreeCompareCmd(cwd, ref string) tea.Cmd {
	return func() tea.Msg {
		paths, err := git.WorkTreeDiffFiles(cwd, ref)
		var files []diffview.File
		if err == nil {
			for i, p := range paths {
				if i == gitxCompareMaxFiles {
					break
				}
				old, _ := git.ShowFileAtRevision(cwd, ref, p)
				cur, _ := os.ReadFile(filepath.Join(cwd, p))
				files = append(files, diffview.File{Name: p, Old: old, New: string(cur)})
			}
		}
		return gitxDoneMsg{apply: func(m *Model) tea.Cmd {
			switch {
			case err != nil:
				return m.gitxFail("Compare failed", err)
			case len(files) == 0:
				return m.gitxInfo("The working tree matches " + ref)
			}
			s := m.aiS()
			if s.review != nil && m.aiReviewPending() > 0 {
				return m.aiInfo("Finish the AI review first (AI: Review Pending Changes)")
			}
			title := "Working tree → " + ref
			if len(paths) > len(files) {
				title += fmt.Sprintf(" (first %d of %d files)", len(files), len(paths))
			}
			s.review = &aiReview{card: diffview.New(title, files, false)}
			return m.showAIReview()
		}}
	}
}
