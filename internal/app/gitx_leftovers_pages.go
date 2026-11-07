package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
)

// GITX-39: the read-only git pages of the small leftovers: the
// contributors (E28), the Git LFS files (M10) and the split-commit
// guide (J12).

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-contributors", Title: "Git: Show Contributors", Hint: "the authors of this branch and their commit counts"},
			{ID: "git-lfs-files", Title: "Git: Show LFS Files", Hint: "the files Git LFS tracks; which ones are only pointers"},
			{ID: "git-split-guide", Title: "Git: How to Split a Commit", Hint: "step by step, with the commands of this app"},
		},
		policies: map[string]aiActionPolicy{
			"git-contributors": aiAuto(),
			"git-lfs-files":    aiAuto(),
			"git-split-guide":  aiAuto(),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-contributors":
				return m.gitxOpenContributors(), true
			case "git-lfs-files":
				return m.gitxOpenLFSFiles(), true
			case "git-split-guide":
				m.gitEnsureLoaded()
				return m.gitxOpenPage("How to Split a Commit", gitxSplitGuidePage(m.gitBranch.Name)), true
			}
			return nil, false
		},
	})
}

// gitxOpenPage opens a read-only text page.
func (m *Model) gitxOpenPage(name, text string) tea.Cmd {
	m.ensureEditorWindowCurrent()
	if err := m.openReadOnlyPage(name, text, "", "log"); err != nil {
		return m.gitxFail("Could not open the page", err)
	}
	return nil
}

func (m *Model) gitxOpenContributors() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	list, err := git.Contributors(cwd)
	if err != nil {
		return m.gitxFail("Could not read the contributors", err)
	}
	return m.gitxOpenPage("Contributors", gitxContributorsPage(list))
}

// gitxContributorsPage is the contributors as text: count, share, author.
func gitxContributorsPage(list []git.Contributor) string {
	total := 0
	for _, c := range list {
		total += c.Commits
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s, %s on this branch (merge commits not counted).\n\n", gitxPlural(len(list), "contributor"), gitxPlural(total, "commit"))
	w := len(fmt.Sprint(total))
	for _, c := range list {
		who := c.Name
		if c.Email != "" {
			who += " <" + c.Email + ">"
		}
		fmt.Fprintf(&b, "%*d  %d%%  %s\n", w, c.Commits, c.Commits*100/max(total, 1), who)
	}
	return b.String()
}

func (m *Model) gitxOpenLFSFiles() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	files, err := git.LFSFiles(cwd)
	if err != nil {
		return m.gitxFail("Could not read the LFS files", err)
	}
	return m.gitxOpenPage("Git LFS Files", gitxLFSPage(files))
}

// gitxLFSPage is the LFS files as text: pointer only or downloaded, the
// real size, the path.
func gitxLFSPage(files []git.LFSFile) string {
	var b strings.Builder
	if len(files) == 0 {
		b.WriteString("No file of this repository is tracked by Git LFS (no filter=lfs rule in .gitattributes matches a tracked file).\n")
		return b.String()
	}
	pointers := 0
	for _, f := range files {
		if f.Pointer {
			pointers++
		}
	}
	fmt.Fprintf(&b, "%s tracked by Git LFS (filter=lfs in .gitattributes); %d only as pointers.\n", gitxPlural(len(files), "file"), pointers)
	if pointers > 0 {
		b.WriteString("A pointer is a small text file in place of the real one: the real file is not downloaded. git lfs pull downloads them.\n")
	}
	b.WriteString("\n")
	for _, f := range files {
		state := "downloaded  "
		if f.Pointer {
			state = "pointer only"
		}
		fmt.Fprintf(&b, "%s  %9s  %s\n", state, humanBytes(f.Size), f.Path)
	}
	return b.String()
}

// gitxSplitGuidePage is the split-commit guide; branch is the current
// branch.
func gitxSplitGuidePage(branch string) string {
	if branch == "" {
		branch = "this branch"
	}
	return `How to split a commit into smaller ones (on ` + branch + `)

Splitting rewrites the commit: do it before you push, or push with
"Git: Force Push (with Lease)…" afterwards.

A. The last commit
  1. Run "Git: Uncommit Last Commit (Keep Changes)". The commit goes
     away; its changes stay staged.
  2. In Source Control, unstage everything that is not part of the first
     new commit, or mark its files with Space and run
     "Git: Commit Marked Files Only…" (c with rows marked).
  3. Commit the first part, then stage and commit the next part, until
     nothing is left.

B. An older commit
  1. Run "Git: Interactive Rebase…" from a commit before the one to
     split, and set the commit to split to edit.
  2. The rebase stops after that commit. Run
     "Git: Uncommit Last Commit (Keep Changes)": its changes are staged.
  3. Commit the parts one by one, as in A.2 and A.3.
  4. Run "Git: Rebase in Progress (Continue / Skip / Abort)…" and choose
     Continue. Abort there brings back the branch as it was.

"Git: Plan Commits for My Changes with AI" can suggest how to group the
changes into commits before you start.
`
}
