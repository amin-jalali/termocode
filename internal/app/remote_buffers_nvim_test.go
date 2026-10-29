package app

import (
	"os"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// remoteNvim is a real nvim with remote buffers on for a fake container
// workspace /app.
func remoteNvim(t *testing.T, mod ...func(*fakedocker.Container)) (*nvim.Client, *fakedocker.Container) {
	t.Helper()
	ws, _, _, c := fakeContainerWS(t, mod...)
	n, err := nvim.New()
	if err != nil {
		t.Skipf("nvim not available: %v", err)
	}
	t.Cleanup(func() { _ = n.Close() })
	if err := n.Attach(80, 24); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	_ = n.Command("set cmdheight=0")
	if err := startRemoteBuffers(n, ws); err != nil {
		t.Fatal(err)
	}
	return n, c
}

func remoteBufText(t *testing.T, n *nvim.Client) string {
	t.Helper()
	s, err := n.EvalLuaString(`return table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func remoteEval(t *testing.T, n *nvim.Client, lua string) string {
	t.Helper()
	s, err := n.EvalLuaString(lua)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRemoteBufferOpensTheContainerFile(t *testing.T) {
	n, _ := remoteNvim(t)
	if err := n.Command("edit /app/main.go"); err != nil {
		t.Fatal(err)
	}
	if got := remoteBufText(t, n); got != "package main\n\nfunc main() {}" {
		t.Fatalf("buffer = %q", got)
	}
	if got := remoteEval(t, n, `return vim.api.nvim_buf_get_name(0) .. ' ' .. vim.bo.filetype .. ' ' .. tostring(vim.bo.modified) .. ' ' .. vim.bo.buftype`); got != "/app/main.go go false " {
		t.Fatalf("buffer state = %q", got)
	}
	// Loading is not an edit the user can undo.
	_ = n.Command("silent! undo")
	if got := remoteBufText(t, n); got != "package main\n\nfunc main() {}" {
		t.Fatalf("undo emptied the loaded buffer: %q", got)
	}
}

func TestRemoteBufferSavesToTheContainer(t *testing.T) {
	n, c := remoteNvim(t)
	_ = n.Command("edit /app/main.go")
	_ = n.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'package main', '', 'func main() { println() }'})`)
	if err := n.Command("silent w"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if b, _ := os.ReadFile(c.Local("/app/main.go")); string(b) != "package main\n\nfunc main() { println() }\n" {
		t.Fatalf("container file = %q", b)
	}
	if got := remoteEval(t, n, `return tostring(vim.bo.modified)`); got != "false" {
		t.Fatalf("modified after save = %s", got)
	}
}

func TestRemoteBufferNewFileIsCreatedOnSave(t *testing.T) {
	n, c := remoteNvim(t)
	if err := n.Command("edit /app/cmd/new.go"); err != nil {
		t.Fatal(err)
	}
	if got := remoteBufText(t, n); got != "" {
		t.Fatalf("new buffer = %q", got)
	}
	_ = n.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'package cmd'})`)
	if err := n.Command("silent w"); err != nil {
		t.Fatalf("save: %v", err)
	}
	if b, _ := os.ReadFile(c.Local("/app/cmd/new.go")); string(b) != "package cmd\n" {
		t.Fatalf("container file = %q", b)
	}
}

func TestRemoteBufferKeepsWindowsLineEnds(t *testing.T) {
	n, c := remoteNvim(t, func(c *fakedocker.Container) {
		c.WriteFile(t, "/app/win.txt", []byte("a\r\nb\r\n"), 0o644)
	})
	_ = n.Command("edit /app/win.txt")
	if got := remoteBufText(t, n); got != "a\nb" {
		t.Fatalf("buffer = %q", got)
	}
	_ = n.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'a', 'b', 'c'})`)
	if err := n.Command("silent w"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(c.Local("/app/win.txt")); string(b) != "a\r\nb\r\nc\r\n" {
		t.Fatalf("container file = %q", b)
	}
}

func TestRemoteBufferEmptyFileStaysEmpty(t *testing.T) {
	n, c := remoteNvim(t, func(c *fakedocker.Container) {
		c.WriteFile(t, "/app/empty.txt", nil, 0o644)
	})
	_ = n.Command("edit /app/empty.txt")
	if err := n.Command("silent w"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(c.Local("/app/empty.txt")); len(b) != 0 {
		t.Fatalf("container file = %q", b)
	}
}

// A failed save keeps the editor's text and says why.
func TestRemoteBufferFailedSaveKeepsTheText(t *testing.T) {
	n, c := remoteNvim(t)
	_ = n.Command("edit /app/main.go")
	_ = n.ExecLua(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {'my edit'})`)
	c.Remove()
	err := n.Command("silent w")
	if err == nil || !strings.Contains(err.Error(), "api-1 is not running") {
		t.Fatalf("save error = %v", err)
	}
	if got := remoteBufText(t, n); got != "my edit" {
		t.Fatalf("buffer after failed save = %q", got)
	}
	if got := remoteEval(t, n, `return tostring(vim.bo.modified)`); got != "true" {
		t.Fatalf("modified after failed save = %s", got)
	}
}
