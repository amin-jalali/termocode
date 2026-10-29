package editor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// realNvim starts an embedded nvim that never reads the user's config.
func realNvim(t *testing.T) *nvim.Client {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	base := t.TempDir()
	for _, v := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(v, filepath.Join(base, strings.ToLower(v)))
	}
	c, err := nvim.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(40, 10); err != nil {
		t.Fatal(err)
	}
	return c
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func bufText(c *nvim.Client) string {
	s, _ := c.BufferContent()
	return s
}

func TestEditorTypesPastesAndSaves(t *testing.T) {
	c := realNvim(t)
	m := New(c)
	path := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := m.Open(path); err != nil || m.Path() != path {
		t.Fatalf("open: %v path %q", err, m.Path())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("A")}) // append at line end
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" <world>\\")})
	// SGR mouse fragments never reach the buffer.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<35;70;19M")})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("35;7"), Alt: true})
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	eventually(t, "typed text", func() bool { return bufText(c) == "hello <world>\\" })

	// A bracketed paste goes in as one block.
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("\nline two\nline three"), Paste: true})
	eventually(t, "pasted text", func() bool { return strings.Count(bufText(c), "\n") == 2 })

	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	eventually(t, "saved file", func() bool {
		b, _ := os.ReadFile(path)
		return strings.Contains(string(b), "line three")
	})
}

func TestEditorMouseClickAndDrag(t *testing.T) {
	c := realNvim(t)
	m := New(c)
	m.SetSize(40, 10)
	path := filepath.Join(t.TempDir(), "m.txt")
	os.WriteFile(path, []byte("abcdef\nghijkl\nmnopqr\n"), 0o644)
	if err := m.Open(path); err != nil {
		t.Fatal(err)
	}
	m, _ = m.HandleMouse(2, 1, tea.MouseLeft)
	m, _ = m.HandleMouse(4, 2, tea.MouseLeft)   // drag
	m, _ = m.HandleMouse(4, 2, tea.MouseMotion) // same cell: dropped
	m, _ = m.HandleMouse(5, 2, tea.MouseMotion) // drag on
	m, _ = m.HandleMouse(5, 2, tea.MouseRelease)
	eventually(t, "visual selection", func() bool {
		mode, _, _ := c.Mode()
		return mode == "v"
	})
	pos := func() string {
		s, _ := c.EvalLuaString(`local p = vim.api.nvim_win_get_cursor(0); return p[1] .. ':' .. p[2]`)
		return s
	}
	eventually(t, "cursor at drag end", func() bool { return pos() == "3:5" })
	// Motion with no button down is ignored; wheel and other buttons pass.
	before := m
	m, _ = m.HandleMouse(0, 0, tea.MouseMotion)
	if m != before {
		t.Fatal("motion without a press changed state")
	}
	m, _ = m.HandleMouse(0, 0, tea.MouseUnknown)
	m, _ = m.HandleMouse(0, 0, tea.MouseWheelDown)
	m, _ = m.HandleMouse(0, 0, tea.MouseWheelUp)
	m, _ = m.HandleMouse(0, 0, tea.MouseMiddle)
	// The right button reaches nvim as a right press: nvim opens its
	// popup menu and waits for a choice.
	m, _ = m.HandleMouse(0, 0, tea.MouseRight)
	eventually(t, "right click popup", func() bool {
		_, blocking, _ := c.Mode()
		return blocking
	})
}

func TestEditorWithoutNvim(t *testing.T) {
	m := New(nil)
	if m.Init() != nil {
		t.Fatal("Init has nothing to do")
	}
	if got, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}); cmd != nil || got.Path() != "" {
		t.Fatal("key without nvim")
	}
	if _, cmd := m.HandleMouse(1, 1, tea.MouseLeft); cmd != nil {
		t.Fatal("mouse without nvim")
	}
	if err := m.Open("/x/y.go"); err != nil || m.Path() != "/x/y.go" {
		t.Fatal("open without nvim keeps the path")
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if m.View(true) != "" {
		t.Fatal("unsized view must be empty")
	}
	m.SetMeta("/a.go", true, "go")
	m.SetDiagnostics(2, 5)
	if !m.IsDirty() || m.Lang() != "go" || m.Errors() != 2 || m.Warnings() != 5 {
		t.Fatal("meta not kept")
	}
	m.MarkSaved()
	if m.IsDirty() {
		t.Fatal("MarkSaved")
	}
	m.ApplyRedraw([][]any{{"grid_resize", []any{1, 10, 4}}, {"grid_cursor_goto", []any{1, 2, 3}}})
	if cur := m.Cursor(); cur.Line != 2 || cur.Col != 3 || m.LineCount() != 4 {
		t.Fatalf("cursor %+v lines %d", cur, m.LineCount())
	}
}

func TestMouseFragmentHelpers(t *testing.T) {
	if isMouseCodeRunes(nil) || isMouseCodeRunes([]rune("1a")) || !isMouseCodeRunes([]rune("35;7M")) {
		t.Fatal("isMouseCodeRunes")
	}
	if isMouseFragmentEvent([]rune("[")) || isMouseFragmentEvent([]rune("ab")) || isMouseFragmentEvent([]rune("[a")) {
		t.Fatal("isMouseFragmentEvent false positives")
	}
}
