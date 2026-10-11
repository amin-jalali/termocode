package testkit

import (
	"strings"
	"testing"
)

// Spec: Given a changed file whose name has % and # (Vim expands them to
// the current / alternate file), when the user selects it in Source Control
// and presses Enter, then exactly that file opens.
func TestSCMNvimEnterOpensFileWithSpecialChars(t *testing.T) {
	d, _ := scmNvimDriver(t, func(r *repo) {
		r.write("100%#a.txt", "special body\n")
	})
	// Keyboard path (git_actions.go): a click on a file row would open its
	// diff and move focus to the editor, so focus the panel on the section
	// header (two clicks: collapse, expand), step down to the file, Enter.
	scmClickText(d, "CHANGES")
	scmClickText(d, "CHANGES")
	until(d, func() bool { return inSection(d, "CHANGES", "100%#a.txt") })
	d.Key("down")
	d.Key("enter")
	ok := until(d, func() bool { return strings.Contains(bufLines(d), "special body") })
	name := lua(d, `return vim.api.nvim_buf_get_name(0) .. (vim.wo.diff and ' [diff]' or '')`)
	if !ok || !strings.HasSuffix(name, "/100%#a.txt") {
		t.Fatalf("Enter opened %q, want .../100%%#a.txt:%s", name, dump(d))
	}
}
