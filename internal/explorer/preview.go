package explorer

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/preview"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/workspace"
)

type PreviewMsg struct {
	Path, Text string
	Seq        uint64
}

func (m Model) PreviewEnabled() bool { return m.previewOn }
func (m Model) PreviewVisible() bool { return m.previewOn && m.terminalWidth >= 100 }
func (m *Model) SetTerminalWidth(w int) {
	was := m.PreviewVisible()
	m.terminalWidth = w
	if was != m.PreviewVisible() {
		m.previewPath = ""
		m.previewSeq++
	}
}
func (m *Model) TogglePreview() tea.Cmd {
	m.previewOn = !m.previewOn
	m.previewSeq++
	m.previewPath = ""
	m.previewText = ""
	return m.SchedulePreview()
}

// SchedulePreview cancels stale results by sequence and path. The timer channel
// is allocated now, so tests can advance it before executing the command.
func (m *Model) SchedulePreview() tea.Cmd {
	if !m.PreviewVisible() {
		return nil
	}
	path := m.CursorPath()
	if path == m.previewPath {
		return nil
	}
	m.previewPath = path
	m.previewText = "Loading…"
	m.previewTop = 0
	m.previewSeq++
	seq := m.previewSeq
	timer := clock.After(100 * time.Millisecond)
	var fsys workspace.FS
	if m.root != nil {
		fsys = m.root.fsys
	}
	return func() tea.Msg {
		<-timer
		if fsys != nil { // a container workspace
			return PreviewMsg{Path: path, Seq: seq, Text: preview.ReadFileFS(context.Background(), fsys, path).Text}
		}
		return PreviewMsg{Path: path, Seq: seq, Text: preview.ReadFile(path).Text}
	}
}
func (m Model) acceptPreview(p PreviewMsg) Model {
	if m.PreviewVisible() && p.Seq == m.previewSeq && p.Path == m.CursorPath() {
		m.previewText = p.Text
	}
	return m
}
func (m Model) previewView(focused bool, activePath string, dirty bool, gs GitStatus) string {
	tree := m
	tree.previewOn = false
	tree.w = m.w / 2
	left := strings.Split(tree.draw(focused, activePath, dirty, gs), "\n")
	text := m.previewText
	if m.previewPath != m.CursorPath() {
		text = "Loading…"
	}
	width := max(m.w-tree.w-1, 0)
	body := strings.Split(ansi.Wrap(text, max(width, 1), ""), "\n")
	top := min(m.previewTop, max(len(body)-max(m.h-2, 1), 0))
	rows := append([]string{"Preview · " + filepath.Base(m.CursorPath()), "PgUp/PgDn scroll"}, body[top:]...)
	style := lipgloss.NewStyle().Background(theme.LG(theme.BgEditor)).Foreground(theme.LG(theme.TextPrimary))
	for i := range left {
		right := ""
		if i < len(rows) {
			right = rows[i]
		}
		right = ansi.Truncate(right, width, "")
		left[i] += "│" + style.Render(right+strings.Repeat(" ", max(width-lipgloss.Width(right), 0)))
	}

	return strings.Join(left, "\n")
}

func (m *Model) scrollPreview(delta int) {
	width := max(m.w-m.w/2-1, 1)
	count := len(strings.Split(ansi.Wrap(m.previewText, width, ""), "\n"))
	m.previewTop = min(max(m.previewTop+delta, 0), max(count-max(m.h-2, 1), 0))
}
