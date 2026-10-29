package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeTestConfig writes config.json in the test's config folder.
func writeTestConfig(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "termocode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "termocode", "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTerminalRestoreSettings(t *testing.T) {
	dir := withConfigDir(t)
	if !terminalRestoreOn() || terminalRestoreCommandsOn() {
		t.Fatal("defaults: restore on, commands off")
	}
	writeTestConfig(t, dir, `{"terminal_restore": false, "terminal_restore_commands": true}`)
	if terminalRestoreOn() || !terminalRestoreCommandsOn() {
		t.Fatal("config.json values are not read")
	}
	writeTestConfig(t, dir, `{"terminal_restore": "yes"`)
	if !terminalRestoreOn() || terminalRestoreCommandsOn() {
		t.Fatal("a broken config.json keeps the defaults")
	}
}

func TestTerminalSnapshotKeepsTabsSplitsAndFocus(t *testing.T) {
	m := &Model{termOpen: true, terminalActiveTab: 2, terminalTabs: []terminalTab{
		{Name: "api", Cwd: "/p/api", Busy: true, LastCmd: "PORT=1 npm run dev --token=abc", PaneFocus: 2, Splits: []termPane{
			{BufID: 4, Cwd: "/p/web", Dir: paneRight},
			{BufID: 5, Cwd: "/p/web/src", Dir: paneDown, Parent: 1, Busy: true, LastCmd: "/usr/bin/go test ./..."},
		}},
		{Name: "tests", Task: "test", Cwd: "/p"}, // a task tab: not a shell of the user
		{Name: "p", Cwd: "/p", LastCmd: "ls"},    // a command that ended is not running
	}}
	got := m.terminalSnapshot(false)
	want := &savedTerminals{Open: true, Active: 1, Tabs: []savedTermTab{
		{Name: "api", Focus: 2, Panes: []savedTermPane{
			{Cwd: "/p/api", Program: "npm"},
			{Cwd: "/p/web", Split: "right"},
			{Cwd: "/p/web/src", Split: "down", Parent: 1, Program: "go"},
		}},
		{Name: "p", Panes: []savedTermPane{{Cwd: "/p"}}},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	// The command line is kept only with terminal_restore_commands.
	withCmd := m.terminalSnapshot(true)
	if c := withCmd.Tabs[0].Panes[0].Cmd; c != "PORT=1 npm run dev --token=abc" {
		t.Errorf("cmd = %q", c)
	}
	if c := withCmd.Tabs[1].Panes[0].Cmd; c != "" {
		t.Errorf("a pane that runs nothing keeps no command: %q", c)
	}
}

func TestTerminalSnapshotOfHiddenAndClosedPanels(t *testing.T) {
	m := &Model{hiddenTerminalActive: 1, hiddenTerminalTabs: []terminalTab{{Name: "a", Cwd: "/a"}, {Name: "b", Cwd: "/b"}}}
	got := m.terminalSnapshot(false)
	if got == nil || got.Open || got.Active != 1 || len(got.Tabs) != 2 {
		t.Fatalf("hidden panel: %+v", got)
	}
	if got := (&Model{}).terminalSnapshot(false); got != nil {
		t.Fatalf("no terminals: %+v", got)
	}
	pending := &savedTerminals{Tabs: []savedTermTab{{Name: "later", Panes: []savedTermPane{{Cwd: "/x"}}}}}
	if got := (&Model{termRestorePending: pending}).terminalSnapshot(false); !reflect.DeepEqual(got, pending) {
		t.Fatalf("terminals not yet restored are kept as they are: %+v", got)
	}
}

func TestProgramName(t *testing.T) {
	for cmd, want := range map[string]string{
		"npm run dev":               "npm",
		"FOO=1 BAR=2 ./bin/server":  "server",
		"  /usr/local/bin/python3 ": "python3",
		"":                          "",
		"A=1":                       "",
	} {
		if got := programName(cmd); got != want {
			t.Errorf("programName(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestSessionKeepsTerminalsPerWorkspace(t *testing.T) {
	withConfigDir(t)
	layoutProject(t)
	s := loadSession()
	s.Terminals = &savedTerminals{Open: true, Tabs: []savedTermTab{{Name: "a", Panes: []savedTermPane{{Cwd: "/a"}}}}}
	saveSession(s)
	if got := loadSession().Terminals; !reflect.DeepEqual(got, s.Terminals) {
		t.Fatalf("round trip: %+v", got)
	}
}

func TestTerminalSnapshotDropsPendingCommandsWhenTheSettingIsOff(t *testing.T) {
	pending := &savedTerminals{Tabs: []savedTermTab{{Panes: []savedTermPane{{Cwd: "/x", Program: "npm", Cmd: "npm start"}}}}}
	m := &Model{termRestorePending: pending}
	if got := m.terminalSnapshot(false); got.Tabs[0].Panes[0].Cmd != "" || got.Tabs[0].Panes[0].Program != "npm" {
		t.Fatalf("got %+v", got.Tabs[0].Panes[0])
	}
	if pending.Tabs[0].Panes[0].Cmd != "npm start" {
		t.Fatal("the pending terminals must not change")
	}
}

// A command started in a restored pane ends its note.
func TestCommandStartDropsTheRestoreNote(t *testing.T) {
	m := &Model{terminalTabs: []terminalTab{{BufID: 7, Splits: []termPane{{BufID: 9}}}},
		termHints: map[int]termHint{7: {Text: "a"}, 9: {Text: "b"}}}
	m.onTermOSC(9, "\x1b]133;C;cmdline_url=ls")
	if _, ok := m.termHints[9]; ok {
		t.Error("pane 1's note should go")
	}
	if _, ok := m.termHints[7]; !ok {
		t.Error("pane 0's note should stay")
	}
	m.onTermOSC(7, "\x1b]133;C;cmdline_url=ls")
	if len(m.termHints) != 0 {
		t.Errorf("notes left: %v", m.termHints)
	}
}

func TestRestoreHintTextNamesTheProgram(t *testing.T) {
	cases := []struct {
		p    savedTermPane
		gone bool
		want string
	}{
		{savedTermPane{Cwd: "/p"}, false, ""},
		{savedTermPane{Cwd: "/p", Program: "npm"}, false, "was running: npm"},
		// Without terminal_restore_commands the command line is not shown,
		// even if an older session kept one.
		{savedTermPane{Cwd: "/p", Program: "npm", Cmd: "npm run dev"}, false, "was running: npm"},
		{savedTermPane{Cwd: "/p/web", Program: "npm"}, true, "was running: npm · folder web is gone · started in the project folder"},
	}
	for _, c := range cases {
		if got := restoreHintText(c.p, c.gone, false); got != c.want {
			t.Errorf("%+v gone=%v: %q, want %q", c.p, c.gone, got, c.want)
		}
	}
}

func TestRestoreHintTextOffersTheCommandWhenOptedIn(t *testing.T) {
	p := savedTermPane{Cwd: "/p", Program: "npm", Cmd: "npm run dev"}
	if got := restoreHintText(p, false, true); got != "was running: npm run dev · Enter to run it again" {
		t.Errorf("opted in: %q", got)
	}
	// In the wrong folder the command is not offered again.
	if got := restoreHintText(p, true, true); strings.Contains(got, "Enter") {
		t.Errorf("gone folder: %q", got)
	}
}

// Shells opened another way (a layout, a new tab) replace terminals that
// were waiting for Ctrl+T.
func TestLiveTerminalsReplacePendingOnes(t *testing.T) {
	m, _, _ := sessionTestModel(t)
	m.termRestoreReady = true
	m.termRestorePending = &savedTerminals{Tabs: []savedTermTab{{Name: "old", Panes: []savedTermPane{{Cwd: "/old"}}}}}
	m.termOpen = true
	m.terminalTabs = []terminalTab{{Name: "new", Cwd: "/new"}}
	m.observeTerminals()
	got := loadSession().Terminals
	if got == nil || got.Tabs[0].Name != "new" || !got.Open {
		t.Fatalf("saved %+v", got)
	}
	if m.termRestorePending != nil {
		t.Error("the old terminals must not come back on a later Ctrl+T")
	}
}
