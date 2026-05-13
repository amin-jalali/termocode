package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/nvim"
	"termocode/internal/toast"
)

// togglePinActiveTab adds or removes the active buffer from the pinned set.
// Pinned tabs are kept at the front of the tab bar in the order they were
// pinned — see reorderPinned, which is invoked from the StateMsg handler
// before m.tabs.SetBuffers.
func (m *Model) togglePinActiveTab() tea.Cmd {
	if m.activeBuf == 0 {
		return nil
	}
	if m.pinnedBufs == nil {
		m.pinnedBufs = make(map[int]int)
	}
	if _, on := m.pinnedBufs[m.activeBuf]; on {
		delete(m.pinnedBufs, m.activeBuf)
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Info, "Tab unpinned")
		return toastCmd
	}
	m.pinOrder++
	m.pinnedBufs[m.activeBuf] = m.pinOrder
	var toastCmd tea.Cmd
	m.toast, toastCmd = m.toast.Push(toast.Info, "Tab pinned")
	return toastCmd
}

// reorderPinned moves pinned buffers to the front of `bufs` (preserving
// pin-order: earliest pinned first), keeping unpinned buffers in their
// original relative order. Pinned-but-no-longer-listed buffers are dropped
// from the pinned set as a side effect (cleanup).
func (m *Model) reorderPinned(bufs []nvim.BufferInfo) []nvim.BufferInfo {
	if len(m.pinnedBufs) == 0 || len(bufs) == 0 {
		return bufs
	}
	pinned := make([]nvim.BufferInfo, 0, len(bufs))
	other := make([]nvim.BufferInfo, 0, len(bufs))
	stillPinned := make(map[int]int, len(m.pinnedBufs))
	for _, b := range bufs {
		if order, ok := m.pinnedBufs[b.ID]; ok {
			pinned = append(pinned, b)
			stillPinned[b.ID] = order
		} else {
			other = append(other, b)
		}
	}
	// Garbage-collect: drop pinned IDs that aren't in the current buffer
	// list (e.g. after :bdelete) so the map doesn't grow without bound.
	m.pinnedBufs = stillPinned
	// Sort pinned by their pin order (earliest first).
	for i := 0; i < len(pinned); i++ {
		for j := i + 1; j < len(pinned); j++ {
			if m.pinnedBufs[pinned[j].ID] < m.pinnedBufs[pinned[i].ID] {
				pinned[i], pinned[j] = pinned[j], pinned[i]
			}
		}
	}
	return append(pinned, other...)
}

// isPinned is exported as a helper for the tabbar renderer (when we wire
// the visual indicator); it's a tiny wrapper so internal pinned-set
// implementation can change later without churning every caller.
func (m Model) isPinned(id int) bool {
	_, ok := m.pinnedBufs[id]
	return ok
}
