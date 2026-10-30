package app

import (
	"testing"
)

// JavaScript regex pieces of the find bar become nvim patterns that
// match the same text: checked in a real nvim buffer.
func TestCovCoreFindRegexInNvim(t *testing.T) {
	m := covNvimModel(t, 100, 30)
	covEditorFile(t, &m, "r.txt", "x a_1 b\tc [y] ^q ]z /p \\d\n")
	cases := []struct {
		js   string
		want string // the matched text
	}{
		{`[\w]+`, "x"},
		{`a[\w]+`, "a_1"},
		{`b[\s]c`, "b\tc"},
		{`[\]]z`, "]z"},
		{`[^]a-z ]q`, "^q"},
		{`\/p`, "/p"},
		{`\[y\]`, "[y]"},
		{`[\\]d`, `\d`},
	}
	for _, c := range cases {
		pat := jsRegexToVim(c.js)
		var got string
		if err := m.nvim.EvalLuaArgs(`local pat = ...
vim.api.nvim_win_set_cursor(0, {1, 0})
local s = vim.fn.searchpos(pat, 'cnW')
if s[1] == 0 then return '' end
local e = vim.fn.searchpos(pat, 'cneW')
return vim.fn.getline(1):sub(s[2], e[2])`, &got, pat); err != nil {
			t.Errorf("%s → %s: %v", c.js, pat, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s → %s matched %q, want %q", c.js, pat, got, c.want)
		}
	}
	for in, want := range map[string]string{
		`$1-$&-$$`: `\1-\0-$`,
		`a\tb\nc`:  "a\tb\\rc",
		`x/y&`:     `x\/y\&`,
	} {
		if got := jsReplaceToVim(in); got != want {
			t.Errorf("jsReplaceToVim(%q) = %q, want %q", in, got, want)
		}
	}
}
