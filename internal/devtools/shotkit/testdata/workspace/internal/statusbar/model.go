// Package statusbar draws the one-line bar at the bottom of the screen.
package statusbar

import (
	"fmt"
	"strings"
)

// Model is the status bar state.
type Model struct {
	width  int
	branch string
	line   int
	col    int
	errors int
}

// New returns a bar of the given width.
func New(width int) Model { return Model{width: width, line: 1, col: 1} }

// SetBranch sets the git branch shown on the left.
func (m *Model) SetBranch(b string) { m.branch = b }

// SetCursor sets the cursor position shown on the right.
func (m *Model) SetCursor(line, col int) {
	m.line, m.col = line, col
}

// SetErrors sets the error count badge.
func (m *Model) SetErrors(n int) { m.errors = n }

// View renders the bar padded to the full width.
func (m Model) View() string {
	left := " " + m.branch
	if m.errors > 0 {
		left += fmt.Sprintf("  %d errors", m.errors)
	}
	right := fmt.Sprintf("Ln %d, Col %d ", m.line, m.col)
	gap := m.width - len(left) - len(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
