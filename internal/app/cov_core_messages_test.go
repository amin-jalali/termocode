package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/explorer"
	"github.com/amin-jalali/termocode/internal/findbar"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/recents"
	"github.com/amin-jalali/termocode/internal/replacebar"
	"github.com/amin-jalali/termocode/internal/search"
	"github.com/amin-jalali/termocode/internal/tabbar"
	"github.com/amin-jalali/termocode/internal/toast"
)

func covBufText(t *testing.T, m Model) string {
	t.Helper()
	out, err := m.nvim.EvalLuaString(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The replace bar and the find bar change the buffer through nvim.
func TestCovCoreReplaceAndFindMessages(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	covEditorFile(t, &m, "r.txt", "a.b a.b\nx/y a.b\n")
	m.replaceOpen = true
	m, _ = m.update(replacebar.ReplaceNextMsg{Find: "a.b", Replace: "c&d"})
	if got := covBufText(t, m); !strings.HasPrefix(got, "c&d a.b") {
		t.Errorf("replace next: %q", got)
	}
	m, _ = m.update(replacebar.ReplaceAllMsg{Find: "a.b", Replace: "Z"})
	if got := covBufText(t, m); got != "c&d Z\nx/y Z" {
		t.Errorf("replace all: %q", got)
	}
	// Empty find: nothing happens.
	m, _ = m.update(replacebar.ReplaceAllMsg{Find: "", Replace: "Q"})
	// A key and a resize go to the open bar.
	m, _ = m.update(coreRunes("q"))
	m, _ = m.update(tea.MouseMsg{X: 1, Y: 1, Type: tea.MouseLeft})
	m, _ = m.update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m, _ = m.update(replacebar.CloseMsg{})
	if m.replaceOpen {
		t.Error("replace bar still open")
	}

	m.findOpen = true
	m, _ = m.update(findbar.SearchMsg{Query: "x/y"})
	m, _ = m.update(findbar.FindNextMsg{})
	m, _ = m.update(findbar.FindPrevMsg{})
	m, _ = m.update(findbar.ReplaceMsg{Find: "x/y", Replace: "w\\v"})
	if got := covBufText(t, m); !strings.Contains(got, `w\v Z`) {
		t.Errorf("find bar replace: %q", got)
	}
	m, _ = m.update(findbar.ReplaceAllMsg{Find: "Z", Replace: "~"})
	if got := covBufText(t, m); strings.Contains(got, "Z") {
		t.Errorf("find bar replace all: %q", got)
	}
	m, _ = m.update(findbar.SearchMsg{Query: ""})
	m, _ = m.update(findbar.CloseMsg{})
	if m.findOpen {
		t.Error("find bar still open")
	}
}

func TestCovCoreVimReplaceEscape(t *testing.T) {
	if got := vimReplaceEscape("a&b/c~\\\nd"); got != `a\&b\/c\~\\\rd` {
		t.Errorf("replace escape: %q", got)
	}
}

// Overlay results: recents, search, explorer opens, tabs, toasts.
func TestCovCoreOverlayResults(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	dir := t.TempDir()
	file := filepath.Join(dir, "main.go")
	_ = os.WriteFile(file, []byte("package main\n\nfunc main() {}\n"), 0o644)

	m.recentsOpen = true
	m, _ = m.update(recents.SelectMsg{Path: file})
	if m.recentsOpen || m.focus != FocusEditor {
		t.Errorf("recents select: open %v focus %v", m.recentsOpen, m.focus)
	}
	covWaitLua(t, m, `return vim.api.nvim_buf_get_name(0)`, file)
	m.recentsOpen = true
	m, _ = m.update(recents.SelectMsg{})
	m.recentsOpen = true
	m, _ = m.update(recents.CloseMsg{})
	if m.recentsOpen {
		t.Error("recents close")
	}

	other := filepath.Join(dir, "b.go")
	_ = os.WriteFile(other, []byte("package main\n\nvar x = 1\n"), 0o644)
	m.searchOpen = true
	m, _ = m.update(search.SelectMsg{Path: other, Line: 3})
	covWaitLua(t, m, `return vim.api.nvim_buf_get_name(0) .. ':' .. vim.fn.line('.')`, other+":3")
	m.searchOpen = true
	m, _ = m.update(search.CloseMsg{})
	if m.searchOpen {
		t.Error("search close")
	}

	// The explorer: an unsupported file warns, a plain file opens.
	bin := filepath.Join(dir, "blob.bin")
	_ = os.WriteFile(bin, []byte{0, 1, 2, 0, 0, 0}, 0o644)
	m, _ = m.update(explorer.OpenFileMsg{Path: bin})
	if !isSupportedFile(bin) && !strings.Contains(toastText(m), "blob.bin") && toastText(m) == "" {
		t.Errorf("unsupported open gave no toast")
	}
	m.focus = FocusExplorer
	m, _ = m.update(explorer.OpenFileMsg{Path: file, KeepFocus: true})
	if m.focus != FocusExplorer {
		t.Error("KeepFocus open moved the focus")
	}

	// Tabs: switch, the tab menu, close.
	id, _ := m.nvim.EvalLuaString(`return tostring(vim.fn.bufnr('` + file + `'))`)
	n := parseInt(id)
	m.bufs = append(m.bufs, nvim.BufferInfo{ID: n, Path: file})
	m, _ = m.update(tabbar.SwitchMsg{ID: n})
	covWaitLua(t, m, `return vim.api.nvim_buf_get_name(0)`, file)
	m, _ = m.update(tabbar.TabContextMsg{ID: n, X: 30, Y: 1})
	if !m.menuOpen {
		t.Error("tab menu not open")
	}
	m, _ = m.update(menu.CloseMsg{})
	m, _ = m.update(tabbar.CloseMsg{ID: n})
	covWaitLua(t, m, `return vim.fn.buflisted(`+id+`) == 1 and '1' or '0'`, "0")

	m, _ = m.update(ErrMsg{Err: errors.New("disk on fire")})
	if m.err != "disk on fire" {
		t.Errorf("err = %q", m.err)
	}
	m, _ = m.update(ToastMsg{Level: toast.Info, Title: "only a title"})
	m, _ = m.update(ToastMsg{Level: toast.Warn, Title: "with body", Body: "the body"})
	m, _ = m.update(hoverEmptyMsg{})
	for _, w := range []string{"only a title", "the body", "No hover info"} {
		if !strings.Contains(toastText(m), w) {
			t.Errorf("toast misses %q:\n%s", w, toastText(m))
		}
	}
	m, _ = m.update(stickyContextMsg{Signature: "func main()"})
	if m.stickyContext != "func main()" {
		t.Error("sticky context")
	}
	m, _ = m.update(stickyContextMsg{})
	m, _ = m.update(scrollbarMarkersMsg{TotalLines: 9})
	if m.scrollbarTotalLines != 9 {
		t.Error("scrollbar markers")
	}
	m, _ = m.update(SettingsCloseMsg{})
	covFitsScreen(t, m, "after the messages")
}
