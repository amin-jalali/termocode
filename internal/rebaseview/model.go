// Package rebaseview is the interactive-rebase card: the commits to
// replay (oldest first), each with an action (pick, reword, edit, squash,
// fixup, drop) set by one key, rows moved up and down, and a preview of
// what is left ("5 commits → 3 commits"). It runs nothing: Enter asks
// the app to start (StartMsg), r asks it for a new message (RewordMsg).
package rebaseview

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
)

// StartMsg: the user started the rebase with this todo (it is valid).
type StartMsg struct{ Todo git.RebaseTodo }

// CloseMsg: the user closed the card (nothing ran).
type CloseMsg struct{}

// RewordMsg: row Index is now reword; the app asks for its message and
// calls SetMessage.
type RewordMsg struct {
	Index int
	Item  git.RebaseItem
}

// Model is the card.
type Model struct {
	title string
	todo  git.RebaseTodo
	cur   int
	w, h  int
	err   string // why Enter did not start
}

// New makes the card for todo.
func New(title string, todo git.RebaseTodo) Model {
	todo.Items = slices.Clone(todo.Items)
	return Model{title: title, todo: todo}
}

// Todo is the todo as the user set it.
func (m Model) Todo() git.RebaseTodo {
	t := m.todo
	t.Items = slices.Clone(t.Items)
	return t
}

// Cursor is the row under the cursor.
func (m Model) Cursor() int { return m.cur }

// SetSize is the screen size.
func (m *Model) SetSize(w, h int) { m.w, m.h = w, h }

// SetMessage gives row i (made reword) its new message.
func (m *Model) SetMessage(i int, msg string) {
	if i < 0 || i >= len(m.todo.Items) {
		return
	}
	m.todo.Items = slices.Clone(m.todo.Items)
	m.todo.Items[i].Action = git.RebaseReword
	m.todo.Items[i].Message = msg
}

var actionKeys = map[rune]git.RebaseAction{
	'p': git.RebasePick, 'r': git.RebaseReword, 'e': git.RebaseEdit,
	's': git.RebaseSquash, 'f': git.RebaseFixup, 'd': git.RebaseDrop,
}

func emit(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

// Update handles a key.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	n := len(m.todo.Items)
	switch k.Type {
	case tea.KeyEsc:
		return m, emit(CloseMsg{})
	case tea.KeyEnter:
		if err := m.todo.Validate(); err != nil {
			m.err = err.Error()
			return m, nil
		}
		return m, emit(StartMsg{Todo: m.Todo()})
	case tea.KeyUp:
		if k.Alt {
			m.move(-1)
		} else {
			m.cur = max(m.cur-1, 0)
		}
	case tea.KeyDown:
		if k.Alt {
			m.move(1)
		} else {
			m.cur = min(m.cur+1, n-1)
		}
	case tea.KeyHome:
		m.cur = 0
	case tea.KeyEnd:
		m.cur = n - 1
	case tea.KeyRunes:
		if len(k.Runes) != 1 || k.Alt {
			return m, nil
		}
		switch r := k.Runes[0]; r {
		case 'j':
			m.cur = min(m.cur+1, n-1)
		case 'k':
			m.cur = max(m.cur-1, 0)
		case 'J':
			m.move(1)
		case 'K':
			m.move(-1)
		case 'q':
			return m, emit(CloseMsg{})
		default:
			a, ok := actionKeys[r]
			if !ok {
				return m, nil
			}
			m.todo.Items = slices.Clone(m.todo.Items)
			m.todo.SetAction(m.cur, a)
			m.err = ""
			if a == git.RebaseReword {
				return m, emit(RewordMsg{Index: m.cur, Item: m.todo.Items[m.cur]})
			}
		}
	}
	return m, nil
}

// move moves the cursor row up (-1) or down (+1).
func (m *Model) move(d int) {
	m.todo.Items = slices.Clone(m.todo.Items)
	m.cur = m.todo.Move(m.cur, d)
	m.err = ""
}

// width is the card's outer width.
func (m Model) width() int { return max(min(m.w-4, 96), 40) }

// listRows is how many todo rows fit.
func (m Model) listRows() int { return max(m.h-12, 3) }

// firstRow is the first todo row shown (the cursor stays in view).
func (m Model) firstRow() int { return max(m.cur-m.listRows()+1, 0) }

// headRows is how many card rows come before the todo rows.
const headRows = 3

// lines are the card's rows (inside the border).
func (m Model) lines() []string {
	f := theme.Float()
	innerW := m.width() - 2
	base := lipgloss.NewStyle().Background(f.Bg)
	pad := func(s string, bg lipgloss.Color) string {
		w := ansi.StringWidth(s)
		if w > innerW {
			s, w = ansi.Truncate(s, innerW-1, "")+lipgloss.NewStyle().Background(bg).Render("…"), innerW
		}
		return s + lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", innerW-w))
	}
	var rows []string
	rows = append(rows, pad(base.Foreground(f.Title).Bold(true).Render(" "+m.title), f.Bg))
	rows = append(rows, pad(base.Foreground(f.Dim).Render(" Oldest first. Rows run top to bottom."), f.Bg))
	rows = append(rows, pad("", f.Bg)) // headRows

	vis, top := m.listRows(), m.firstRow()
	for i := top; i < len(m.todo.Items) && i < top+vis; i++ {
		rows = append(rows, m.row(i, innerW, pad))
	}
	rows = append(rows, pad("", f.Bg))
	rows = append(rows, pad(base.Foreground(f.Text).Bold(true).Render(" "+m.todo.Summary()), f.Bg))
	if m.err != "" {
		rows = append(rows, pad(base.Foreground(f.Danger).Render(" "+m.err), f.Bg))
	}
	rows = append(rows, base.Foreground(f.Hairline).Render(strings.Repeat("─", innerW)))
	rows = append(rows, pad(" "+theme.HintBar(f.Bg, "p r e s f d", "action", "J/K", "move", "enter", "start", "esc", "cancel"), f.Bg))
	return rows
}

// row draws todo row i.
func (m Model) row(i, innerW int, pad func(string, lipgloss.Color) string) string {
	f := theme.Float()
	it := m.todo.Items[i]
	bg := f.Bg
	marker := "  "
	if i == m.cur {
		bg, marker = f.SelBg, "▌ "
	}
	st := lipgloss.NewStyle().Background(bg)
	actFg := map[git.RebaseAction]lipgloss.Color{
		git.RebasePick: f.Text, git.RebaseReword: f.Primary, git.RebaseEdit: f.Match,
		git.RebaseSquash: f.Sub, git.RebaseFixup: f.Sub, git.RebaseDrop: f.Danger,
	}[it.Action]
	subject := it.Subject
	if it.Action == git.RebaseReword && it.Message != "" {
		subject, _, _ = strings.Cut(it.Message, "\n")
	}
	subFg := f.Text
	if it.Action == git.RebaseDrop {
		subFg = f.Dim
	}
	s := st.Foreground(f.SelAccent).Render(marker) +
		st.Foreground(actFg).Bold(true).Render(padRight(string(it.Action), 7)) +
		st.Foreground(f.Dim).Render(" "+it.Hash+"  ") +
		st.Foreground(subFg).Strikethrough(it.Action == git.RebaseDrop).Render(subject)
	return pad(s, bg)
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

// Box renders the card (not placed).
func (m Model) Box() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	return theme.FloatBox(strings.Join(m.lines(), "\n"), m.width())
}

// Origin is the card's top-left cell when centred.
func (m Model) Origin() (x, y int) {
	h := len(m.lines()) + 2
	return max((m.w-m.width())/2, 0), max((m.h-h)/2, 0)
}

// HandleMouse: a click on a row puts the cursor there; the wheel moves it.
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) Model {
	switch button {
	case tea.MouseButtonWheelUp:
		m.cur = max(m.cur-1, 0)
		return m
	case tea.MouseButtonWheelDown:
		m.cur = min(m.cur+1, len(m.todo.Items)-1)
		return m
	}
	if action != tea.MouseActionPress || button != tea.MouseButtonLeft {
		return m
	}
	ox, oy := m.Origin()
	i := m.firstRow() + y - oy - 1 - headRows
	if x > ox && x < ox+m.width()-1 && y-oy-1-headRows >= 0 && y-oy-1-headRows < m.listRows() && i < len(m.todo.Items) {
		m.cur = i
	}
	return m
}
