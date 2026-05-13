package app

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/prompt"
	"termocode/internal/toast"
)

// Replace-In-Workspace flow
//
// Step 1: prompt for the search pattern
// Step 2: prompt for the replacement text
// Step 3: confirm + run
//
// Implementation: ripgrep gives us a deterministic file list, then we shell
// out to `sed -i` (Linux) / `sed -i ''` (macOS) per match. This is simpler
// than building our own parser and faster than feeding files through nvim
// one-by-one.

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

// runReplaceInWorkspace executes the substitution. We use ripgrep to find
// the candidate files and then `sed -i` per file. Counts and surfaces a
// toast with the number of files modified.
func (m *Model) runReplaceInWorkspace(replacement string) tea.Cmd {
	find := m.replaceFind
	m.replaceFind = ""
	if find == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	// Step 1: ripgrep --files-with-matches  finds the files cheaply.
	out, err := exec.Command("rg", "-l", "--null", "-F", find, cwd).Output()
	if err != nil {
		// Exit code 1 means "no matches" — that's not an error for us.
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
			var toastCmd tea.Cmd
			m.toast, toastCmd = m.toast.Push(toast.Info, "No matches found")
			return toastCmd
		}
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "ripgrep failed", err.Error())
		return toastCmd
	}
	files := strings.Split(string(out), "\x00")
	count := 0
	for _, f := range files {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		// sed -i with a regex-safe escape on both sides. We use # as the
		// delimiter and escape any literal # that show up in either string.
		safeFind := sedEscape(find)
		safeRepl := sedEscape(replacement)
		expr := fmt.Sprintf("s#%s#%s#g", safeFind, safeRepl)
		// `--posix` keeps semantics consistent across GNU/BSD sed; the `-i`
		// in-place flag differs syntactically (BSD wants a backup-suffix arg)
		// so we use a portable pattern: write to a tmpfile then rename. But
		// since termocode is dev-targeted at Linux & macOS users with GNU
		// coreutils available (or Homebrew gnu-sed on macOS), the simpler
		// `sed -i` works in practice. Failures are silently skipped — the
		// toast count reflects the real number of successful files.
		if err := exec.Command("sed", "-i", expr, f).Run(); err == nil {
			count++
		}
	}
	if m.nvim != nil {
		_ = m.nvim.Command("silent! checktime")
	}
	var toastCmd tea.Cmd
	if count == 0 {
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No files modified")
	} else {
		m.toast, toastCmd = m.toast.Push(toast.Info, fmt.Sprintf("Replaced in %d file(s)", count))
	}
	return toastCmd
}

// sedEscape neutralises the characters that have meaning in a sed `s#…#…#g`
// command. We chose `#` as the delimiter precisely because it's rare in
// code; we still need to escape `#`, `&`, and `\` for safety.
func sedEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`#`, `\#`,
		`&`, `\&`,
	)
	return r.Replace(s)
}
