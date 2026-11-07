package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEBP-1): the web editor's Vim mode follows the user's own
// mappings. editor.vim_maps gives the laptop editor's global mappings
// of normal, visual, operator-pending and insert mode that the web can
// run: a key sequence on the right side. Lua callbacks, expressions,
// <Plug>/<SID> names, script-local ones, Lua commands, the editor's own
// defaults and keys a browser never gives the editor (Alt, the mouse)
// are left out; at most MaxVimMaps, by mode and keys.
func TestWebVimMapsKeepsWhatTheWebCanRun(t *testing.T) {
	raw := []nvimVimMap{
		{Mode: "n", LHS: "<Space>w", RHS: ":w<CR>", Sid: -8, Noremap: 1},
		{Mode: "i", LHS: "jk", RHS: "<Esc>", Sid: 3, Noremap: 1},
		{Mode: "x", LHS: "<", RHS: "<gv", Sid: 3},
		{Mode: "n", LHS: "Y", RHS: "y$", Sid: -8, Noremap: 1, Desc: ":help Y-default"},
		{Mode: "n", LHS: "<C-L>", RHS: "<Cmd>nohlsearch<CR>", Sid: 0, Noremap: 1},
		{Mode: "n", LHS: "<Space>e", RHS: "<Cmd>lua vim.diagnostic.open_float()<CR>", Sid: -8},
		{Mode: "n", LHS: "<Space>l", RHS: ":lua print(1)<CR>", Sid: -8},
		{Mode: "n", LHS: "gx", RHS: "<Plug>NetrwBrowseX", Sid: 5},
		{Mode: "n", LHS: "zq", RHS: "<SNR>12_quit()", Sid: 5},
		{Mode: "n", LHS: "zr", RHS: "x", Sid: 5, Script: 1},
		{Mode: "n", LHS: "ze", RHS: "Expand()", Sid: 5, Expr: 1},
		{Mode: "n", LHS: "<M-Down>", RHS: ":m .+1<CR>==", Sid: -8, Noremap: 1},
		{Mode: "n", LHS: "<LeftDrag>", RHS: "<Nop>", Sid: -8, Noremap: 1},
		{Mode: "o", LHS: "", RHS: "x", Sid: 5},
		{Mode: "n", LHS: "Q", RHS: "@q", Sid: 5},
		{Mode: "n", LHS: "zz", RHS: strings.Repeat("x", webproto.MaxVimMapRHS+1), Sid: 5},
	}
	got := webVimMaps(raw)
	want := []webproto.VimMap{
		{Mode: "i", LHS: "jk", RHS: "<Esc>", Noremap: true},
		{Mode: "n", LHS: "<Space>w", RHS: ":w<CR>", Noremap: true},
		{Mode: "n", LHS: "Q", RHS: "@q"},
		{Mode: "x", LHS: "<", RHS: "<gv"},
	}
	if len(got) != len(want) {
		t.Fatalf("maps %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("map %d is %+v, want %+v", i, got[i], want[i])
		}
	}
	var many []nvimVimMap
	for i := 0; i < webproto.MaxVimMaps+20; i++ {
		many = append(many, nvimVimMap{Mode: "n", LHS: "<Space>" + strings.Repeat("a", i%50+1) + string(rune('A'+i/50)), RHS: "x", Sid: 3})
	}
	if n := len(webVimMaps(many)); n != webproto.MaxVimMaps {
		t.Errorf("%d maps, want at most %d", n, webproto.MaxVimMaps)
	}
}

// Without the laptop's editor the list is empty, never an error.
func TestWebVimMapsWithoutTheEditor(t *testing.T) {
	r := newWebFilesRig(t)
	var list webproto.VimMapList
	webResData(t, r.call(webproto.OpEditorVimMaps, &webproto.EmptyArgs{}), &list)
	if list.Items == nil || len(list.Items) != 0 {
		t.Fatalf("list %+v", list)
	}
}

// Spec (WEBP-1): editor.vim_buf_maps gives the mappings of one open
// file's buffer in the laptop's editor: the ones made for that file
// (<buffer>) and the ones its language's settings make (a FileType
// autocmd, an ftplugin), kept as for editor.vim_maps. A file not open
// on the web is not found.
func TestWebVimBufMapsPerFileAndLanguage(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.lua(`vim.api.nvim_create_autocmd('FileType', { pattern = 'go', callback = function(a)
  vim.keymap.set('n', '<Space>t', ':w<CR>', { buffer = a.buf })
  vim.keymap.set('n', '<Space>q', function() end, { buffer = a.buf })
end })
vim.keymap.set('n', '<Space>g', 'dd')`, nil)
	r.write("a.go", "package a\n")
	r.write("b.txt", "b\n")
	a := webBufOpen(t, r, r.c, "a.go")
	b := webBufOpen(t, r, r.c, "b.txt")
	r.lua(`local a, b = ...
vim.bo[a].filetype = 'go'
vim.keymap.set('i', 'jj', '<Esc>', { buffer = b, noremap = true })`, nil, a.Buf, b.Buf)

	var la, lb webproto.VimMapList
	webResData(t, r.call(webproto.OpEditorVimBufMaps, &webproto.BufArgs{Buf: a.Buf}), &la)
	if len(la.Items) != 1 || la.Items[0] != (webproto.VimMap{Mode: "n", LHS: "<Space>t", RHS: ":w<CR>", Noremap: true}) {
		t.Errorf("the Go file's maps %+v", la.Items)
	}
	webResData(t, r.call(webproto.OpEditorVimBufMaps, &webproto.BufArgs{Buf: b.Buf}), &lb)
	if len(lb.Items) != 1 || lb.Items[0] != (webproto.VimMap{Mode: "i", LHS: "jj", RHS: "<Esc>", Noremap: true}) {
		t.Errorf("the text file's maps %+v", lb.Items)
	}
	if f := r.call(webproto.OpEditorVimBufMaps, &webproto.BufArgs{Buf: 999}); webErrCode(f) != "not_found" {
		t.Errorf("a file not open: %v", f)
	}
}
