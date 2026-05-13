package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"termocode/internal/toast"
)

// runTestsCmd kicks off the test runner appropriate for the project type
// and surfaces the output in the preview overlay.
//
// We sniff the project type by looking for a marker file in the cwd:
//
//   go.mod           → `go test ./...`
//   pyproject.toml   → `pytest`
//   Cargo.toml       → `cargo test`
//   package.json     → `npm test --silent`
//
// First match wins. Failure to find any marker surfaces a "no test command"
// toast rather than guessing.
func (m *Model) runTestsCmd() tea.Cmd {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	cmd, label := detectTestCommand(cwd)
	if cmd == nil {
		var toastCmd tea.Cmd
		m.toast, toastCmd = m.toast.Push(toast.Warn, "No test command detected for this project")
		return toastCmd
	}
	return func() tea.Msg {
		// CombinedOutput so failing tests don't lose their stderr message —
		// most language test runners write progress to stdout and failures
		// to stderr. Capping output at 200 KB prevents pathological cases
		// (e.g. millions of "FAIL" lines) from blowing up the preview.
		out, err := cmd.CombinedOutput()
		body := string(out)
		if len(body) > 200_000 {
			body = body[:200_000] + "\n…[truncated]…"
		}
		title := fmt.Sprintf("Tests · %s", label)
		if err != nil {
			title = "✘ " + title + " — FAIL"
		} else {
			title = "✓ " + title + " — PASS"
		}
		// Colour the output a bit: "FAIL"/"PASS"/"ok"/"---" in green/red.
		body = colourTestOutput(body)
		return PreviewMsg{Title: title, Body: body}
	}
}

// detectTestCommand returns the (*exec.Cmd, label) for the first matching
// project type. The exec.Cmd is pre-configured with the working dir.
func detectTestCommand(cwd string) (*exec.Cmd, string) {
	check := func(name string) bool {
		_, err := os.Stat(filepath.Join(cwd, name))
		return err == nil
	}
	switch {
	case check("go.mod"):
		c := exec.Command("go", "test", "./...")
		c.Dir = cwd
		return c, "go test ./..."
	case check("Cargo.toml"):
		c := exec.Command("cargo", "test")
		c.Dir = cwd
		return c, "cargo test"
	case check("pyproject.toml"), check("setup.py"), check("pytest.ini"):
		c := exec.Command("pytest")
		c.Dir = cwd
		return c, "pytest"
	case check("package.json"):
		c := exec.Command("npm", "test", "--silent")
		c.Dir = cwd
		return c, "npm test"
	}
	return nil, ""
}

// colourTestOutput adds light ANSI colouring to the output so PASS/FAIL
// jump out at a glance. Operates line-by-line; preserves any ANSI the test
// runner already emitted.
func colourTestOutput(s string) string {
	const (
		reset = "\x1b[0m"
		green = "\x1b[38;2;115;201;145m"
		red   = "\x1b[38;2;199;78;57m"
		dim   = "\x1b[38;2;138;138;138m"
	)
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		switch {
		case strings.Contains(line, "FAIL") && !strings.Contains(line, "FAIL\t0"):
			b.WriteString(red)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(strings.TrimSpace(line), "ok ") || strings.HasPrefix(strings.TrimSpace(line), "PASS"):
			b.WriteString(green)
			b.WriteString(line)
			b.WriteString(reset)
		case strings.HasPrefix(line, "---") || strings.HasPrefix(line, "==="):
			b.WriteString(dim)
			b.WriteString(line)
			b.WriteString(reset)
		default:
			b.WriteString(line)
		}
		b.WriteString("\n")
	}
	return b.String()
}
