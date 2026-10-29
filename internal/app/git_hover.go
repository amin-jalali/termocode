package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
)

// commitDetailMsg delivers a fetched commit's metadata for the hover card.
type commitDetailMsg struct {
	hash   string
	detail git.CommitDetail
}

// gitCommitAtScreenY returns the SHA of the commit row under the pointer (x,y),
// or ("", false) when the pointer isn't over a commit in the Source Control
// panel.
func (m Model) gitCommitAtScreenY(x, y int) (string, bool) {
	if !m.showExp || m.activity.Active() != activity.ViewGit || !m.gitIsRepo {
		return "", false
	}
	if x < activity.Width || x >= activity.Width+m.explorerWidth {
		return "", false
	}
	top, bodyH, _ := m.gitPanelLayout(m.h)
	bodyRow := y - m.gitPanelTopOffset()
	if bodyRow < 0 || bodyRow >= bodyH {
		return "", false
	}
	idx := top + bodyRow
	rows := m.gitPanelRows()
	if idx >= 0 && idx < len(rows) && rows[idx].kind == gitRowCommit {
		return rows[idx].hash, true
	}
	return "", false
}

// fetchCommitDetailCmd loads a commit's metadata off the main loop.
func (m Model) fetchCommitDetailCmd(hash string) tea.Cmd {
	cwd, cwdErr := m.rootDir()
	return func() tea.Msg {
		if cwdErr != nil {
			return nil
		}
		d, err := git.ShowCommitDetail(cwd, hash)
		if err != nil {
			return nil
		}
		return commitDetailMsg{hash: hash, detail: d}
	}
}

// updateHoverCommit syncs hoverCommitHash with the pointer position and returns
// a fetch command when the pointer moves onto a not-yet-cached commit. Called
// from the Update wrapper after every message.
func (m *Model) updateHoverCommit() tea.Cmd {
	hash, ok := m.gitCommitAtScreenY(m.hoverX, m.hoverY)
	if !ok {
		m.hoverCommitHash = ""
		return nil
	}
	if hash == m.hoverCommitHash {
		return nil
	}
	m.hoverCommitHash = hash
	if _, cached := m.commitDetails[hash]; cached {
		return nil
	}
	return m.fetchCommitDetailCmd(hash)
}

// overlayCommitHoverCard paints the GitLens-style commit card next to the
// Source Control panel when the pointer is over a commit whose details have
// loaded. Returns base unchanged otherwise.
func (m Model) overlayCommitHoverCard(base string) string {
	if m.hoverCommitHash == "" || m.commitViewShown() || m.rebaseCardOpen() {
		return base // the commit view already shows the commit
	}
	d, ok := m.commitDetails[m.hoverCommitHash]
	if !ok || d.Hash == "" {
		return base
	}
	sidebarRight := activity.Width + m.explorerWidth
	cardW := commitHoverCardW
	if avail := m.w - sidebarRight - 2; cardW > avail {
		cardW = avail
	}
	if cardW < 28 {
		return base
	}
	var refs []git.Ref
	for _, c := range m.gitCommits {
		if c.Hash == m.hoverCommitHash || c.FullHash == m.hoverCommitHash {
			refs = c.Refs
			break
		}
	}
	card := renderCommitHoverCard(d, refs, cardW)
	if len(card) == 0 {
		return base
	}

	// Anchor just right of the sidebar, vertically near the pointer, clamped
	// to stay on-screen.
	x := sidebarRight + 1
	y := m.hoverY - 1
	if maxY := m.h - 1 - len(card); y > maxY {
		y = maxY
	}
	if y < 0 {
		y = 0
	}

	lines := strings.Split(base, "\n")
	for i, cl := range card {
		row := y + i
		if row < 0 || row >= len(lines) {
			continue
		}
		lines[row] = spliceAt(lines[row], cl, x)
	}
	return strings.Join(lines, "\n")
}

// commitHoverCardW is the hover card width (scm-graph §2.7).
const commitHoverCardW = 44

// commitHoverBodyMax is the most body lines the hover card shows.
const commitHoverBodyMax = 8

// renderCommitHoverCard builds the bordered commit card (author + date,
// subject, wrapped body, the refs as pills, a +/- stat line with the
// short hash and a key hint).
func renderCommitHoverCard(d git.CommitDetail, refs []git.Ref, w int) []string {
	var (
		panelBg  = theme.LG(theme.BgFloat)
		border   = theme.Float().Border
		authorFG = theme.LG(theme.AccentBlue)
		dimFG    = theme.LG(theme.TextMuted)
		subjFG   = theme.LG(theme.TextWhite)
		bodyFG   = theme.LG(theme.TextPrimary)
		addFG    = theme.LG(theme.GitAdded)
		delFG    = theme.LG(theme.GitDeleted)
	)
	inner := w - 4 // rounded border (2) + horizontal padding (2)
	if inner < 8 {
		return nil
	}
	st := func(fg lipgloss.Color) lipgloss.Style {
		return lipgloss.NewStyle().Background(panelBg).Foreground(fg)
	}
	pad := st(dimFG)

	var lines []string
	pushPlain := func(styled string, plainW int) {
		// Measure like lipgloss does: runewidth and the renderer can
		// disagree on symbols such as ⎇ or ⌥.
		plainW = ansi.StringWidth(styled)
		if plainW > inner {
			styled, plainW = ansi.Truncate(styled, inner, "…"), inner
		}
		if g := inner - plainW; g > 0 {
			styled += pad.Render(strings.Repeat(" ", g))
		}
		lines = append(lines, styled)
	}

	// Author · relative date
	author := runewidth.Truncate(d.Author, inner-len(d.DateRel)-3, "…")
	head := st(authorFG).Bold(true).Render(author) + pad.Render("  ·  ") + st(dimFG).Render(d.DateRel)
	pushPlain(head, runewidth.StringWidth(author)+5+runewidth.StringWidth(d.DateRel))
	if d.DateAbs != "" {
		pushPlain(st(dimFG).Render(d.DateAbs), runewidth.StringWidth(d.DateAbs))
	}
	pushPlain("", 0)

	for _, l := range wrapPlain(d.Subject, inner) {
		pushPlain(st(subjFG).Bold(true).Render(l), runewidth.StringWidth(l))
	}
	if d.Body != "" {
		pushPlain("", 0)
		bodyLines := wrapPlain(d.Body, inner)
		if len(bodyLines) > commitHoverBodyMax {
			bodyLines = bodyLines[:commitHoverBodyMax]
			last := runewidth.Truncate(bodyLines[commitHoverBodyMax-1], inner-1, "")
			bodyLines[commitHoverBodyMax-1] = last + "…"
		}
		for _, l := range bodyLines {
			pushPlain(st(bodyFG).Render(l), runewidth.StringWidth(l))
		}
	}

	pushPlain("", 0)
	for _, row := range hoverRefPillRows(refs, inner, panelBg) {
		pushPlain(row, 0)
	}
	stat := st(addFG).Render(fmt.Sprintf("+%d", d.Add)) + pad.Render(" ") +
		st(delFG).Render(fmt.Sprintf("−%d", d.Del)) + pad.Render(fmt.Sprintf("  %d file%s", d.Files, plural(d.Files)))
	statW := runewidth.StringWidth(fmt.Sprintf("+%d −%d  %d file%s", d.Add, d.Del, d.Files, plural(d.Files)))
	hash := st(dimFG).Render(d.Hash)
	gap := inner - statW - runewidth.StringWidth(d.Hash)
	if gap < 1 {
		gap = 1
	}
	pushPlain(stat+pad.Render(strings.Repeat(" ", gap))+hash, 0)
	key, label := st(theme.LG(theme.TextSecondary)), st(dimFG)
	pushPlain(key.Render("Enter")+label.Render(" open   ")+key.Render("y")+label.Render(" copy SHA"), 0)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(border).
		BorderBackground(panelBg).
		Background(panelBg).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))
	return strings.Split(box, "\n")
}

// wrapPlain word-wraps text to width w, preserving existing newlines as
// paragraph breaks. Long words are hard-split.
func wrapPlain(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		para = strings.TrimRight(para, " ")
		if para == "" {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case runewidth.StringWidth(line)+1+runewidth.StringWidth(word) <= w:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
			for runewidth.StringWidth(line) > w {
				out = append(out, runewidth.Truncate(line, w, ""))
				line = string([]rune(line)[len([]rune(runewidth.Truncate(line, w, ""))):])
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// hoverRefPillRows lays all refs out as pills (local, tag, remote tints
// on the card background), wrapping to more rows when they do not fit.
func hoverRefPillRows(refs []git.Ref, inner int, bg lipgloss.Color) []string {
	var rows []string
	var sb strings.Builder
	used := 0
	gap := lipgloss.NewStyle().Background(bg).Render(" ")
	for _, r := range refs {
		name := r.Name
		if runewidth.StringWidth(name) > inner-2 {
			name = runewidth.Truncate(name, inner-2, "…")
		}
		var fg, pbg lipgloss.Color
		switch r.Kind {
		case git.RefTag:
			fg, pbg = theme.TintPill(theme.AccentAmber, theme.BgFloat)
		case git.RefRemote:
			fg, pbg = theme.LG(theme.TextMuted), theme.Wash(theme.BgFloat, theme.WashHover)
		default:
			fg, pbg = theme.TintPill(theme.AccentBlue, theme.BgFloat)
		}
		pw := theme.PillWidth(name)
		if used > 0 && used+1+pw > inner {
			rows = append(rows, sb.String())
			sb.Reset()
			used = 0
		}
		if used > 0 {
			sb.WriteString(gap)
			used++
		}
		sb.WriteString(theme.Pill(name, fg, pbg, bg, false))
		used += pw
	}
	if used > 0 {
		rows = append(rows, sb.String())
	}
	return rows
}
