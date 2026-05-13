package app

import (
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/picker"
	"termocode/internal/toast"
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

// onDiffRightPicked runs `diff -u` between the two stored paths and shows
// the result in the preview overlay (with the same colorising helper used
// by the git diff viewer).
func (m *Model) onDiffRightPicked(rightPath string) tea.Cmd {
	leftPath := m.diffLeftPath
	if leftPath == "" || rightPath == "" {
		return nil
	}
	return func() tea.Msg {
		out, err := exec.Command("diff", "-u", leftPath, rightPath).CombinedOutput()
		// `diff` exits 1 when files differ — that's the normal case. Only
		// treat exit ≥ 2 as a real failure.
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
				err = nil
			}
		}
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("diff: %w", err)}
		}
		body := colorizeDiff(string(out))
		title := fmt.Sprintf("diff · %s ↔ %s", leftPath, rightPath)
		return PreviewMsg{Title: title, Body: body}
	}
}
