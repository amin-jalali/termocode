package app

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// CMP-67 Full Terminal Use: the agent drives a program inside the
// visible terminal (psql, gdb, a REPL), and the user takes over by typing.

func TestAITermPromptKind(t *testing.T) {
	for line, want := range map[string]string{
		"postgres=# ":                 "psql",
		"shop=> ":                     "psql",
		"shop-# ":                     "psql",
		"(gdb) ":                      "gdb",
		"(lldb) ":                     "lldb",
		"(Pdb) ":                      "pdb",
		">>> ":                        "python",
		"... ":                        "python",
		"mysql> ":                     "mysql",
		"MariaDB [shop]> ":            "mysql",
		"sqlite> ":                    "sqlite",
		"irb(main):001:0> ":           "irb",
		"iex(1)> ":                    "iex",
		"ghci> ":                      "ghci",
		"127.0.0.1:6379> ":            "redis",
		"> ":                          "repl",
		"user@host:~/src$ ":           "",
		"~/src % ":                    "",
		"":                            "",
		"Running 12 tests...":         "",
		"SELECT 1 FROM t WHERE a > 2": "",
	} {
		if got := aiTermPromptKind(line); got != want {
			t.Errorf("%q: %q, want %q", line, got, want)
		}
	}
}

func TestAITermAsksSecret(t *testing.T) {
	for line, want := range map[string]bool{
		"Password for user postgres: ":       true,
		"Enter passphrase for key '/k/id': ": true,
		"[sudo] password for amin: ":         true,
		"Password:":                          true,
		"Enter PIN: ":                        true,
		"postgres=# ":                        false,
		"SELECT password FROM users;":        false,
		"password reset done":                false,
	} {
		if got := aiTermAsksSecret(line); got != want {
			t.Errorf("%q: %v, want %v", line, got, want)
		}
	}
}

func TestAITermKeyBytes(t *testing.T) {
	for spec, want := range map[string]string{
		"ctrl-c":           "\x03",
		"Ctrl+D":           "\x04",
		"enter":            "\r",
		"up down":          "\x1b[A\x1b[B",
		"left, right":      "\x1b[D\x1b[C",
		"tab esc":          "\t\x1b",
		"backspace space":  "\x7f ",
		"ctrl-l ctrl-z":    "\x0c\x1a",
		"home end":         "\x1b[H\x1b[F",
		"pageup pagedown":  "\x1b[5~\x1b[6~",
		"delete":           "\x1b[3~",
		"ctrl-\\":          "\x1c",
		"  ctrl-c  enter ": "\x03\r",
	} {
		got, err := aiTermKeyBytes(spec)
		if err != nil || got != want {
			t.Errorf("%q: %q %v, want %q", spec, got, err, want)
		}
	}
	for _, bad := range []string{"", "   ", "f13", "ctrl-1", "x"} {
		if _, err := aiTermKeyBytes(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	if _, err := aiTermKeyBytes("hyper"); err == nil || !strings.Contains(err.Error(), "ctrl-c") {
		t.Errorf("unknown key error should list the keys: %v", err)
	}
}

// A shell running psql is "busy" forever; the read must stop once psql
// shows its prompt again, not wait the full wait_ms.
func TestAITermReadDone(t *testing.T) {
	q := aiTermQuietIdle
	cases := []struct {
		name    string
		started bool
		busy    int
		quiet   time.Duration
		tail    string
		until   string
		want    bool
		why     string
	}{
		{"not started", false, 0, time.Hour, "$ ", "", false, ""},
		{"idle shell", true, 0, q, "$ ", "", true, "idle"},
		{"idle but noisy", true, 0, q / 2, "$ ", "", false, ""},
		{"unknown state quiet", true, -1, aiTermQuietUnknown, "x", "", true, "quiet"},
		{"psql prompt", true, 1, q, " count\n-------\n     3\n(1 row)\n\nshop=# ", "", true, "prompt"},
		{"gdb prompt still printing", true, 1, q / 2, "(gdb) ", "", false, ""},
		{"busy no prompt", true, 1, time.Hour, "Running…", "", false, ""},
		{"password prompt", true, 1, q, "Password for user postgres: ", "", true, "password"},
		{"until matches", true, 1, 0, "Listening on :8080", `Listening on`, true, "until"},
		{"until not yet", true, 0, time.Hour, "$ ", `Listening on`, false, ""},
		{"until sees a password prompt", true, 1, q, "Password: ", `ready`, true, "password"},
	}
	for _, c := range cases {
		var re *regexp.Regexp
		if c.until != "" {
			re = regexp.MustCompile(c.until)
		}
		done, why := aiTermReadDone(c.started, c.busy, c.quiet, c.tail, re)
		if done != c.want || why != c.why {
			t.Errorf("%s: %v %q, want %v %q", c.name, done, why, c.want, c.why)
		}
	}
}

func driveModel() *Model {
	return &Model{termOpen: true, panelActive: panelKindTerminal,
		terminalTabs: []terminalTab{{BufID: 5, Name: "psql"}, {BufID: 7, Name: "zsh"}}}
}

func TestAITermTakeOverByTyping(t *testing.T) {
	m := driveModel()
	if err := m.aiTermDriveCheck(5); err != nil {
		t.Fatalf("a tab the agent never typed in: %v", err)
	}
	_ = m.aiTermDriveNote(5)
	if !m.aiTermDriving(5) {
		t.Fatal("tab 1 should be driven by the agent")
	}
	m.terminalActiveTab = 1
	_ = m.aiTermNoteUserKey() // the user types in another tab
	if err := m.aiTermDriveCheck(5); err != nil {
		t.Fatalf("typing in tab 2 took over tab 1: %v", err)
	}
	m.terminalActiveTab = 0
	_ = m.aiTermNoteUserKey()
	err := m.aiTermDriveCheck(5)
	if err == nil || !strings.Contains(err.Error(), "took over") || !strings.Contains(err.Error(), "Hand Back") {
		t.Fatalf("after the user typed: %v", err)
	}
	if m.aiTermDriving(5) {
		t.Error("a taken-over tab is not driven")
	}
	_ = m.aiTermDriveNote(5) // the agent can not take it back by itself
	if m.aiTermDriveCheck(5) == nil {
		t.Error("the agent's own send cleared the take-over")
	}
}

func TestAITermHandBackAndTakeOverCommands(t *testing.T) {
	m := driveModel()
	_ = m.aiTermDriveNote(5)
	if _, ok := m.runTermDriveAction("terminal-ai-take-over"); !ok {
		t.Fatal("take-over id not handled")
	}
	if m.aiTermDriveCheck(5) == nil {
		t.Fatal("take over did not stop the agent")
	}
	if _, ok := m.runTermDriveAction("terminal-ai-hand-back"); !ok {
		t.Fatal("hand-back id not handled")
	}
	if err := m.aiTermDriveCheck(5); err != nil {
		t.Fatalf("after hand back: %v", err)
	}
	if _, ok := m.runTermDriveAction("terminal-new-tab"); ok {
		t.Error("other ids must fall through")
	}
	// Closed tabs are forgotten.
	m.terminalTabs = m.terminalTabs[1:]
	m.aiTermDrivePrune()
	if len(m.aiTermDrive) != 0 {
		t.Errorf("closed tab kept: %v", m.aiTermDrive)
	}
}

func TestAITermDriveIDsHavePolicyAndPalette(t *testing.T) {
	ids := map[string]bool{}
	for _, it := range paletteItems() {
		ids[it.ID] = true
	}
	for _, id := range []string{"terminal-ai-take-over", "terminal-ai-hand-back"} {
		if !ids[id] {
			t.Errorf("%s not in the palette", id)
		}
		p, ok := aiActionPolicies[id]
		if !ok || p.Gate != aiGateNever {
			t.Errorf("%s: policy %+v, want never (only the user decides who types)", id, p)
		}
	}
}
