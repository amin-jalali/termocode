package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/confirm"
	"termocode/internal/git"
	"termocode/internal/picker"
	"termocode/internal/prompt"
	"termocode/internal/toast"
)

// handleGitSidebarKey routes key presses when the Git sidebar has focus.
// Returns (model, cmd, true) if the key was consumed; (model, nil, false)
// otherwise so the caller can fall through to other handlers.
//
// Bindings (mirroring VSCode-ish habits while staying single-keystroke
// friendly for the terminal):
//
//	j / down, k / up   move cursor
//	g / G              jump top / bottom
//	enter              open the file in the editor
//	s                  stage / unstage (toggle, picks the right one based on
//	                   whether the file already has staged changes)
//	d                  diff vs HEAD (open in preview overlay)
//	c                  commit (opens a text-input prompt)
//	x                  discard worktree changes (with confirm)
//	r                  refresh
func (m Model) handleGitSidebarKey(msg tea.KeyMsg) (tea.Model, tea.Cmd, bool) {
	if !m.gitIsRepo {
		return m, nil, false
	}
	// Navigation and selection operate over the unified accordion rows
	// (section headers + change files/dirs + graph commits) so every section
	// shares one cursor model. Connector lines are skipped by gitMoveCursor.
	cur, hasCur := m.gitCurrentRow()

	switch msg.Type {
	case tea.KeyDown:
		m.gitMoveCursor(1)
		return m, nil, true
	case tea.KeyUp:
		m.gitMoveCursor(-1)
		return m, nil, true
	case tea.KeyLeft:
		// Collapse the section/dir under the cursor.
		if hasCur {
			switch {
			case cur.kind == gitRowSection:
				m.gitToggleSection(cur.section)
			case cur.kind == gitRowDir && !m.gitCollapsed[cur.dirPath]:
				m.gitToggleCollapse(cur.dirPath)
			}
		}
		return m, nil, true
	case tea.KeyRight:
		if hasCur {
			switch {
			case cur.kind == gitRowSection:
				m.gitToggleSection(cur.section)
			case cur.kind == gitRowDir && m.gitCollapsed[cur.dirPath]:
				m.gitToggleCollapse(cur.dirPath)
			}
		}
		return m, nil, true
	case tea.KeyEnter:
		if !hasCur {
			return m, nil, true
		}
		switch cur.kind {
		case gitRowSection:
			m.gitToggleSection(cur.section)
		case gitRowDir:
			m.gitToggleCollapse(cur.dirPath)
		case gitRowCommit:
			return m, m.gitShowCommitCmd(cur.hash), true
		case gitRowFile:
			path := gitFileAbsPath(m.gitFiles[cur.fileIndex].Path)
			if m.nvim != nil {
				m.ensureEditorWindowCurrent()
				_ = m.nvim.Command("edit " + path)
			}
			m.focus = FocusEditor
		}
		return m, nil, true
	case tea.KeyRunes:
		if len(msg.Runes) != 1 {
			return m, nil, false
		}
		switch msg.Runes[0] {
		case 'j':
			m.gitMoveCursor(1)
			return m, nil, true
		case 'k':
			m.gitMoveCursor(-1)
			return m, nil, true
		case 'g':
			m.gitCursor = 0
			m.gitClampCursor()
			return m, nil, true
		case 'G':
			m.gitCursor = len(m.gitPanelRows()) - 1
			m.gitClampCursor()
			return m, nil, true
		case 's':
			return m, m.gitStageToggle(), true
		case 'd':
			// On a graph commit, diff that commit; otherwise the file's diff.
			if hasCur && cur.kind == gitRowCommit {
				return m, m.gitShowCommitCmd(cur.hash), true
			}
			return m, m.gitDiffCmd(), true
		case 'c':
			m.openCommitPrompt()
			return m, nil, true
		case 'x':
			m.openGitDiscardConfirm()
			return m, nil, true
		case 'r':
			return m, fetchGitCmd(), true
		case 't':
			m.toggleGitViewMode()
			return m, nil, true
		}
	}
	return m, nil, false
}

// gitStageToggle stages an unstaged change (or untracked file) and unstages
// any change that's only present in the index. Files that have *both* staged
// and unstaged changes get the unstaged ones promoted to the index — that
// matches VSCode's "Stage" affordance and is the more useful default.
func (m *Model) gitStageToggle() tea.Cmd {
	fi, ok := m.gitCurrentFileIndex()
	if !ok {
		return nil
	}
	f := m.gitFiles[fi]
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var opErr error
	var verb string
	if f.Untracked() || f.Unstaged() {
		opErr = git.Stage(cwd, f.Path)
		verb = "Staged"
	} else {
		opErr = git.Unstage(cwd, f.Path)
		verb = "Unstaged"
	}
	var toastCmd tea.Cmd
	if opErr != nil {
		m.err = "git: " + opErr.Error()
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Git error", opErr.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, verb, f.Path)
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitDiffCmd loads the diff for the current file and opens it in the preview
// overlay. ANSI-coloured per-line so additions/removals stand out.
func (m Model) gitDiffCmd() tea.Cmd {
	fi, ok := m.gitCurrentFileIndex()
	if !ok {
		return nil
	}
	path := m.gitFiles[fi].Path
	return func() tea.Msg {
		cwd, err := os.Getwd()
		if err != nil {
			return ErrMsg{Err: err}
		}
		raw, err := git.Diff(cwd, path)
		if err != nil {
			return ErrMsg{Err: err}
		}
		body := colorizeDiff(raw)
		return PreviewMsg{Title: "diff · " + path, Body: body}
	}
}

// openCommitPrompt asks for a commit message via the standard prompt overlay.
// Submission is handled in handlePromptSubmit (case promptKindCommit).
func (m *Model) openCommitPrompt() {
	if !m.gitIsRepo {
		return
	}
	// We surface staged-only as the line under the title so the user knows
	// what the commit will include. If nothing is staged, warn — git would
	// normally just refuse, and "Commit (0 staged)" is clearer.
	staged := 0
	for _, f := range m.gitFiles {
		if f.Staged() {
			staged++
		}
	}
	subtitle := fmt.Sprintf("Message (%d staged file%s):", staged, plural(staged))
	m.prompt = prompt.New("Commit", subtitle, "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindCommit
}

// openGitDiscardConfirm wires up the standard confirm dialog for the destructive
// "Discard Changes" action.
func (m *Model) openGitDiscardConfirm() {
	fi, ok := m.gitCurrentFileIndex()
	if !ok {
		return
	}
	f := m.gitFiles[fi]
	m.confirm = confirm.New(
		"Discard changes?",
		fmt.Sprintf("This will revert \"%s\" to its last committed state. This cannot be undone.", f.Path),
		[]confirm.Button{
			{ID: "discard", Title: "Discard", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitDiscard
	m.confirmTargetPath = f.Path
}

// colorizeDiff prefixes each unified-diff line with ANSI SGR colours so the
// preview overlay (which renders pre-styled content as-is) shows additions
// in green, removals in red, and hunk headers in cyan.
func colorizeDiff(s string) string {
	const (
		reset = "\x1b[0m"
		green = "\x1b[38;2;115;201;145m"
		red   = "\x1b[38;2;199;78;57m"
		cyan  = "\x1b[38;2;86;156;214m"
		dim   = "\x1b[38;2;138;138;138m"
	)
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---"):
			b.WriteString(dim)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "diff ") || strings.HasPrefix(line, "index "):
			b.WriteString(dim)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "@@"):
			b.WriteString(cyan)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "+"):
			b.WriteString(green)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "-"):
			b.WriteString(red)
			b.WriteString(line)
			b.WriteString(reset)
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// gitDiscardConfirmed runs the actual discard once the user confirms. Called
// from handleConfirmSelect (confirm.go) for the confirmKindGitDiscard branch.
func (m *Model) gitDiscardConfirmed() tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	path := m.confirmTargetPath
	var toastCmd tea.Cmd
	if err := git.Discard(cwd, path); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Discard failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Discarded changes", path)
	}
	// Buffer that file may now be stale on disk — nudge nvim to checktime so
	// the editor picks up the reverted contents (autoread is on).
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitCommitFromPrompt runs `git commit -m <msg>` after a successful prompt
// submission. Called from handlePromptSubmit (file_ops.go) for the
// promptKindCommit branch.
func (m *Model) gitCommitFromPrompt(message string) tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Commit(cwd, message); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Commit failed", err.Error())
	} else {
		// Trim the message for the toast: keep first line, cap at 50 chars.
		first := message
		if i := strings.Index(first, "\n"); i >= 0 {
			first = first[:i]
		}
		if len(first) > 50 {
			first = first[:50] + "…"
		}
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Committed", first)
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitStageActiveFile stages the file currently shown in the editor. Used
// from the command palette so the user doesn't have to navigate the git
// sidebar for the common single-file case.
func (m *Model) gitStageActiveFile() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	rel, ok := m.activeGitRel()
	if !ok {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Stage(cwd, rel); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stage failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Staged", rel)
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitStageAll runs `git add -A`.
func (m *Model) gitStageAll() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Stage(cwd, "."); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stage all failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Staged all changes")
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitDiffActiveFile opens the diff for the currently active editor buffer.
func (m *Model) gitDiffActiveFile() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	rel, ok := m.activeGitRel()
	if !ok {
		return nil
	}
	return func() tea.Msg {
		cwd, err := os.Getwd()
		if err != nil {
			return ErrMsg{Err: err}
		}
		raw, err := git.Diff(cwd, rel)
		if err != nil {
			return ErrMsg{Err: err}
		}
		body := colorizeDiff(raw)
		return PreviewMsg{Title: "diff · " + rel, Body: body}
	}
}

// openGitDiscardForActiveFile opens the discard-confirm dialog for whichever
// file is currently in the editor. Same UX as 'x' on the git sidebar but
// reachable from the palette.
func (m *Model) openGitDiscardForActiveFile() {
	rel, ok := m.activeGitRel()
	if !ok {
		return
	}
	m.confirm = confirm.New(
		"Discard changes?",
		fmt.Sprintf("This will revert \"%s\" to its last committed state. This cannot be undone.", rel),
		[]confirm.Button{
			{ID: "discard", Title: "Discard", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		},
	)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitDiscard
	m.confirmTargetPath = rel
}

// gitPush runs `git push`. If the current branch has no upstream we add the
// `-u origin <branch>` form so the user doesn't have to drop to the
// integrated terminal for the first push.
func (m *Model) gitPush() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var opErr error
	if git.HasUpstream(cwd) {
		opErr = git.Push(cwd)
	} else if m.gitBranch.Name != "" {
		opErr = git.PushSetUpstream(cwd, m.gitBranch.Name)
	} else {
		opErr = fmt.Errorf("no current branch")
	}
	var toastCmd tea.Cmd
	if opErr != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Push failed", opErr.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Pushed to origin")
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitPull runs `git pull`.
func (m *Model) gitPull() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Pull(cwd); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Pull failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Pulled from origin")
		// Buffers may be stale on disk after a pull — nudge nvim to reload.
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// openBranchPicker shows a fuzzy picker of local branches; selecting one
// runs `git checkout <name>`. The first item is always "Create new branch
// …" (id "new") so users can stay in the picker flow for the new-branch
// case too.
func (m *Model) openBranchPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	branches, err := git.Branches(cwd)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Branches failed", err.Error())
		return toastCmd
	}
	items := make([]picker.Item, 0, len(branches)+1)
	items = append(items, picker.Item{ID: "new", Title: "+  Create new branch…", Hint: ""})
	for _, b := range branches {
		hint := ""
		if b == m.gitBranch.Name {
			hint = "current"
		}
		items = append(items, picker.Item{ID: "co-" + b, Title: b, Hint: hint})
	}
	m.picker = picker.NewItems(" Switch Branch ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindBranches
	return nil
}

// gitCheckoutSelected runs `git checkout <branch>` for the picked entry.
// Called from handlePickerSelect (case pickerKindBranches).
func (m *Model) gitCheckoutSelected(id string) tea.Cmd {
	if id == "new" {
		// Open a prompt for the new branch name; commit on submit goes
		// through handlePromptSubmit.
		m.prompt = prompt.New("New Branch", "Branch name:", "")
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindGitNewBranch
		return nil
	}
	branch := id[len("co-"):]
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Checkout(cwd, branch); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Checkout failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Switched to", branch)
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitCreateBranchFromPrompt runs `git checkout -b <name>` after a successful
// prompt submission for promptKindGitNewBranch.
func (m *Model) gitCreateBranchFromPrompt(name string) tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.CreateBranch(cwd, name); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Branch failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Created branch", name)
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitStash runs `git stash push` to save uncommitted changes. We don't
// prompt for a message — the timestamped default git produces is fine for
// the day-to-day "I need to switch context briefly" use case. Users who
// want a labelled stash can run `git stash push -m "label"` from the
// integrated terminal.
func (m *Model) gitStash() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Stash(cwd, ""); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Warn, "Stash failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Stashed changes")
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitStashPop runs `git stash pop` (applies stash@{0} and removes it).
func (m *Model) gitStashPop() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.StashPop(cwd); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stash pop failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, "Popped latest stash")
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// openStashPicker shows a list of all stashes; selection applies (without
// dropping). Drop has to go through the terminal for safety — accidental
// drops are unrecoverable.
func (m *Model) openStashPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	entries, err := git.StashList(cwd)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stash list failed", err.Error())
		return toastCmd
	}
	if len(entries) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No stashes")
		return toastCmd
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		items = append(items, picker.Item{
			ID:    e.Ref,
			Title: e.Ref + "   " + e.Message,
			Hint:  "apply",
		})
	}
	m.picker = picker.NewItems(" Stash List (Enter applies) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindStash
	return nil
}

// applyStashSelected runs `git stash apply <ref>` for the picked stash.
func (m *Model) applyStashSelected(ref string) tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.StashApply(cwd, ref); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Stash apply failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Applied", ref)
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitBlameCurrentLine fetches blame info for the cursor's line and shows
// it as a toast — quick "who wrote this" without taking the user out of
// flow. (The full file-history picker is gitFileHistoryPicker, below.)
func (m *Model) gitBlameCurrentLine() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	path := m.editor.Path()
	if path == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No file to blame")
		return toastCmd
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	line := m.cursorLine
	if line <= 0 {
		line = 1
	}
	bl, err := git.BlameLineAt(cwd, path, line)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Blame failed", err.Error())
		return toastCmd
	}
	msg := fmt.Sprintf("%s · %s · %s · %s", bl.Hash, bl.Author, bl.When, bl.Subject)
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, msg)
	return toastCmd
}

// gitFileHistoryPicker opens a picker showing every commit that touched
// the active file. Selecting a commit shows its full diff, same as the
// general "Show Log" picker.
func (m *Model) gitFileHistoryPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	path := m.editor.Path()
	if path == "" {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No file selected")
		return toastCmd
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	entries, err := git.FileHistory(cwd, path, 200)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "History failed", err.Error())
		return toastCmd
	}
	if len(entries) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No history (file untracked or new)")
		return toastCmd
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		title := e.Hash + "  " + e.Subject
		hint := e.Author + ", " + e.When
		items = append(items, picker.Item{
			ID:    "commit-" + e.Hash,
			Title: title,
			Hint:  hint,
		})
	}
	rel := path
	if r, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(r, "..") {
		rel = r
	}
	m.picker = picker.NewItems(" History · "+rel+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindGitLog // reuse: same Enter-shows-diff flow
	return nil
}

// gitTagPicker shows every tag; selection puts the tag name on the
// clipboard as a small "did something" affordance.
func (m *Model) gitTagPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	tags, err := git.Tags(cwd)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Tags failed", err.Error())
		return toastCmd
	}
	if len(tags) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No tags in this repo")
		return toastCmd
	}
	items := make([]picker.Item, 0, len(tags))
	for _, t := range tags {
		items = append(items, picker.Item{ID: "tag-" + t, Title: t})
	}
	m.picker = picker.NewItems(" Tags (Enter checks out) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindTags
	return nil
}

// checkoutTagSelected does `git checkout <tag>` (detached HEAD).
func (m *Model) checkoutTagSelected(id string) tea.Cmd {
	tag := strings.TrimPrefix(id, "tag-")
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	var toastCmd tea.Cmd
	if err := git.Checkout(cwd, tag); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Checkout failed", err.Error())
	} else {
		m.toast, toastCmd = m.toast.PushDetail(toast.Info, "Checked out tag (detached HEAD)", tag)
		if m.nvim != nil {
			_ = m.nvim.Command("silent! checktime")
		}
	}
	return tea.Batch(fetchGitCmd(), toastCmd)
}

// gitLogPicker shows the recent commit history. Selecting a commit opens
// its full diff in the preview overlay.
func (m *Model) gitLogPicker() tea.Cmd {
	if !m.gitIsRepo {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	entries, err := git.Log(cwd, 200)
	if err != nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Log failed", err.Error())
		return toastCmd
	}
	if len(entries) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No commits yet")
		return toastCmd
	}
	items := make([]picker.Item, 0, len(entries))
	for _, e := range entries {
		title := e.Hash + "  " + e.Subject
		if e.Refs != "" {
			title = title + "   " + e.Refs
		}
		hint := e.Author + ", " + e.When
		items = append(items, picker.Item{
			ID:    "commit-" + e.Hash,
			Title: title,
			Hint:  hint,
		})
	}
	m.picker = picker.NewItems(" Git Log (Enter shows diff) ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindGitLog
	return nil
}

// showCommitFromPicker handles the "user picked a commit from the log"
// case: load the commit's diff via `git show` and stuff it into the
// preview overlay.
func (m *Model) showCommitFromPicker(id string) tea.Cmd {
	return m.gitShowCommitCmd(strings.TrimPrefix(id, "commit-"))
}

// gitShowCommitCmd loads a commit's full diff (`git show <hash>`) and opens it
// in the preview overlay. Shared by the log picker and the Source Control
// graph's click-to-diff.
func (m *Model) gitShowCommitCmd(hash string) tea.Cmd {
	if hash == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		body, err := git.ShowCommit(cwd, hash)
		if err != nil {
			return ErrMsg{Err: err}
		}
		return PreviewMsg{Title: "commit · " + hash, Body: colorizeDiff(body)}
	}
}

// openCompareWithRevisionPrompt asks the user for a revision/branch to
// diff against (e.g. "main", "HEAD~3", "origin/main"). The submission goes
// through promptKindCompareRevision in handlePromptSubmit.
func (m *Model) openCompareWithRevisionPrompt() {
	m.prompt = prompt.New("Compare with Revision", "Revision (branch / HEAD~N / SHA):", "HEAD")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindCompareRevision
}

// runCompareWithRevision fetches `git diff <ref>` and renders it.
func (m *Model) runCompareWithRevision(ref string) tea.Cmd {
	if ref == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	return func() tea.Msg {
		body, err := git.DiffAgainst(cwd, ref)
		if err != nil {
			return ErrMsg{Err: err}
		}
		if strings.TrimSpace(body) == "" {
			body = "(no differences)"
		}
		return PreviewMsg{Title: "diff · vs " + ref, Body: colorizeDiff(body)}
	}
}

// activeGitRel returns the active editor file's path expressed as a
// repo-relative path (suitable for git CLI args). Returns ("", false) when
// there's no active file or it's outside the working directory.
func (m Model) activeGitRel() (string, bool) {
	abs := m.editor.Path()
	if abs == "" {
		return "", false
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}
	rel := abs
	if strings.HasPrefix(abs, cwd+"/") {
		rel = abs[len(cwd)+1:]
	} else if abs == cwd {
		return "", false
	}
	return rel, true
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
