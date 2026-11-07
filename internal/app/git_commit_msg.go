package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Commit box upgrades (GITX-6): Up / Down step through the recent
// messages, Pick Recent Commit Message…, commit.template fills an empty
// box, a length ruler for the subject, the message in a full editor
// buffer, and Co-authored-by trailers.

const (
	pickerKindCommitHistory pickerKindEnum = 3650
	pickerKindCoAuthor      pickerKindEnum = 3651
)

const (
	gitSubjectSoft = 50 // past it the ruler warns
	gitSubjectHard = 72 // past it the ruler is red
	gitHistMax     = 20
)

// gitCommitEditName is the editor file, in the repository's git folder.
const gitCommitEditName = "TERMOCODE_EDITMSG"

// gitCommitRuler is the subject length and its level: 0 fine, 1 warn
// (past 50, or a second line that is not blank), 2 over 72.
func gitCommitRuler(msg string) (string, int) {
	subject, rest, multi := strings.Cut(msg, "\n")
	n := len([]rune(subject))
	level := 0
	switch {
	case n > gitSubjectHard:
		level = 2
	case n > gitSubjectSoft:
		level = 1
	}
	if multi && level == 0 {
		second, _, _ := strings.Cut(rest, "\n")
		if strings.TrimSpace(second) != "" {
			level = 1
		}
	}
	return strconv.Itoa(n), level
}

// gitCommitMsgReset forgets the history position, the template and the
// editor file after a commit.
func (m *Model) gitCommitMsgReset() {
	m.gitCommitHist, m.gitCommitHistLoaded, m.gitCommitHistPos, m.gitCommitDraft = nil, false, 0, ""
	m.gitCommitTplTried = false
	m.gitCommitEditFile = ""
}

func (m *Model) gitLoadCommitHistory() {
	if m.gitCommitHistLoaded {
		return
	}
	m.gitCommitHistLoaded = true
	if cwd, err := m.gitRootDir(); err == nil {
		m.gitCommitHist, _ = git.RecentMessages(cwd, gitHistMax)
	}
}

// gitCommitHistoryStep shows an older (d = 1) or newer (d = -1) message;
// past the newest the draft comes back.
func (m *Model) gitCommitHistoryStep(d int) {
	m.gitLoadCommitHistory()
	pos := m.gitCommitHistPos + d
	if pos < 0 || pos > len(m.gitCommitHist) {
		return
	}
	if m.gitCommitHistPos == 0 {
		m.gitCommitDraft = m.gitCommitMsg
	}
	m.gitCommitHistPos = pos
	if pos == 0 {
		m.gitCommitMsg = m.gitCommitDraft
	} else {
		m.gitCommitMsg = m.gitCommitHist[pos-1]
	}
	m.gitCommitCaret = len([]rune(m.gitCommitMsg))
}

// gitSetCommitMsg puts text in the box; the old text stays reachable with
// Up as the draft's history.
func (m *Model) gitSetCommitMsg(text string) {
	m.gitLoadCommitHistory()
	if old := strings.TrimSpace(m.gitCommitMsg); old != "" && old != strings.TrimSpace(text) {
		m.gitCommitHist = append([]string{old}, m.gitCommitHist...)
	}
	m.gitCommitHistPos = 0
	m.gitCommitMsg = text
	m.gitCommitFocused = true
	m.gitCommitTplTried = true
	m.gitCommitCaret = len([]rune(text))
}

// gitFillCommitTemplate fills an empty box with commit.template, once
// until the next commit.
func (m *Model) gitFillCommitTemplate() {
	if m.gitCommitMsg != "" || m.gitCommitTplTried {
		return
	}
	m.gitCommitTplTried = true
	cwd, err := m.gitRootDir()
	if err != nil {
		return
	}
	if tpl, err := git.CommitTemplate(cwd); err == nil && tpl != "" {
		m.gitCommitMsg = tpl
	}
}

// openCommitHistoryPicker lists the recent messages.
func (m *Model) openCommitHistoryPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	m.gitLoadCommitHistory()
	if len(m.gitCommitHist) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No commit messages yet")
		return c
	}
	items := make([]picker.Item, 0, len(m.gitCommitHist))
	for i, msg := range m.gitCommitHist {
		subject, body, _ := strings.Cut(msg, "\n")
		it := picker.Item{ID: "hist-" + strconv.Itoa(i), Title: subject}
		if n := strings.Count(strings.TrimSpace(body), "\n"); strings.TrimSpace(body) != "" {
			it.Hint = "+" + strconv.Itoa(n+1) + " lines"
		}
		items = append(items, it)
	}
	m.picker = picker.NewItems(" Recent Commit Messages ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindCommitHistory
	return nil
}

func (m *Model) onCommitHistoryPicked(id string) tea.Cmd {
	i, err := strconv.Atoi(strings.TrimPrefix(id, "hist-"))
	if err != nil || i < 0 || i >= len(m.gitCommitHist) {
		return nil
	}
	m.gitSetCommitMsg(m.gitCommitHist[i])
	m.gitRevealCommitBox()
	return nil
}

// gitRevealCommitBox shows the Source Control panel with the box
// focused.
func (m *Model) gitRevealCommitBox() {
	m.gitCommitFocused = true
	m.focus = FocusExplorer
}

// gitCommitEditHelp ends the editor file.
const gitCommitEditHelp = `

# Write the commit message above. Save the file (Ctrl+S): the Source
# Control message box takes the text. Commit from there as usual.
# Lines that start with # are left out.
`

// gitOpenCommitEditor writes the box's text (or the template) to
// .git/TERMOCODE_EDITMSG and opens it in the editor.
func (m *Model) gitOpenCommitEditor() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	gitDir, err := gitPathIn(cwd, gitCommitEditName)
	var c tea.Cmd
	if err != nil {
		m.toast, c = m.toast.PushDetail(toast.Errr, "No git folder", err.Error())
		return c
	}
	m.gitFillCommitTemplate()
	if err := os.WriteFile(gitDir, []byte(m.gitCommitMsg+gitCommitEditHelp), 0o600); err != nil {
		m.toast, c = m.toast.PushDetail(toast.Errr, "Could not write the message file", err.Error())
		return c
	}
	m.gitCommitEditFile = gitDir
	if st, err := os.Stat(gitDir); err == nil {
		m.gitCommitEditMod = st.ModTime()
	}
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`vim.cmd('edit ' .. vim.fn.fnameescape(...)); vim.bo.filetype = 'gitcommit'; pcall(vim.api.nvim_win_set_cursor, 0, {1, 0})`, gitDir)
		m.focus = FocusEditor
	}
	m.toast, c = m.toast.PushDetail(toast.Info, "Write the message, then save", "The Source Control box takes it")
	return c
}

// gitPathIn resolves a file in the repository's git folder.
func gitPathIn(cwd, name string) (string, error) {
	out, err := runGitOut(cwd, "rev-parse", "--git-path", name)
	if err != nil {
		return "", err
	}
	p := strings.TrimSpace(out)
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return p, nil
}

// syncCommitEditFile takes the editor file's text into the box after a
// save.
func (m *Model) syncCommitEditFile() {
	if m.gitCommitEditFile == "" {
		return
	}
	st, err := os.Stat(m.gitCommitEditFile)
	if err != nil {
		m.gitCommitEditFile = ""
		return
	}
	if !st.ModTime().After(m.gitCommitEditMod) {
		return
	}
	m.gitCommitEditMod = st.ModTime()
	b, err := os.ReadFile(m.gitCommitEditFile)
	if err != nil {
		return
	}
	m.gitCommitMsg = git.CleanMessage(string(b))
	m.gitCommitCaret = len([]rune(m.gitCommitMsg))
	m.gitCommitHistPos = 0
}

// openCoAuthorPicker lists the people who committed here (not you).
func (m *Model) openCoAuthorPicker() tea.Cmd {
	if !m.gitEnsureLoaded() {
		return nil
	}
	cwd, err := m.gitRootDir()
	if err != nil {
		return nil
	}
	me, _ := runGitOut(cwd, "config", "user.email")
	me = strings.ToLower(strings.TrimSpace(me))
	var items []picker.Item
	for _, a := range git.Authors(cwd, 500) {
		if me != "" && strings.Contains(strings.ToLower(a), "<"+me+">") {
			continue
		}
		items = append(items, picker.Item{ID: "coauthor-" + a, Title: a})
	}
	if len(items) == 0 {
		var c tea.Cmd
		m.toast, c = m.toast.Push(toast.Info, "No other authors in this repository yet")
		return c
	}
	m.picker = picker.NewItems(" Add Co-Author ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindCoAuthor
	return nil
}

func (m *Model) onCoAuthorPicked(id string) tea.Cmd {
	who, ok := strings.CutPrefix(id, "coauthor-")
	if !ok {
		return nil
	}
	msg := m.gitCommitMsg
	if strings.TrimSpace(msg) == "" {
		msg = "Message"
	}
	m.gitSetCommitMsg(git.AddTrailer(msg, "Co-authored-by", who))
	m.gitRevealCommitBox()
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Info, "Added a co-author", who)
	return c
}

// runGitOut runs a read-only git command and returns its output.
func runGitOut(dir string, args ...string) (string, error) {
	cmd := spawn.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	return string(out), err
}
