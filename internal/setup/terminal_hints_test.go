package setup

import (
	"strings"
	"testing"
)

func hintEnv(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// Terminal R9, R12, R13, R14: Doctor rows that only the host terminal
// can fix (settings termocode never changes itself).
func TestTerminalHints(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		row  string   // the row's name ("" = no row at all)
		want []string // words the detail must hold
	}{
		{"macOS Terminal", map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, "Alt key",
			[]string{"Use Option as Meta key", "Alt+A"}},
		{"iTerm2", map[string]string{"TERM_PROGRAM": "iTerm.app"}, "Alt key",
			[]string{"Esc+", "Option"}},
		{"VS Code", map[string]string{"TERM_PROGRAM": "vscode"}, "palette key",
			[]string{"F1", "Alt+X"}},
		{"JetBrains", map[string]string{"TERMINAL_EMULATOR": "JetBrains-JediTerm"}, "palette key",
			[]string{"F1", "Alt+X"}},
		{"Konsole", map[string]string{"KONSOLE_VERSION": "230804"}, "right-to-left text",
			[]string{"Bi-Directional text rendering", "rtl"}},
		{"Linux console", map[string]string{"TERM": "linux"}, "Linux console",
			[]string{"16 colours", "no mouse"}},
		{"GNOME Terminal", map[string]string{"VTE_VERSION": "7800", "TERM": "xterm-256color"}, "", nil},
	}
	for _, c := range cases {
		rows := TerminalHints(hintEnv(c.env))
		if c.row == "" {
			if len(rows) != 0 {
				t.Errorf("%s: rows %+v, want none", c.name, rows)
			}
			continue
		}
		var found *Check
		for i := range rows {
			if rows[i].Name == c.row {
				found = &rows[i]
			}
		}
		if found == nil {
			t.Errorf("%s: no %q row in %+v", c.name, c.row, rows)
			continue
		}
		if found.Group != "terminal" || found.Status != Warn {
			t.Errorf("%s: group %q status %v, want terminal / Warn", c.name, found.Group, found.Status)
		}
		for _, w := range c.want {
			if !strings.Contains(found.Detail, w) {
				t.Errorf("%s: detail %q lacks %q", c.name, found.Detail, w)
			}
		}
	}
}

// Doctor shows the rows for the terminal it runs in.
func TestDoctorHasTerminalHints(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "vscode")
	found := false
	for _, c := range Doctor() {
		if c.Name == "palette key" {
			found = true
		}
	}
	if !found {
		t.Error("Doctor in VS Code's terminal has no palette key row")
	}
}
