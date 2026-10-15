package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Spec (UI review #7): with the integrated terminal focused, the status
// bar gets the active shell and its folder (home shortened to ~).
func TestStatusStateTerminalFocused(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	m := Model{termOpen: true, inTerminal: true,
		terminalTabs: []terminalTab{{Shell: "bash", Cwd: "/tmp"}, {Shell: "zsh", Cwd: filepath.Join(home, "proj")}},
		terminalActiveTab: 1}
	st := m.statusState()
	if !st.TermFocused || st.TermLabel != "zsh · ~/proj" {
		t.Errorf("TermFocused=%v TermLabel=%q, want true, %q", st.TermFocused, st.TermLabel, "zsh · ~/proj")
	}
	m.inTerminal = false
	if m.statusState().TermFocused {
		t.Error("editor focused but TermFocused")
	}
}

// Before the shell reports its folder, the tab holds nvim's terminal name
// (term://DIR//PID:CMD); the label shows only DIR (no process id).
func TestTerminalStatusLabelFromTermURI(t *testing.T) {
	m := Model{terminalTabs: []terminalTab{{Shell: "sh", Cwd: "term://~/termocode//3177228:/bin/sh"}}}
	if got := m.terminalStatusLabel(); got != "sh · ~/termocode" {
		t.Errorf("label %q, want %q", got, "sh · ~/termocode")
	}
}
