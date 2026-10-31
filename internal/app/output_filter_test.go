package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// MCPX-30: a channel can show only the lines a filter keeps, with a note
// row that says so; nil shows every line again. Lines still arrive.
func TestOutputFilter(t *testing.T) {
	s := newOutputStore()
	s.Append("MCP", "10:00:00  a: started", "10:00:01  b: started", "10:00:02  a: failed")
	s.SetActive("MCP")
	s.SetFilter("MCP", func(l string) bool { return strings.Contains(l, "  a: ") }, "only a")
	m := &Model{output: s}
	rows := renderOutputTab(m, 80, 10)
	text := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(text, "only a") || strings.Contains(text, "b: started") || !strings.Contains(text, "a: failed") {
		t.Errorf("filtered view:\n%s", text)
	}
	if got := len(s.Lines("MCP")); got != 3 {
		t.Errorf("filter dropped stored lines: %d", got)
	}
	s.SetFilter("MCP", nil, "")
	text = ansi.Strip(strings.Join(renderOutputTab(m, 80, 10), "\n"))
	if !strings.Contains(text, "b: started") || strings.Contains(text, "only a") {
		t.Errorf("after clear:\n%s", text)
	}
}
