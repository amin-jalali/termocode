package testkit

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Spec: Given a file open in nvim, when the user drags the terminal
// window's edge (a burst of size events), then each step is cheap (nvim is
// not resized and waited on per event), and once the burst stops nvim's
// grid matches the final size.
func TestWindowResizeBurstIsCheapAndSettles(t *testing.T) {
	d := New(t, Options{W: 200, H: 55, Nvim: true, Files: map[string]string{"main.go": bigGoFile()}})
	d.Action("openfile:main.go")
	d.Settle(300 * time.Millisecond)
	before := lua(d, `return vim.o.columns .. 'x' .. vim.o.lines`)

	var m tea.Model = d.Model()
	const steps = 40
	start := time.Now()
	for i := 0; i < steps; i++ {
		m, _ = m.Update(tea.WindowSizeMsg{Width: 200 - i, Height: 55})
		_ = m.View()
	}
	per := time.Since(start) / steps
	t.Logf("window resize step: %v (Update + View)", per)
	// The burst must not resize nvim step by step (each resize is a
	// synchronous round trip): its grid still has the size from before.
	if got := lua(d, `return vim.o.columns .. 'x' .. vim.o.lines`); got != before {
		t.Errorf("nvim was resized during the burst: grid %s, want %s until it ends", got, before)
	}
	// Timing is noise on a busy machine; checked only on demand.
	if os.Getenv("TERMOCODE_PERF") == "1" && per > 12*time.Millisecond {
		t.Errorf("window resize step costs %v, want < 12ms", per)
	}

	// The real driver path: the burst ends, nvim gets the final size.
	for i := 0; i < 10; i++ {
		d.Resize(150+i, 45)
	}
	d.Settle(400 * time.Millisecond)
	want := d.Model().EditorPaneSize()
	if !eventually(d, func() bool {
		return lua(d, `return vim.o.columns .. 'x' .. vim.o.lines`) == want
	}) {
		t.Fatalf("nvim grid %s after the burst, want %s", lua(d, `return vim.o.columns .. 'x' .. vim.o.lines`), want)
	}
}

// Spec: Given a file open in nvim, when the user drags the sidebar's edge,
// then each drag step is cheap (nvim is resized once, not per step), and
// after release nvim's grid matches the new editor width.
func TestSidebarDragIsCheapAndSettles(t *testing.T) {
	d := New(t, Options{W: 200, H: 55, Nvim: true, Files: map[string]string{"main.go": bigGoFile()}})
	d.Action("openfile:main.go")
	d.Settle(300 * time.Millisecond)
	before := lua(d, `return vim.o.columns .. 'x' .. vim.o.lines`)

	ex, _, ok := find(d, "▕    1  package")
	if !ok {
		t.Fatalf("no sidebar edge:\n%s", screen(d))
	}
	edge := ex
	var m tea.Model = d.Model()
	m, _ = m.Update(tea.MouseMsg{X: edge, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	const steps = 40
	start := time.Now()
	for i := 1; i <= steps; i++ {
		m, _ = m.Update(tea.MouseMsg{X: edge + i, Y: 10, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
		_ = m.View()
	}
	per := time.Since(start) / steps
	t.Logf("sidebar drag step: %v (Update + View)", per)
	if got := lua(d, `return vim.o.columns .. 'x' .. vim.o.lines`); got != before {
		t.Errorf("nvim was resized during the drag: grid %s, want %s until release", got, before)
	}
	if os.Getenv("TERMOCODE_PERF") == "1" && per > 12*time.Millisecond {
		t.Errorf("sidebar drag step costs %v, want < 12ms", per)
	}

	// Real path through the driver: drag, release, nvim follows.
	d.Drag(edge, 10, edge+30, 10)
	d.Settle(400 * time.Millisecond)
	want := d.Model().EditorPaneSize()
	if want == before {
		t.Fatalf("drag did not change the editor size (%s)", want)
	}
	if !eventually(d, func() bool {
		return lua(d, `return vim.o.columns .. 'x' .. vim.o.lines`) == want
	}) {
		t.Fatalf("nvim grid %s after the drag, want %s", lua(d, `return vim.o.columns .. 'x' .. vim.o.lines`), want)
	}
}

// bigGoFile is a Go file large enough that nvim's re-layout on resize
// (syntax, long lines) costs real time.
func bigGoFile() string {
	var b strings.Builder
	b.WriteString("package main\n\n")
	for i := 0; i < 300; i++ {
		b.WriteString("func f" + strconv.Itoa(i) + "() { println(\"a long line of code that wraps at narrow widths\") }\n")
	}
	return b.String()
}
