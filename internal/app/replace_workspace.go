package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Replace-In-Workspace flow
//
// Step 1: prompt for the search pattern
// Step 2: prompt for the replacement text
// Step 3: count matches per file, then confirm ("Replace 12 matches in 3
//         files?") before anything is written
// Step 4: rewrite each file in Go
//
// The search is literal and case-sensitive across every workspace root.
// search.CountLiteral finds the files (ripgrep when installed, so
// .gitignore is honoured; the built-in walker otherwise) and the same exact
// byte match drives both the counts and the write, so the dialog's numbers
// are what actually gets replaced.

// openReplaceInWorkspacePrompt is step 1 — ask for the search pattern.
func (m *Model) openReplaceInWorkspacePrompt() {
	m.prompt = prompt.New("Replace in Workspace", "Find:", "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindReplaceFind
}

// continueReplaceWithReplacement is step 2 — once we have the search
// pattern, ask for the replacement.
func (m *Model) continueReplaceWithReplacement(find string) {
	m.replaceFind = find
	m.prompt = prompt.New("Replace in Workspace", fmt.Sprintf("Replace `%s` with:", find), "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindReplaceReplacement
}

// runReplaceInWorkspace is step 3: count the matches and open the confirm
// dialog. Nothing is written until the user picks "Replace".
func (m *Model) runReplaceInWorkspace(replacement string) tea.Cmd {
	find := m.replaceFind
	m.replaceFind = ""
	if find == "" {
		return nil
	}
	counts, err := search.CountLiteral(workspaceRoots(), find)
	var toastCmd tea.Cmd
	if err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Replace failed", err.Error())
		return toastCmd
	}
	if len(counts) == 0 {
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("No matches for %q", find))
		return toastCmd
	}
	m.replaceFind = find
	m.replaceRepl = replacement
	m.replaceTargets = counts
	m.confirm = confirm.New("Replace in Workspace",
		replaceConfirmMessage(find, replacement, counts, m.dirtyBufferCount(counts)),
		[]confirm.Button{
			{ID: "replace", Title: "Replace", Style: confirm.StyleDestructive},
			{ID: "cancel", Title: "Cancel"},
		})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindReplaceWorkspace
	return nil
}

// replaceConfirmMessage builds the dialog text: total matches, file count,
// and a warning when some target files have unsaved edits in the editor
// (those buffers will be reloaded from disk by :checktime).
func replaceConfirmMessage(find, repl string, counts []search.FileCount, dirty int) string {
	total := 0
	for _, c := range counts {
		total += c.Count
	}
	msg := fmt.Sprintf("Replace %d match%s of %q with %q in %d file%s?",
		total, matchPlural(total), find, repl, len(counts), plurals(len(counts)))
	if dirty > 0 {
		msg += fmt.Sprintf(" %d of them %s unsaved changes in the editor.",
			dirty, map[bool]string{true: "has", false: "have"}[dirty == 1])
	}
	return msg
}

// dirtyBufferCount counts target files that are open with unsaved edits.
func (m *Model) dirtyBufferCount(counts []search.FileCount) int {
	n := 0
	for _, c := range counts {
		for _, b := range m.bufs {
			if b.Modified && b.Path == c.Path {
				n++
				break
			}
		}
	}
	return n
}

// applyReplaceInWorkspace is step 4, run when the confirm dialog's
// "Replace" button is picked. Rewrites every counted file, reloads open
// buffers, and toasts the outcome.
func (m *Model) applyReplaceInWorkspace() tea.Cmd {
	find, repl, targets := m.replaceFind, m.replaceRepl, m.replaceTargets
	m.replaceFind, m.replaceRepl, m.replaceTargets = "", "", nil
	if find == "" {
		return nil
	}
	files, matches := 0, 0
	var failed []string
	for _, t := range targets {
		n, err := search.ReplaceLiteralInFile(t.Path, find, repl)
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", t.Path, err))
			continue
		}
		if n > 0 {
			files++
			matches += n
		}
	}
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	var toastCmd tea.Cmd
	switch {
	case len(failed) > 0:
		m.toast, toastCmd = m.toast.PushDetail(toast.Warn,
			fmt.Sprintf("Replaced in %d file%s, %d failed", files, plurals(files), len(failed)),
			strings.Join(failed, "\n"))
	case files == 0:
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No files modified")
	default:
		m.toast, toastCmd = m.toast.Push(toast.Info,
			fmt.Sprintf("Replaced %d match%s in %d file%s", matches, matchPlural(matches), files, plurals(files)))
	}
	return toastCmd
}
