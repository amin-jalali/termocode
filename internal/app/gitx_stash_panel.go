package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/theme"
)

// The STASHES section of the Source Control panel (GITX-10, L18). It is
// shown between CHANGES and GRAPH while the repository has stashes and
// starts folded, so a long list of old stashes does not push GRAPH down;
// the header shows how many there are. Enter or a click on a stash opens
// the same actions as Git: Stashes… (apply, pop, view, branch, drop).

// appendStashRows adds the spacer, the STASHES header and (when open)
// one row per stash. No stash: nothing.
func (m Model) appendStashRows(rows []gitPanelRow) []gitPanelRow {
	if len(m.gitStashes) == 0 {
		return rows
	}
	rows = append(rows, gitPanelRow{kind: gitRowSpacer}, gitPanelRow{kind: gitRowSection, section: gitSecStashes})
	if m.gitStashesOpen {
		for i := range m.gitStashes {
			rows = append(rows, gitPanelRow{kind: gitRowStash, stash: i})
		}
	}
	return rows
}

// onStashRow opens the actions of stash i.
func (m *Model) onStashRow(i int) tea.Cmd {
	if i < 0 || i >= len(m.gitStashes) {
		return nil
	}
	return m.onStashPicked(m.gitStashes[i].Ref)
}

// renderGitStashRow is "   <message>  stash@{n}": the message, then the
// ref dim on the right.
func (m Model) renderGitStashRow(i int, active, focused bool, w int) string {
	if i < 0 || i >= len(m.gitStashes) {
		return sidebarFill.Render(strings.Repeat(" ", w))
	}
	e := m.gitStashes[i]
	bg := gitSelBg(active, focused)
	fgMsg, fgRef := theme.LG(theme.TextPrimary), theme.LG(theme.TextMuted)
	if active && focused {
		fgMsg, fgRef = theme.LG(theme.TextWhite), theme.Mix(theme.TextSecondary, theme.BorderFocus, 0.3)
	}
	rowStyle := lipgloss.NewStyle().Background(bg)
	msg := strings.TrimSpace(e.Message)
	ref := e.Ref
	avail := w - 4
	if runewidth.StringWidth(msg)+2+runewidth.StringWidth(ref) > avail {
		ref = ""
	}
	if runewidth.StringWidth(msg) > avail {
		msg = runewidth.Truncate(msg, max(avail, 1), "…")
	}
	var sb strings.Builder
	sb.WriteString(gitLead(active, focused, bg))
	sb.WriteString(rowStyle.Render("   "))
	sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(fgMsg).Render(msg))
	used := 1 + 3 + runewidth.StringWidth(msg)
	if ref != "" {
		sb.WriteString(rowStyle.Render("  "))
		sb.WriteString(lipgloss.NewStyle().Background(bg).Foreground(fgRef).Render(ref))
		used += 2 + runewidth.StringWidth(ref)
	}
	return clampSidebarRow(sb.String(), used, w, rowStyle)
}
