package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/preview"
	"github.com/amin-jalali/termocode/internal/search"
)

// Regression (monkey seed 1793227386945818000): a preview opened over
// Find in Files took the window resizes, so after the preview closed
// Find in Files was drawn at the old terminal size.
func TestFindInFilesTakesResizeUnderPreview(t *testing.T) {
	m := Model{w: 144, h: 31}
	m.search = search.New()
	m.search.SetSize(m.w, m.h)
	m.searchOpen = true
	m.preview = preview.New(" Keyboard Shortcuts ", "body")
	m.preview.SetSize(m.w, m.h)
	m.previewOpen = true

	m, _ = m.update(tea.WindowSizeMsg{Width: 63, Height: 54})
	m.previewOpen = false

	assertFrameSize(t, m.viewFrame(), 63, 54)
}

func assertFrameSize(t *testing.T, frame string, w, h int) {
	t.Helper()
	rows := strings.Split(frame, "\n")
	if len(rows) != h {
		t.Errorf("%d rows, want %d", len(rows), h)
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != w {
			t.Errorf("row %d is %d cells, want %d", i, got, w)
			return
		}
	}
}
