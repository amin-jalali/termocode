package testkit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SCN-7: the folders termocode keeps its own state in cannot be written.
// The user is told once in plain words; their text in the editor is
// never touched and quitting still asks about it.

// Spec: Given the config folder is read-only,
// when the user changes a setting, then an error toast says in plain
// words that the folder cannot be written, config.json is unchanged, and an edited
// file is still modified and asked about at quit.
func TestSettingWithReadOnlyConfigDir(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	cfgDir := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode")
	before, _ := os.ReadFile(filepath.Join(cfgDir, "config.json"))
	entsBefore, _ := os.ReadDir(cfgDir)
	chmodBack(t, cfgDir, 0o555, 0o755)
	open(d, "a.txt")
	d.Type("Hi ")
	d.Action("setting:setting-tab-size=2")
	if !d.WaitForText("Settings error", 3*time.Second) {
		t.Fatalf("no clear error toast:\n%s", screen(d))
	}
	if s := screen(d); !strings.Contains(s, "no permission to write in ~") {
		t.Errorf("the toast does not say why:\n%s", s)
	}
	after, _ := os.ReadFile(filepath.Join(cfgDir, "config.json"))
	if string(after) != string(before) {
		t.Errorf("config.json changed: %q → %q", before, after)
	}
	if ents, _ := os.ReadDir(cfgDir); len(ents) != len(entsBefore) {
		t.Errorf("config folder holds %d entries, had %d: a temp file is left", len(ents), len(entsBefore))
	}
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Errorf("tabs = %v, want [a.txt*]", got)
	}
	d.Key("ctrl+q")
	if d.Quit() || !d.WaitForText("unsaved changes", 3*time.Second) {
		t.Errorf("quit did not ask about a.txt:\n%s", screen(d))
	}
}

// lockStateDirs makes the state folder and the termocode folders in it
// read-only (each one that exists), so no backup or session file can be
// written there.
func lockStateDirs(t *testing.T) {
	t.Helper()
	p := os.Getenv("XDG_STATE_HOME")
	for _, sub := range []string{"", "termocode", "backup"} {
		p = filepath.Join(p, sub)
		if _, err := os.Stat(p); err != nil {
			return
		}
		chmodBack(t, p, 0o555, 0o755)
	}
}

// Spec: Given the state folder (where unsaved changes are backed up) is
// read-only,
// when the user edits a.txt, then one warning says the unsaved changes
// cannot be backed up, the text stays modified in the editor, saving
// still works, and the warning is not repeated on every key.
func TestHotExitBackupWithReadOnlyStateDir(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	lockStateDirs(t)
	open(d, "a.txt")
	d.Type("Hi ")
	if !d.WaitForText("Cannot back up", 3*time.Second) {
		t.Fatalf("no warning that the backup failed:\n%s", screen(d))
	}
	d.Action("toasts:clear")
	d.Type("there ")
	d.Settle(700 * time.Millisecond) // past the 400 ms backup timer
	if strings.Contains(screen(d), "Cannot back up") {
		t.Errorf("the warning came again on the next edit:\n%s", screen(d))
	}
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Errorf("tabs = %v, want [a.txt*]", got)
	}
	d.Key("esc", "ctrl+s")
	if !eventually(d, func() bool { return diskFile(d, "a.txt") == "Hi there alpha\nbravo\ncharlie\n" }) {
		t.Errorf("save did not work: %q", diskFile(d, "a.txt"))
	}
}

// Spec: Given the state folder (session files, backups) is read-only,
// when the user edits a.txt and quits, then the quit card still asks
// about a.txt; Don't Save quits; and the next start works (no crash, a
// usable editor) and leaves a.txt as it was on disk.
func TestQuitAndRestartWithReadOnlyStateDir(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	lockStateDirs(t)
	open(d, "a.txt")
	d.Type("Hi ")
	d.Key("ctrl+q")
	if d.Quit() || !d.WaitForText("unsaved changes", 3*time.Second) {
		t.Fatalf("quit did not ask about a.txt:\n%s", screen(d))
	}
	clickButton(d, "Don't Save")
	if !d.Quit() {
		t.Fatalf("Don't Save did not quit:\n%s", screen(d))
	}
	d.Restart()
	open(d, "b.txt")
	if got := bufLines(d); got != "one\ntwo\nthree" {
		t.Errorf("b.txt after restart holds %q", got)
	}
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("a.txt on disk changed to %q", got)
	}
}

// Spec: Given HOME itself is read-only (its config and state folders
// still exist),
// when the user edits and saves a file and changes a setting, then both
// work and nothing crashes.
func TestReadOnlyHome(t *testing.T) {
	skipIfRoot(t)
	d := editorDriver(t, nil)
	chmodBack(t, os.Getenv("HOME"), 0o555, 0o755)
	open(d, "a.txt")
	d.Type("Hi ")
	d.Key("esc", "ctrl+s")
	if !eventually(d, func() bool { return diskFile(d, "a.txt") == "Hi alpha\nbravo\ncharlie\n" }) {
		t.Errorf("save did not work: %q", diskFile(d, "a.txt"))
	}
	d.Action("setting:setting-tab-size=2")
	if !d.WaitForText("tab_size = 2", 3*time.Second) {
		t.Errorf("setting not saved:\n%s", screen(d))
	}
}
