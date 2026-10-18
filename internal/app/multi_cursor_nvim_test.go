package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// U17: termocode keeps the editor in Insert mode ("type to insert"), so
// the multi-cursor keys must work there and in Normal mode. Before the fix
// Ctrl+D in Insert mode was Vim's "remove one indent", and Ctrl+Alt+Up /
// Down did nothing in any mode (a <C-M-…> alias mapped them to themselves).
// Runs the real setup chunk in a headless nvim with the real plugin.
func TestMultiCursorKeysWorkInEveryMode(t *testing.T) {
	if _, err := exec.LookPath("nvim"); err != nil {
		t.Skip("nvim not installed")
	}
	plugin := os.Getenv("TERMOCODE_TEST_VM_PLUGIN")
	if plugin == "" && os.Getenv(pluginsDirEnv) != "" {
		plugin = filepath.Join(os.Getenv(pluginsDirEnv), "vim-visual-multi")
	}
	if plugin == "" {
		home, _ := os.UserHomeDir()
		plugin = filepath.Join(home, ".local", "share", "termocode", "plugins", "vim-visual-multi")
	}
	if st, err := os.Stat(filepath.Join(plugin, "plugin", "visual-multi.vim")); err != nil || st.IsDir() {
		t.Skip("vim-visual-multi not cloned (" + plugin + ")")
	}
	dir := t.TempDir()
	setup := filepath.Join(dir, "setup.lua")
	if err := os.WriteFile(setup, []byte(multiCursorSetupLua(plugin)), 0o600); err != nil {
		t.Fatal(err)
	}
	type tcase struct {
		name, keys string
		minRegions int
		insert     bool
	}
	var cases []tcase
	for _, insert := range []bool{true, false} {
		mode := "normal"
		if insert {
			mode = "insert"
		}
		cases = append(cases,
			tcase{mode + " ctrl+d next match", "<C-d><C-d>", 2, insert},
			tcase{mode + " ctrl+alt+d select all", "<C-A-d>", 3, insert},
			tcase{mode + " ctrl+alt+down add cursor", "<C-A-Down>", 2, insert},
			tcase{mode + " ctrl+alt+up add cursor", "<C-A-Up>", 2, insert},
		)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(dir, "t.txt")
			out := filepath.Join(dir, "out.txt")
			_ = os.Remove(out)
			if err := os.WriteFile(file, []byte("    foo bar foo\n    foo end\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			probe := `lua vim.defer_fn(function()
  vim.api.nvim_input('` + tc.keys + `')
  vim.defer_fn(function()
    local s = vim.b.VM_Selection
    local n = s and #(s.Regions or {}) or 0
    vim.fn.writefile({tostring(n), vim.fn.getline(1)}, '` + out + `')
    vim.cmd('qa!')
  end, 400)
end, 200)`
			cmd := exec.Command("nvim", "--headless", "-u", "NONE", "-i", "NONE", "-n",
				"-c", "luafile "+setup, file,
				"-c", "lua vim.api.nvim_win_set_cursor(0, {"+map[bool]string{true: "2", false: "1"}[strings.Contains(tc.name, "+up")]+", 5})",
				"-c", map[bool]string{true: "startinsert", false: "echo"}[tc.insert], "-c", probe)
			done := make(chan error, 1)
			go func() { done <- cmd.Run() }()
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatal("nvim timed out")
			}
			b, err := os.ReadFile(out)
			if err != nil {
				t.Fatalf("no result: %v", err)
			}
			got := strings.Split(strings.TrimSpace(string(b)), "\n")
			if len(got) != 2 {
				t.Fatalf("result %q", b)
			}
			// The keys must add cursors, never edit text (Insert-mode Ctrl+D
			// used to remove the indent).
			if got[1] != "    foo bar foo" {
				t.Errorf("line changed to %q (the key edited text instead of adding cursors)", got[1])
			}
			if n := got[0]; n < "0" || len(n) != 1 || int(n[0]-'0') < tc.minRegions {
				t.Errorf("regions = %s, want at least %d", n, tc.minRegions)
			}
		})
	}
}
