package app

import (
	"strings"
	"testing"
)

// Spec (WEB-F1): Copy Link puts the one-time link only on the system
// clipboard (or OSC 52), never in an nvim register, where it would stay
// after it was used and could be pasted into a file or a chat. A copy
// that reaches no clipboard says so.
func TestWebCopyLinkNeverInNvimRegister(t *testing.T) {
	m := aiTestModel(t)
	c := aiTestNvim(t, &m)
	// A fake + provider: the test never reaches the real clipboard, and
	// it sees what nvim would have copied.
	if err := c.ExecLua(`vim.g.clipboard = { name = 'test',
  copy = { ['+'] = function(l) _G.plus = table.concat(l, '\n') end, ['*'] = function() end },
  paste = { ['+'] = function() return {} end, ['*'] = function() return {} end } }`); err != nil {
		t.Fatal(err)
	}
	var got []string
	restore := SetClipboardSink(func(s string) { got = append(got, s) })
	defer restore()
	m.webPanel = &webPanelState{up: true, port: 4242, plain: "sec" + "ret-one"}
	m.webDeliverLink(false, true)
	if len(got) != 1 || !strings.Contains(got[0], "#s=secret-one") {
		t.Fatalf("system clipboard got %q", got)
	}
	var regs string
	if err := c.ExecLuaResult(`return vim.fn.getreg('"') .. '|' .. vim.fn.getreg('0') .. '|' .. (_G.plus or '')`, &regs); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(regs, "secret-one") {
		t.Fatalf("the link is in an nvim register: %q", regs)
	}
}
