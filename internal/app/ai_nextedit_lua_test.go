package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/nvim"
)

// nesNvim opens src in a real nvim with the snippet and AI Lua loaded and
// next edit on. It returns the client and a reader of the
// termocode_ai_nes states seen so far.
func nesNvim(t *testing.T, src string) (*nvim.Client, func() []string) {
	t.Helper()
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed; skipping integration test")
	}
	withConfigDir(t)
	c, err := nvim.New()
	if err != nil {
		t.Fatalf("nvim.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if err := c.Attach(100, 30); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	var mu sync.Mutex
	var states []string
	for _, meth := range []string{"termocode_ai_changed", "termocode_ai_cancel", "termocode_ai_edit"} {
		_ = c.RegisterNotify(meth, func([]any) bool { return false })
	}
	_ = c.RegisterNotify("termocode_ai_nes", func(a []any) bool {
		mu.Lock()
		defer mu.Unlock()
		if len(a) > 0 {
			s, _ := a[0].(string)
			states = append(states, s)
		}
		return false
	})
	if err := c.ExecLua(snippetsLua); err != nil {
		t.Fatalf("snippetsLua: %v", err)
	}
	if err := c.ExecLua(aiLua); err != nil {
		t.Fatalf("aiLua: %v", err)
	}
	if err := c.ExecLua(aiNESEnableLua(true)); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "main.go")
	_ = os.WriteFile(path, []byte(src), 0o644)
	if err := c.Command("edit " + path); err != nil {
		t.Fatal(err)
	}
	return c, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), states...)
	}
}

func nesShow(t *testing.T, c *nvim.Client, s, e int, old, repl []string) bool {
	t.Helper()
	var out string
	err := c.EvalLuaArgs(`local a = _G._termocode_ai
local buf = vim.api.nvim_get_current_buf()
local s, e, old, new = ...
if a.nes_show(buf, vim.api.nvim_buf_get_changedtick(buf), s, e, old, new) then return 'y' end
return ''`, &out, s, e, old, repl)
	if err != nil {
		t.Fatal(err)
	}
	return out == "y"
}

// waitLua polls a Lua expression until it returns want.
func waitLua(t *testing.T, c *nvim.Client, expr, want string) {
	t.Helper()
	got := ""
	for i := 0; i < 200; i++ {
		got, _ = c.EvalLuaString("return " + expr)
		if got == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s = %q, want %q", expr, got, want)
}

func nesMarks(t *testing.T, c *nvim.Client) string {
	t.Helper()
	s, err := c.EvalLuaString(`local a = _G._termocode_ai
local out = {}
for _, m in ipairs(vim.api.nvim_buf_get_extmarks(0, a.nes_ns, 0, -1, { details = true })) do
  local d = m[4]
  local row = m[2]
  for _, c in ipairs(d.virt_text or {}) do out[#out + 1] = row .. ':' .. c[1] .. '@' .. c[2] end
  for _, l in ipairs(d.virt_lines or {}) do out[#out + 1] = row .. ':' .. l[1][1] .. '@' .. l[1][2] end
end
return table.concat(out, '\n')`)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const nesSrc = "package main\n\nfunc area(width, h int) int {\n\treturn w * h\n}\n"

// TestNextEditLuaTabJumpsThenApplies: the marker shows on the target line;
// the first Tab jumps there and shows the change, the second applies it
// as one undo step.
func TestNextEditLuaTabJumpsThenApplies(t *testing.T) {
	c, states := nesNvim(t, nesSrc)
	_ = c.Command("call cursor(1, 1)")
	if !nesShow(t, c, 3, 3, []string{"\treturn w * h"}, []string{"\treturn width * h"}) {
		t.Fatal("suggestion not shown")
	}
	if got := nesMarks(t, c); !strings.Contains(got, "3:  ⇥ next edit@TermocodeAIAccent") || strings.Contains(got, "return width") {
		t.Fatalf("before the jump only the marker shows:\n%s", got)
	}
	_ = c.Input("<Tab>")
	waitLua(t, c, `tostring(vim.api.nvim_win_get_cursor(0)[1])`, "4")
	got := nesMarks(t, c)
	for _, want := range []string{"3:- " + strings.Repeat(" ", 8) + "return w * h@TermocodeAINesDel", "3:+ " + strings.Repeat(" ", 8) + "return width * h@TermocodeAIGhost"} {
		if !strings.Contains(got, want) {
			t.Fatalf("after the jump the diff shows (%q):\n%s", want, got)
		}
	}
	_ = c.Input("<Tab>")
	waitLua(t, c, `vim.api.nvim_buf_get_lines(0, 3, 4, false)[1]`, "\treturn width * h")
	if got := nesMarks(t, c); got != "" {
		t.Fatalf("marks left after apply:\n%s", got)
	}
	_ = c.Input("u")
	waitLua(t, c, `vim.api.nvim_buf_get_lines(0, 3, 4, false)[1]`, "\treturn w * h")
	waitLua(t, c, `table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '|')`, strings.ReplaceAll(strings.TrimSuffix(nesSrc, "\n"), "\n", "|"))
	if s := strings.Join(states(), ","); s != "shown,jumped,gone" {
		t.Fatalf("states %q", s)
	}
}

// TestNextEditLuaEscAndStale: Esc dismisses; a stale suggestion (old text
// changed) is never shown; a change clears a shown one.
func TestNextEditLuaEscAndStale(t *testing.T) {
	c, _ := nesNvim(t, nesSrc)
	if nesShow(t, c, 3, 3, []string{"\treturn x"}, []string{"\treturn width * h"}) {
		t.Fatal("stale old text must not show")
	}
	if !nesShow(t, c, 3, 3, []string{"\treturn w * h"}, []string{"\treturn width * h"}) {
		t.Fatal("not shown")
	}
	_ = c.Input("<Esc>")
	waitLua(t, c, `tostring(_G._termocode_ai.nes ~= nil)`, "false")
	if got := nesMarks(t, c); got != "" {
		t.Fatalf("Esc left marks:\n%s", got)
	}
	// Tab with nothing shown keeps its usual job and changes no text.
	_ = c.Input("<Tab>")
	waitLua(t, c, `vim.api.nvim_buf_get_lines(0, 3, 4, false)[1]`, "\treturn w * h")

	if !nesShow(t, c, 3, 3, []string{"\treturn w * h"}, []string{"\treturn width * h"}) {
		t.Fatal("not shown again")
	}
	_ = c.Command("call setline(1, 'package foo')")
	_ = c.Command("doautocmd TextChanged")
	waitLua(t, c, `tostring(_G._termocode_ai.nes ~= nil)`, "false")
}

// TestNextEditLuaGhostTabWins: with ghost text at the caret, Tab accepts
// the ghost and leaves the next edit alone.
func TestNextEditLuaGhostTabWins(t *testing.T) {
	c, _ := nesNvim(t, nesSrc)
	_ = c.Command("call cursor(5, 1)")
	_ = c.Input("o")
	waitLua(t, c, `vim.api.nvim_get_mode().mode`, "i")
	if !nesShow(t, c, 3, 3, []string{"\treturn w * h"}, []string{"\treturn width * h"}) {
		t.Fatal("not shown")
	}
	var ok string
	if err := c.EvalLuaArgs(`local a = _G._termocode_ai
local buf = vim.api.nvim_get_current_buf()
local pos = vim.api.nvim_win_get_cursor(0)
return a.show(buf, vim.api.nvim_buf_get_changedtick(buf), pos[1] - 1, pos[2], '// hi', 0, nil) and 'y' or ''`, &ok); err != nil || ok != "y" {
		t.Fatalf("ghost not shown: %v", err)
	}
	_ = c.Input("<Tab>")
	waitLua(t, c, `vim.api.nvim_buf_get_lines(0, 5, 6, false)[1]`, "// hi")
	if got, _ := c.EvalLuaString(`return vim.api.nvim_buf_get_lines(0, 3, 4, false)[1]`); got != "\treturn w * h" {
		t.Fatalf("next edit applied by the ghost Tab: %q", got)
	}
}

// TestNextEditLuaInsertTab: in insert mode Tab jumps, then applies.
func TestNextEditLuaInsertTab(t *testing.T) {
	c, _ := nesNvim(t, nesSrc)
	_ = c.Command("call cursor(1, 1)")
	_ = c.Input("A")
	waitLua(t, c, `vim.api.nvim_get_mode().mode`, "i")
	if !nesShow(t, c, 3, 3, []string{"\treturn w * h"}, []string{"\treturn width * h"}) {
		t.Fatal("not shown")
	}
	_ = c.Input("<Tab>")
	waitLua(t, c, `tostring(vim.api.nvim_win_get_cursor(0)[1])`, "4")
	_ = c.Input("<Tab>")
	waitLua(t, c, `vim.api.nvim_buf_get_lines(0, 3, 4, false)[1]`, "\treturn width * h")
	if got, _ := c.EvalLuaString(`return vim.api.nvim_buf_get_lines(0, 0, 1, false)[1]`); got != "package main" {
		t.Fatalf("line 1 changed: %q", got)
	}
}
