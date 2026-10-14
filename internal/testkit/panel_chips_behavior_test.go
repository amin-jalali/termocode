package testkit

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
)

// Spec: Given the Output panel shows lines of a channel, when the user
// clicks the "Clear" chip in its header, then the lines are gone and the
// channel chip stays.
func TestOutputClearChip(t *testing.T) {
	for _, aiPanel := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "ai-panel"}[aiPanel], func(t *testing.T) {
			testOutputClearChip(t, aiPanel)
		})
	}
}

func testOutputClearChip(t *testing.T, aiPanel bool) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	if aiPanel { // the chat panel narrows the editor pane
		d.Key("alt+a", "esc")
	}
	d.Send(app.OutputMsg{Channel: "Tasks", Lines: []string{"first-out-line", "second-out-line"}})
	d.Action("panel:output")
	if !strings.Contains(screen(d), "second-out-line") {
		t.Fatalf("output lines not shown:\n%s", screen(d))
	}
	clickText(d, "Clear")
	if s := screen(d); strings.Contains(s, "first-out-line") || strings.Contains(s, "second-out-line") {
		t.Errorf("Clear left the lines on screen:\n%s", s)
	}
	if !strings.Contains(screen(d), "Tasks") {
		t.Errorf("Clear removed the channel chip:\n%s", screen(d))
	}
	// New output still shows after a clear.
	d.Send(app.OutputMsg{Channel: "Tasks", Lines: []string{"third-out-line"}})
	if !strings.Contains(screen(d), "third-out-line") {
		t.Errorf("output after Clear not shown:\n%s", screen(d))
	}
}

// Spec: Given a diagnostic selected in the Problems panel and no AI
// provider set up, when the user clicks the row's "✦ Fix" chip, then a
// toast says AI is not configured (and nothing else happens).
func TestProblemsFixChip(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	lua(d, `local ns = vim.api.nvim_create_namespace('chiptest')
vim.diagnostic.set(ns, 0, {{lnum = 1, col = 0, message = 'chip-test-problem', severity = vim.diagnostic.severity.ERROR}})
return ''`)
	d.Action("panel:problems")
	if !d.WaitFor(func() bool { return strings.Contains(screen(d), "chip-test-problem") }, 5*time.Second) {
		t.Fatalf("diagnostic not in Problems:\n%s", screen(d))
	}
	clickText(d, "chip-test-problem") // select the row: its Fix chip shows
	if !d.WaitFor(func() bool { return strings.Contains(screen(d), "✦ Fix") }, 3*time.Second) {
		t.Fatalf("no Fix chip on the selected row:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "AI is not configured") {
		t.Fatalf("toast before the click:\n%s", screen(d))
	}
	clickText(d, "✦ Fix")
	if !d.WaitForText("AI is not configured", 3*time.Second) {
		t.Errorf("Fix chip click showed no toast:\n%s", screen(d))
	}
}
