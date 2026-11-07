package app

import (
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/picker"
)

// Start work from an issue in a lane of its own (CMP-32).
//
// Worktree: Start from Issue… (I in the WORKTREES view) lists the open
// GitHub issues through gh with the user's own sign-in. A pick opens
// Worktree: New… filled in: the issue is linked (its state shows on the
// card), the branch comes from its title, the first installed agent is
// picked and the task names the issue. Cancel stays the first button;
// nothing is made until Create or Create + Start, and every field can
// still be changed. Git: Issues… has the same row for one issue.

const (
	wtFromIssuePaletteID = "worktree-from-issue"
	// wtIssueTitleMax caps the issue title typed into the task.
	wtIssueTitleMax = 200
)

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{{ID: wtFromIssuePaletteID, Title: "Worktree: Start from Issue…"}},
		policies: map[string]aiActionPolicy{
			wtFromIssuePaletteID: aiNever(aiNoPicker),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			if id == wtFromIssuePaletteID {
				return m.openWorktreeIssues(), true
			}
			return nil, false
		},
	})
}

// openWorktreeIssues lists the open issues; a pick opens the filled-in
// New worktree form.
func (m *Model) openWorktreeIssues() tea.Cmd {
	return m.openIssuesFor(" Start work from an issue ", (*Model).openWorktreeFromIssue)
}

// openWorktreeFromIssue reads the repository off the UI thread, then
// opens Worktree: New… for the issue (onWTNewPrep).
func (m *Model) openWorktreeFromIssue(is gitxIssue) tea.Cmd {
	if m.wt == nil || m.wt.off {
		return nil
	}
	root, _ := m.rootDir()
	setting := worktreeDirSetting()
	return func() tea.Msg {
		msg := wtNewPrep(root, setting)
		if msg.f != nil {
			wtPrefillIssue(msg.f, is)
		}
		return msg
	}
}

// wtPrefillIssue fills the form for an issue: Issue #N (the branch is
// drafted from its title), the first agent and a task.
func wtPrefillIssue(f *wtForm, is gitxIssue) {
	f.issueNum, f.issueTitle = is.Number, wtIssueText(is.Title)
	f.vals = map[string]string{
		"from":  f.from,
		"issue": "#" + strconv.Itoa(is.Number),
		"task":  wtIssueTask(is),
	}
	f.pick = 1 // onWTNewPrep drops it when no agent is installed
}

// wtIssueTask is the first task typed into the agent: one line, the
// issue's number and title, and how to read the rest.
func wtIssueTask(is gitxIssue) string {
	n := strconv.Itoa(is.Number)
	return "Work on issue #" + n + ": " + wtIssueText(is.Title) + ". Read it first with `gh issue view " + n + "`, then make the change on this branch."
}

// wtIssueText is an issue title made safe to show and type: control
// bytes and runs of space become one space, at most wtIssueTitleMax
// letters. The title is text from the issue's author, never a command.
func wtIssueText(s string) string {
	var b strings.Builder
	space := false
	n := 0
	inEsc := false
	for _, r := range s {
		if inEsc { // an ANSI escape: skip up to its final letter
			if r >= '@' && r <= '~' && r != '[' {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		if n >= wtIssueTitleMax {
			break
		}
		b.WriteRune(r)
		space = false
		n++
	}
	return strings.TrimSpace(b.String())
}
