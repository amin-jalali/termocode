package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Cherry-pick and revert options (GITX-16), rebase onto a branch and
// one-step history edits from the GRAPH (GITX-17), patches (GITX-25).
// Everything that changes history or files asks on a Cancel-first card.

// gitxRebaser runs the rebases (tests swap it: the test binary is no
// rebase helper).
type gitxRebaser struct {
	onto       func(dir, onto string, keepMerges bool, opt git.RebaseOptions) (git.RebaseOutcome, error)
	reword     func(dir, full, message string, opt git.RebaseOptions) (git.RebaseOutcome, error)
	drop       func(dir, full string, opt git.RebaseOptions) (git.RebaseOutcome, error)
	squash     func(dir, full string, opt git.RebaseOptions) (git.RebaseOutcome, error)
	autosquash func(dir string, opt git.RebaseOptions) (git.RebaseOutcome, error)
}

var gitxRebaseRuns = gitxRebaser{
	onto: git.RebaseOnto, reword: git.RewordCommit, drop: git.DropCommit,
	squash: git.SquashIntoPrevious, autosquash: git.Autosquash,
}

// gitxReadClipboard reads the clipboard (tests swap it).
var gitxReadClipboard = readClipboardText

const (
	gitxMenuPickFile    = "gitx-pick-file"
	gitxMenuRestoreFile = "gitx-restore-file"
	gitxMenuRevertMerge = "gitx-revert-merge"
	gitxMenuReword      = "gitx-reword"
	gitxMenuSquash      = "gitx-squash"
	gitxMenuDrop        = "gitx-drop"
	gitxMenuCopyPatch   = "gitx-copy-patch"
)

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "git-cherry-pick-branch", Title: "Git: Cherry-pick from Branch…"},
			{ID: "git-rebase-onto", Title: "Git: Rebase Current Branch onto…"},
			{ID: "git-autosquash", Title: "Git: Autosquash fixup! Commits…"},
			{ID: "git-patch-save", Title: "Git: Save Changes as Patch…"},
			{ID: "git-patch-apply-file", Title: "Git: Apply Patch from File…"},
			{ID: "git-patch-apply-clip", Title: "Git: Apply Patch from Clipboard…"},
		},
		policies: map[string]aiActionPolicy{
			"git-cherry-pick-branch": aiNever(aiNoPicker),
			"git-rebase-onto":        aiNever(aiNoPicker),
			"git-autosquash":         aiNever("rewrites the branch history; only the user starts a rebase"),
			"git-patch-save":         aiNever(aiNoPicker),
			"git-patch-apply-file":   aiNever(aiNoPicker),
			"git-patch-apply-clip":   aiNever("applies text from the user's clipboard to the files; user only"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "git-cherry-pick-branch":
				return m.openBranchPickerFor(" Cherry-pick from which branch? ", m.openCherryPickBranchCard), true
			case "git-rebase-onto":
				return m.openBranchPickerFor(" Rebase the current branch onto… ", m.openRebaseOntoCard), true
			case "git-autosquash":
				return m.openAutosquashCard(), true
			case "git-patch-save":
				return m.openSavePatchPrompt(), true
			case "git-patch-apply-file":
				return m.openApplyPatchFilePrompt(), true
			case "git-patch-apply-clip":
				return m.gitxCheckPatch("the clipboard", gitxReadClipboard()), true
			}
			return nil, false
		},
		menu: []menu.Item{
			{Sep: true},
			{ID: gitxMenuReword, Title: "Edit Commit Message…", Icon: "✎"},
			{ID: gitxMenuSquash, Title: "Squash into Previous…", Icon: "⇣"},
			{ID: gitxMenuDrop, Title: "Drop Commit…", Icon: "✗"},
			{ID: gitxMenuRevertMerge, Title: "Revert Merge…", Icon: "↩"},
			{ID: gitxMenuPickFile, Title: "Cherry-pick a File from Here…", Icon: "⇢"},
			{ID: gitxMenuRestoreFile, Title: "Restore a File from Here…", Icon: "⟲"},
			{ID: gitxMenuCopyPatch, Title: "Copy as Patch", Icon: "⎘"},
		},
		menuSelect: func(m *Model, id string) (tea.Cmd, bool) {
			if !strings.HasPrefix(id, "gitx-") {
				return nil, false
			}
			c, ok := m.gitxRowCommit()
			if !ok {
				return nil, true
			}
			return m.gitxRowAction(id, c), true
		},
		key: func(m *Model, k tea.KeyMsg) (tea.Cmd, bool) {
			if k.Type != tea.KeyRunes || len(k.Runes) != 1 {
				return nil, false
			}
			id := map[rune]string{'E': gitxMenuReword, 'S': gitxMenuSquash, 'D': gitxMenuDrop}[k.Runes[0]]
			c, ok := m.gitxRowCommit()
			if id == "" || !ok {
				return nil, false
			}
			return m.gitxRowAction(id, c), true
		},
	})
}

// gitxRowCommit is the GRAPH commit under the cursor.
func (m *Model) gitxRowCommit() (git.GraphCommit, bool) {
	if cur, ok := m.gitCurrentRow(); ok && cur.kind == gitRowCommit && cur.ci < len(m.gitCommits) {
		return m.gitCommits[cur.ci], true
	}
	return git.GraphCommit{}, false
}

func (m *Model) gitxRowAction(id string, c git.GraphCommit) tea.Cmd {
	switch id {
	case gitxMenuReword:
		return m.openRewordPrompts(c)
	case gitxMenuSquash:
		return m.openRowRebaseCard(c, "squash", "Squash "+c.Hash+" into the commit before it?",
			"Its changes join the commit before it; both messages are kept.", "Squash")
	case gitxMenuDrop:
		return m.openRowRebaseCard(c, "drop", "Drop commit "+c.Hash+"?",
			"“"+c.Subject+"” and its changes leave the branch; newer commits stay.", "Drop")
	case gitxMenuRevertMerge:
		return m.openRevertMergePicker(c)
	case gitxMenuPickFile:
		return m.gitxPickFileFrom(c.FullHash)
	case gitxMenuRestoreFile:
		return m.gitxRestoreFileFrom(c.FullHash)
	case gitxMenuCopyPatch:
		cwd, err := m.rootDir()
		if err != nil {
			return nil
		}
		p, err := git.CommitPatch(cwd, c.FullHash)
		if err != nil {
			return m.gitxFail("Copy failed", err)
		}
		m.copyToClipboard(p)
		return m.gitxInfo("Copied " + c.Hash + " as a patch")
	}
	return nil
}

// openBranchPickerFor picks a local branch other than the current one.
func (m *Model) openBranchPickerFor(title string, then func(branch string) tea.Cmd) tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	bs, err := git.Branches(cwd)
	if err != nil {
		return m.gitxFail("Branches failed", err)
	}
	var items []picker.Item
	for _, b := range bs {
		if b != m.gitBranch.Name {
			items = append(items, picker.Item{ID: "branch-" + b, Title: b})
		}
	}
	if len(items) == 0 {
		return m.gitxInfo("There is no other branch")
	}
	m.gitxPick(title, items, func(m *Model, id string) tea.Cmd {
		if b, ok := strings.CutPrefix(id, "branch-"); ok {
			return then(b)
		}
		return nil
	})
	return nil
}

// gitxCommitLines lists commits for a card (at most 10).
func gitxCommitLines(es []git.LogEntry) string {
	var b strings.Builder
	for i, e := range es {
		if i == 10 {
			fmt.Fprintf(&b, "… and %d more\n", len(es)-10)
			break
		}
		fmt.Fprintf(&b, "%s  %s\n", e.Hash, e.Subject)
	}
	return b.String()
}

func gitxCount(n int) string {
	if n == 1 {
		return "1 commit"
	}
	return fmt.Sprintf("%d commits", n)
}

func (m *Model) gitxPickDone(verb string, conflicts []string, err error, ok string) tea.Cmd {
	var c tea.Cmd
	switch {
	case len(conflicts) > 0:
		m.toast, c = m.toast.PushDetail(toast.Warn, verb+" stopped: "+conflictWord(len(conflicts)),
			"Fix them in CONFLICTS and commit. To give up: git "+strings.ToLower(verb)+" --abort")
	case err != nil:
		m.toast, c = m.toast.PushDetail(toast.Errr, verb+" failed", err.Error())
	default:
		m.toast, c = m.toast.PushDetail(toast.Info, ok, "Undo: u / Git: Undo Last Git Action")
	}
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	return tea.Batch(c, fetchGitCmd())
}

// ── GITX-16 ─────────────────────────────────────────────────────────────

func (m *Model) openCherryPickBranchCard(branch string) tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	es, err := git.BranchOnlyCommits(cwd, branch, 200)
	if err != nil {
		return m.gitxFail("Cherry-pick failed", err)
	}
	if len(es) == 0 {
		return m.gitxInfo("Nothing to cherry-pick: " + m.gitBranch.Name + " has every change of " + branch)
	}
	text := gitxCommitLines(es) + "\nThey are applied oldest first on top of " + m.gitBranch.Name + "."
	m.gitxCard("Cherry-pick "+gitxCount(len(es))+" from "+branch+"?", text, func(m *Model, id string) tea.Cmd {
		if id != "pick" {
			return nil
		}
		hashes := make([]string, len(es))
		for i, e := range es {
			hashes[i] = e.Hash
		}
		conflicts, err := git.CherryPickMany(cwd, hashes)
		return m.gitxPickDone("Cherry-pick", conflicts, err, "Cherry-picked "+gitxCount(len(es))+" from "+branch)
	}, confirm.Button{ID: "pick", Title: "Cherry-pick All"})
	m.confirm = m.confirm.WithWideText()
	return nil
}

func (m *Model) openRevertMergePicker(c git.GraphCommit) tea.Cmd {
	if !c.IsMerge {
		return m.gitxInfo(c.Hash + " is not a merge: use Revert in the commit view")
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	ps, err := git.MergeParents(cwd, c.FullHash)
	if err != nil {
		return m.gitxFail("Revert failed", err)
	}
	items := make([]picker.Item, len(ps))
	for i, p := range ps {
		hint := "the merged-in side"
		if i == 0 {
			hint = "the branch it was merged into (usual)"
		}
		items[i] = picker.Item{ID: fmt.Sprintf("parent-%d", i+1), Title: fmt.Sprintf("Keep parent %d: %s  %s", i+1, p.Hash, p.Subject), Hint: hint}
	}
	m.gitxPick(" Revert merge "+c.Hash+": keep which parent? ", items, func(m *Model, id string) tea.Cmd {
		var n int
		if _, err := fmt.Sscanf(id, "parent-%d", &n); err != nil {
			return nil
		}
		m.gitxCard("Revert merge "+c.Hash+"?", fmt.Sprintf("A new commit undoes what the merge brought in, keeping parent %d. Nothing is lost: the merge stays in the history.", n),
			func(m *Model, id string) tea.Cmd {
				if id != "revert" {
					return nil
				}
				conflicts, err := git.RevertMerge(cwd, c.FullHash, n)
				return m.gitxPickDone("Revert", conflicts, err, "Reverted merge "+c.Hash)
			}, confirm.Button{ID: "revert", Title: "Revert"})
		return nil
	})
	return nil
}

// gitxCommitFilePicker picks one file the commit changed.
func (m *Model) gitxCommitFilePicker(full, title string, then func(m *Model, path string) tea.Cmd) tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	info, err := git.CommitFiles(cwd, full)
	if err != nil {
		return m.gitxFail("Files failed", err)
	}
	var items []picker.Item
	for _, f := range info.Files {
		if f.Status != 'D' {
			items = append(items, picker.Item{ID: "file-" + f.Path, Title: f.Path, Hint: string(f.Status)})
		}
	}
	if len(items) == 0 {
		return m.gitxInfo("This commit only deletes files")
	}
	m.gitxPick(title, items, func(m *Model, id string) tea.Cmd {
		if p, ok := strings.CutPrefix(id, "file-"); ok {
			return then(m, p)
		}
		return nil
	})
	return nil
}

// gitxPickFileFrom applies one file's changes of a commit to the working
// tree, after a card.
func (m *Model) gitxPickFileFrom(full string) tea.Cmd {
	short := full[:min(7, len(full))]
	return m.gitxCommitFilePicker(full, " Cherry-pick which file of "+short+"? ", func(m *Model, path string) tea.Cmd {
		m.gitxCard("Apply the changes of "+path+"?", "The changes "+short+" made to "+path+" go into your working tree and are staged. Nothing is committed; a clash shows as a conflict.",
			func(m *Model, id string) tea.Cmd {
				if id != "apply" {
					return nil
				}
				cwd, _ := m.rootDir()
				return m.gitxPickDone("Cherry-pick", nil, git.ApplyFileFromCommit(cwd, full, path), "Applied "+path+" from "+short)
			}, confirm.Button{ID: "apply", Title: "Apply"})
		return nil
	})
}

// gitxRestoreFileFrom puts a file back as it was in a commit, after a
// card; uncommitted changes go to the stash list first.
func (m *Model) gitxRestoreFileFrom(full string) tea.Cmd {
	short := full[:min(7, len(full))]
	return m.gitxCommitFilePicker(full, " Restore which file as in "+short+"? ", func(m *Model, path string) tea.Cmd {
		m.gitxCard("Restore "+path+" as in "+short+"?", "Your "+path+" is replaced by its version in "+short+". If it has uncommitted changes, they are saved in the stash list first.",
			func(m *Model, id string) tea.Cmd {
				if id != "restore" {
					return nil
				}
				cwd, _ := m.rootDir()
				backup, err := git.RestoreFileFrom(cwd, full, path)
				if err != nil {
					return m.gitxFail("Restore failed", err)
				}
				detail := "Undo: git restore --source=HEAD --staged --worktree -- " + path
				if backup != "" {
					detail = "Your changes are in " + backup + " (Git: Show Stash List...)"
				}
				var c tea.Cmd
				m.toast, c = m.toast.PushDetail(toast.Info, "Restored "+path+" from "+short, detail)
				if m.nvim != nil {
					_ = m.nvim.Command("silent! checktime")
				}
				return tea.Batch(c, fetchGitCmd())
			}, confirm.Button{ID: "restore", Title: "Restore"})
		return nil
	})
}

// ── GITX-17 ─────────────────────────────────────────────────────────────

// gitxRebaseNotes is the pushed-commits and stash part of a rebase card.
func gitxRebaseNotes(cwd, base string) (string, bool) {
	var lines []string
	if up, n := git.RebasePushed(cwd, base); n > 0 {
		lines = append(lines, "", fmt.Sprintf("⚠ %s of these are on %s already: you will need to force-push.", gitxCount(n), up))
	}
	dirty := git.WorkTreeDirty(cwd)
	if dirty {
		lines = append(lines, "", "You have uncommitted changes: they are stashed first and put back after.")
	}
	lines = append(lines, "", "Undo afterwards: u / Git: Undo Last Git Action")
	return strings.Join(lines, "\n"), dirty
}

func (m *Model) gitxRebaseCmd(summary string, run func(cwd string, opt git.RebaseOptions) (git.RebaseOutcome, error), stash bool) tea.Cmd {
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		out, err := run(cwd, git.RebaseOptions{Autostash: stash})
		return rebaseDoneMsg{summary: summary, out: out, err: err}
	}
}

func (m *Model) openRebaseOntoCard(onto string) tea.Cmd {
	if m.gitRebase.InProgress {
		return m.rebaseBusyToast()
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	mine, err := git.CommitsNotOn(cwd, onto, 200)
	if err != nil {
		return m.gitxFail("Rebase failed", err)
	}
	if len(mine) == 0 {
		return m.gitxInfo(m.gitBranch.Name + " has no commits that " + onto + " lacks")
	}
	notes, dirty := gitxRebaseNotes(cwd, onto)
	text := "Replay " + gitxCount(len(mine)) + " of " + m.gitBranch.Name + " on top of " + onto + ":\n" + gitxCommitLines(mine) +
		"\nRebase (Keep Merges) keeps merge commits as merges." + notes
	m.gitxCard("Rebase "+m.gitBranch.Name+" onto "+onto+"?", text, func(m *Model, id string) tea.Cmd {
		if id != "rebase" && id != "rebase-merges" {
			return nil
		}
		keep := id == "rebase-merges"
		return m.gitxRebaseCmd("onto "+onto, func(cwd string, opt git.RebaseOptions) (git.RebaseOutcome, error) {
			return gitxRebaseRuns.onto(cwd, onto, keep, opt)
		}, dirty)
	}, confirm.Button{ID: "rebase", Title: "Rebase", Style: confirm.StyleDestructive},
		confirm.Button{ID: "rebase-merges", Title: "Rebase (Keep Merges)", Style: confirm.StyleDestructive})
	m.confirm = m.confirm.WithWideText()
	return nil
}

// openRowRebaseCard asks before a drop or squash of one commit.
func (m *Model) openRowRebaseCard(c git.GraphCommit, what, title, text, button string) tea.Cmd {
	if m.gitRebase.InProgress {
		return m.rebaseBusyToast()
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	base, _ := git.ParentOf(cwd, c.FullHash)
	notes, dirty := gitxRebaseNotes(cwd, base)
	m.gitxCard(title, text+notes, func(m *Model, id string) tea.Cmd {
		if id != what {
			return nil
		}
		run := gitxRebaseRuns.drop
		if what == "squash" {
			run = gitxRebaseRuns.squash
		}
		return m.gitxRebaseCmd(what+" "+c.Hash, func(cwd string, opt git.RebaseOptions) (git.RebaseOutcome, error) {
			return run(cwd, c.FullHash, opt)
		}, dirty)
	}, confirm.Button{ID: what, Title: button, Style: confirm.StyleDestructive})
	return nil
}

// openRewordPrompts asks the subject, then the body (\n starts a new
// line), then asks on a card.
func (m *Model) openRewordPrompts(c git.GraphCommit) tea.Cmd {
	if m.gitRebase.InProgress {
		return m.rebaseBusyToast()
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	msg, err := git.CommitMessage(cwd, c.FullHash)
	if err != nil {
		return m.gitxFail("Could not read the message", err)
	}
	subject, body, _ := strings.Cut(strings.TrimRight(msg, "\n"), "\n")
	body = strings.ReplaceAll(strings.TrimLeft(body, "\n"), "\n", `\n`)
	m.gitxPrompt("Edit Commit Message · "+c.Hash, "Subject", subject, func(m *Model, s string) tea.Cmd {
		s = strings.TrimSpace(s)
		if s == "" {
			return m.gitxInfo("The subject is empty: nothing changed")
		}
		m.gitxPrompt("Edit Commit Message · "+c.Hash, `Body (\n starts a new line; empty: no body)`, body, func(m *Model, b string) tea.Cmd {
			full := s
			if b = strings.TrimSpace(strings.ReplaceAll(b, `\n`, "\n")); b != "" {
				full += "\n\n" + b
			}
			base, _ := git.ParentOf(cwd, c.FullHash)
			notes, dirty := gitxRebaseNotes(cwd, base)
			m.gitxCard("Change the message of "+c.Hash+"?", full+"\n"+notes, func(m *Model, id string) tea.Cmd {
				if id != "reword" {
					return nil
				}
				return m.gitxRebaseCmd("reword "+c.Hash, func(cwd string, opt git.RebaseOptions) (git.RebaseOutcome, error) {
					return gitxRebaseRuns.reword(cwd, c.FullHash, full, opt)
				}, dirty)
			}, confirm.Button{ID: "reword", Title: "Change Message", Style: confirm.StyleDestructive})
			return nil
		})
		return nil
	})
	return nil
}

func (m *Model) openAutosquashCard() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	if m.gitRebase.InProgress {
		return m.rebaseBusyToast()
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	n, base, err := git.AutosquashPlan(cwd)
	if err != nil {
		return m.gitxFail("Autosquash failed", err)
	}
	if n == 0 {
		return m.gitxInfo("No fixup! commits on this branch")
	}
	notes, dirty := gitxRebaseNotes(cwd, base)
	m.gitxCard(fmt.Sprintf("Fold %s into their targets?", gitxNoun(n, "fixup! commit")),
		"Each fixup! / squash! commit joins the commit it names (git rebase --autosquash)."+notes, func(m *Model, id string) tea.Cmd {
			if id != "autosquash" {
				return nil
			}
			return m.gitxRebaseCmd("autosquash", gitxRebaseRuns.autosquash, dirty)
		}, confirm.Button{ID: "autosquash", Title: "Autosquash", Style: confirm.StyleDestructive})
	return nil
}

func gitxNoun(n int, one string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// ── GITX-25 ─────────────────────────────────────────────────────────────

// gitxPatchPath is a typed path, relative to the workspace root.
func (m *Model) gitxPatchPath(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", errors.New("no file name")
	}
	if filepath.IsAbs(v) {
		return v, nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, v), nil
}

func (m *Model) openSavePatchPrompt() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	m.gitxPrompt("Save Changes as Patch", "File (in the workspace)", "changes.patch", func(m *Model, v string) tea.Cmd {
		p, err := m.gitxPatchPath(v)
		if err != nil {
			return m.gitxFail("Save failed", err)
		}
		patch, err := git.ChangesPatch(cwd)
		if err != nil {
			return m.gitxFail("Save failed", err)
		}
		if strings.TrimSpace(patch) == "" {
			return m.gitxInfo("No changes to save")
		}
		write := func(m *Model) tea.Cmd {
			if err := os.WriteFile(p, []byte(patch), 0o644); err != nil {
				return m.gitxFail("Save failed", err)
			}
			return m.gitxInfo("Saved the changes to " + v)
		}
		if _, err := os.Stat(p); err == nil {
			m.gitxCard("Replace "+v+"?", v+" exists; the new patch replaces it.", func(m *Model, id string) tea.Cmd {
				if id == "replace" {
					return write(m)
				}
				return nil
			}, confirm.Button{ID: "replace", Title: "Replace", Style: confirm.StyleDestructive})
			return nil
		}
		return write(m)
	})
	return nil
}

func (m *Model) openApplyPatchFilePrompt() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	m.gitxPrompt("Apply Patch", "Patch file (in the workspace)", "", func(m *Model, v string) tea.Cmd {
		p, err := m.gitxPatchPath(v)
		if err != nil {
			return m.gitxFail("Apply failed", err)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return m.gitxFail("Apply failed", err)
		}
		return m.gitxCheckPatch(v, string(b))
	})
	return nil
}

// gitxCheckPatch checks a patch and shows what it changes on a
// Cancel-first card; Apply changes the working tree.
func (m *Model) gitxCheckPatch(from, patch string) tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.rootDir()
	if err != nil {
		return nil
	}
	stat, err := git.CheckPatch(cwd, patch)
	if err != nil {
		return m.gitxFail("The patch does not apply", err)
	}
	m.gitxCard("Apply the patch from "+from+"?", stat+"\n\nThe changes go into your working tree; nothing is committed.", func(m *Model, id string) tea.Cmd {
		if id != "apply" {
			return nil
		}
		if err := git.ApplyPatch(cwd, patch); err != nil {
			return m.gitxFail("Apply failed", err)
		}
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
		return tea.Batch(fetchGitCmd(), m.gitxInfo("Applied the patch from "+from))
	}, confirm.Button{ID: "apply", Title: "Apply"})
	m.confirm = m.confirm.WithWideText()
	return nil
}
