package specui

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// diagInit is a user init.lua for the embedded nvim that puts one error
// and one warning on every .txt file through vim.diagnostic (as an LSP
// would), so the Problems panel has something to show without a server.
const diagInit = `
vim.api.nvim_create_autocmd('BufReadPost', { pattern = '*.txt', callback = function(a)
  local ns = vim.api.nvim_create_namespace('spec')
  vim.schedule(function()
    vim.diagnostic.set(ns, a.buf, {
      { lnum = 0, col = 0, severity = vim.diagnostic.severity.ERROR, message = 'spec error here', source = 'spec' },
      { lnum = 2, col = 4, severity = vim.diagnostic.severity.WARN, message = 'spec warning there', source = 'spec' },
    })
  end)
end })
`

// diagDriver opens a.txt with the two diagnostics in place.
func diagDriver(t *testing.T, extra map[string]string) *testkit.Driver {
	t.Helper()
	slow(t)
	files := map[string]string{"a.txt": "one\ntwo\nthree four\nfive\n"}
	for k, v := range extra {
		files[k] = v
	}
	// The config dir is ~/.config/termocode; nvim reads ~/.config/nvim.
	d := testkit.New(t, testkit.Options{W: 120, H: 36, Nvim: true, Files: files,
		Config: map[string]string{"../nvim/init.lua": diagInit}})
	d.Action("open:a.txt")
	mustSee(t, d, "three four")
	return d
}

// Spec: Given the editor,
// when the user opens View: Output and the Debug Console,
// then each opens as a tab in the bottom panel with its empty state, and
// the panel keeps both tabs.
func TestOutputAndDebugConsoleTabs(t *testing.T) {
	d := diagDriver(t, nil)
	d.Palette("view-output")
	mustSee(t, d, "No output yet.")
	d.Palette("debug-console")
	mustSee(t, d, "No active debug session")
	bar := lineWith(d, "Debug Console")
	if !strings.Contains(bar, "Output") {
		t.Errorf("panel bar lost the Output tab: %q", bar)
	}
}
