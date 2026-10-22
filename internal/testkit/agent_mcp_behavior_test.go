package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
)

// Spec (MS-4): Given a fake `claude` on PATH, when the user starts
// "Agent: Start Claude Code", then it runs with `--mcp-config <file>`;
// the file names `termocode mcp --dir <workspace>`; closing the agent's
// tab removes the file. The user's own ~/.claude.json is not touched.
func TestAgentStartGivesMCPConfig(t *testing.T) {
	needBash(t)
	bin := t.TempDir()
	rec := filepath.Join(bin, "argv.txt")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + rec + "'\nsleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	d := editorDriver(t, nil)
	open(d, "a.txt")
	own := filepath.Join(os.Getenv("HOME"), ".claude.json")
	openBashTerminal(d) // a shell tab stays when the agent's tab closes
	d.Palette("agent-start-claude")
	var argv []string
	if !d.WaitFor(func() bool {
		b, err := os.ReadFile(rec)
		argv = strings.Split(strings.TrimSpace(string(b)), "\n")
		return err == nil && len(argv) == 2
	}, 10*time.Second) {
		t.Fatalf("fake claude did not run:\n%s", screen(d))
	}
	if argv[0] != "--mcp-config" {
		t.Fatalf("argv %q", argv)
	}
	cfg, err := os.ReadFile(argv[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg), `"mcp", "--dir", "`+d.Dir+`"`) {
		t.Errorf("config:\n%s", cfg)
	}
	if fi, _ := os.Stat(argv[1]); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %o", fi.Mode().Perm())
	}
	if _, err := os.Stat(own); err == nil {
		t.Error("the user's own claude config was created")
	}
	waitTerm(d, 1, "agent tab", func(s app.TerminalTabState) bool { return s.Agent == "claude" })
	d.Palette("terminal-close-tab") // the agent's tab is the active one
	if !d.WaitFor(func() bool { _, err := os.Stat(argv[1]); return err != nil }, 5*time.Second) {
		t.Errorf("config file left after the tab closed: %v", termTabs(d))
	}
}
