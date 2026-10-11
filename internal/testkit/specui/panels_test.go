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

// Spec: Given a file with one error and one warning diagnostic,
// when the user opens View: Problems,
// then the panel lists both with their positions and counts, the status
// bar counts them, and the arrow keys + Enter jump to the warning's line
// without typing into the file.
func TestProblemsPanelListsAndJumps(t *testing.T) {
	d := diagDriver(t, nil)
	mustSee(t, d, "E 1  W 1")
	d.Palette("problems-panel")
	mustSee(t, d, "spec warning there")
	s := screen(d)
	for _, want := range []string{"✘ 1   ⚠ 1", "spec error here", "[Ln 1, Col 1]", "[Ln 3, Col 5]"} {
		if !strings.Contains(s, want) {
			t.Errorf("Problems lacks %q:\n%s", want, s)
		}
	}
	d.Key("down", "down", "enter")
	mustSee(t, d, "Ln 3, Col 5")
	if strings.Contains(lineWith(d, "a.txt"), "●") {
		t.Errorf("the keys edited the file:\n%s", screen(d))
	}
	d.Type("X") // focus is back in the editor
	mustSee(t, d, "threXe four")
}

// Spec: Given a tasks.json task with the $go problem matcher whose output
// names a file and line,
// when the user runs it (Tasks: Run Task),
// then the problem shows in the Problems panel at that line.
func TestTaskProblemMatcherFeedsProblems(t *testing.T) {
	tasksJSON := `{"tasks": [{"label": "speclint", "command": "echo 'b.go:3:5: spec task problem'; exit 1", "problemMatcher": "$go"}]}`
	d := diagDriver(t, map[string]string{".termocode/tasks.json": tasksJSON, "b.go": "package b\n\nvar x = 1\n"})
	d.Palette("tasks-run")
	mustSee(t, d, "speclint")
	d.Type("speclint")
	d.Key("enter")
	mustSee(t, d, "spec task problem")
	d.Palette("problems-panel")
	mustSee(t, d, "b.go  1")
	d.Key("end", "enter") // the last row: the task's problem
	mustSee(t, d, "var x = 1")
	d.WaitFor(func() bool { return cursorLine(d) == "Ln 3" }, wait)
	if got := activeTab(d) + " " + cursorLine(d); got != "b.go Ln 3" {
		t.Errorf("Enter on the task problem went to %q, want b.go Ln 3", got)
	}
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
