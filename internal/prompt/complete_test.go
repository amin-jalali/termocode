package prompt

import (
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCompletePath(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"report-2026.txt", "report-2025.txt", "readme.md"} {
		if err := os.WriteFile(filepath.Join(dir, f), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "photos"), 0o755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		dir + "/rep":         dir + "/report-202",      // common prefix
		dir + "/report-2026": dir + "/report-2026.txt", // one match
		dir + "/ph":          dir + "/photos/",         // a folder gets "/"
		dir + "/zz":          dir + "/zz",              // no match: unchanged
		dir + "/r":           dir + "/re",
	}
	for in, want := range cases {
		if got := CompletePath(in); got != want {
			t.Errorf("CompletePath(%q) = %q, want %q", in, got, want)
		}
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		if got := CompletePath("~/"); got != "~/" && len(got) < 2 {
			t.Errorf("~/ -> %q", got)
		}
	}
}

func TestTabCompletes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "only.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	m := New("T", "L", dir+"/o")
	m.SetCompleter(CompletePath)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.value != dir+"/only.txt" {
		t.Errorf("value = %q", m.value)
	}
	// Without a completer Tab does nothing.
	m = New("T", "L", "x")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.value != "x" {
		t.Errorf("value = %q", m.value)
	}
}
