package app

import (
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// openDiffFilesFlow is the entry point for "Compare Two Files...". It
// shows the same file picker as Quick Open; the chosen file becomes the
// "left" side. After submission we open a second picker for the "right"
// side. The two-step flow uses pickerKindDiffLeft / pickerKindDiffRight
// to remember which leg we're on.
func (m *Model) openDiffFilesFlow() tea.Cmd {
	items := loadFiles()
	if len(items) == 0 {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "No files in workspace")
		return toastCmd
	}
	m.picker = picker.NewItems(" Compare: pick LEFT file ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDiffLeft
	return nil
}

// onDiffLeftPicked stores the left-side path and opens the right-side picker.
func (m *Model) onDiffLeftPicked(id string) tea.Cmd {
	m.diffLeftPath = id
	items := loadFiles()
	m.picker = picker.NewItems(" Compare: pick RIGHT file ", items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindDiffRight
	return nil
}

// onDiffRightPicked reads both files and shows them in the diff card
// (internal/diffview, read-only): side by side or unified, word-level
// highlights, change navigation.
func (m *Model) onDiffRightPicked(rightPath string) tea.Cmd {
	leftPath := m.diffLeftPath
	if leftPath == "" || rightPath == "" {
		return nil
	}
	return func() tea.Msg {
		var text [2]string
		for i, p := range []string{leftPath, rightPath} {
			fi, err := os.Stat(p)
			if err != nil {
				return ErrMsg{Err: fmt.Errorf("compare: %w", err)}
			}
			if fi.Size() > maxCompareBytes {
				return ErrMsg{Err: fmt.Errorf("compare: %s is too big (over %d MB)", filepath.Base(p), maxCompareBytes>>20)}
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return ErrMsg{Err: fmt.Errorf("compare: %w", err)}
			}
			text[i] = string(b)
		}
		rel := aiRel(aiWorkspaceRoot())
		return compareReadyMsg{title: "Compare files", file: diffview.File{
			Name: rel(leftPath) + " ↔ " + rel(rightPath), Old: text[0], New: text[1]}}
	}
}

// maxCompareBytes caps each side of Compare Two Files.
const maxCompareBytes = 4 << 20

// compareReadyMsg opens a read-only diff card.
type compareReadyMsg struct {
	title string
	file  diffview.File
}

// openCompareCard shows a read-only diff card. An AI review that is still
// waiting keeps the card.
func (m *Model) openCompareCard(msg compareReadyMsg) tea.Cmd {
	s := m.aiS()
	if s.review != nil && m.aiReviewPending() > 0 {
		return m.aiInfo("Finish the AI review first (AI: Review Pending Changes)")
	}
	s.review = &aiReview{card: diffview.New(msg.title, []diffview.File{msg.file}, false)}
	return m.showAIReview()
}
