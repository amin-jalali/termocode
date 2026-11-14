package testkit

import (
	"strings"
	"testing"
	"time"
)

// Spec (bench finding, PR #539): Given a file open in the editor (which
// is in insert mode: termocode is modeless), when a language server
// reports an error on a line, then the error's text shows at the end of
// that line, not only as a count in the status bar.
func TestDiagnosticTextShowsInTheEditor(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Settle(time.Second)
	lua(d, `local ns = vim.api.nvim_create_namespace('diag-inline-test')
vim.diagnostic.set(ns, 0, {{lnum = 0, col = 0, message = 'undefined: missingName', severity = vim.diagnostic.severity.ERROR}})
return ''`)
	if !d.WaitFor(func() bool { return strings.Contains(screen(d), "undefined: missingName") }, 5*time.Second) {
		t.Errorf("the error text is not in the editor:\n%s", screen(d))
	}
}
