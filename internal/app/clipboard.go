package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var (
	clipboardOnce sync.Once
	clipboardOK   bool
)

// clipboardReady initialises golang.design/x/clipboard once and reports
// whether it is usable. Read / Write without a successful Init silently
// do nothing (or panic on CGO_ENABLED=0 builds), so every caller goes
// through here first.
func clipboardReady() (ok bool) {
	clipboardOnce.Do(func() {
		defer func() {
			if recover() != nil {
				clipboardOK = false
			}
		}()
		clipboardOK = clipLibInit() == nil
	})
	return clipboardOK
}

// clipPushMsg is dispatched from the system-clipboard watcher goroutine to
// the bubbletea event loop so we add to the ring on the main thread.
type clipPushMsg struct{ Content string }

// initClipboardCapture spins up a goroutine that watches the OS clipboard
// for changes and pushes them into the app's ring (clip_watch.go: on
// focus-in when the terminal reports focus, else every second). The picker reads
// snapshot via Items() at render time. Silently no-ops if the clipboard
// library can't initialise (no display, SSH without forwarding, etc.)
// and in Termux, where watching would mean polling termux-clipboard-get
// (battery); there the ring only gets in-editor yanks (clipHost.watch).
//
// initClipboardCapture is value-receiver because tea.Init is too. The
// ring it captures is a *clipring.Ring allocated in New(); its internal
// mutex makes the goroutine-driven Push safe.
func (m Model) initClipboardCapture() tea.Cmd {
	ring := m.clipRing
	if ring == nil {
		return nil
	}
	if !realClipHost().watch() || !clipboardReady() {
		return nil
	}
	w := newClipWatcher(clipLibRead, ring.Push, time.Second)
	return func() tea.Msg {
		activeClipWatcher.Store(w)
		go w.run(context.Background()) // lives until the program exits
		return nil
	}
}

// clipboardPickerItems converts the ring into picker items for the
// "Show Clipboard History" mode.
func (m Model) clipboardPickerItems() []ClipboardPickerItem {
	if m.clipRing == nil {
		return nil
	}
	entries := m.clipRing.Items()
	out := make([]ClipboardPickerItem, len(entries))
	for i, e := range entries {
		preview := clipPreview(e.Content)
		out[i] = ClipboardPickerItem{Index: i, Preview: preview, Content: e.Content}
	}
	return out
}

// ClipboardPickerItem is one row in the clipboard-history picker.
type ClipboardPickerItem struct {
	Index   int
	Preview string
	Content string
}

// clipPreview renders the first line, truncated at 60 cells, with a "Ln N"
// suffix when the captured payload spans multiple lines.
func clipPreview(s string) string {
	first := s
	lines := 1
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		first = s[:i]
		lines = strings.Count(s, "\n") + 1
	}
	first = ansi.Truncate(first, 60, "…")
	if lines > 1 {
		return fmt.Sprintf("%s  · %d lines", first, lines)
	}
	return first
}

// pasteClipboardEntry writes content to nvim's `+` register and pastes it
// at the cursor.
func (m *Model) pasteClipboardEntry(content string) tea.Cmd {
	if m.nvim == nil || content == "" {
		return nil
	}
	// Escape single quotes for the Ex command.
	escaped := strings.ReplaceAll(content, "'", "''")
	// Newlines and carriage returns must go through nvim_replace_termcodes
	// for \r — for our purposes setreg with type 'c' (character-wise)
	// handles multi-line content correctly.
	luaEscaped := strings.ReplaceAll(escaped, "\\", "\\\\")
	luaEscaped = strings.ReplaceAll(luaEscaped, "\n", "\\n")
	_ = m.nvim.Command(fmt.Sprintf(`lua vim.fn.setreg('+', vim.fn.split('%s', '\\n'), 'l')`, luaEscaped))
	_ = m.nvim.Command(`normal! "+p`)
	return nil
}
