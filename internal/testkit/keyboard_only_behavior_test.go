package testkit

import (
	"strings"
	"testing"
	"time"
)

// Keyboard-only audit (SCN-33): the main journeys work with the mouse
// disabled. The driver sends keys only (no Action, Palette or mouse
// call after the start), hover is off in the config, and at the end the
// step history must hold no mouse message at all.

// keyboardDriver starts the app with nvim on the text workspace, mouse
// hover off and a plain /bin/sh for the terminal.
func keyboardDriver(t *testing.T) *Driver {
	t.Helper()
	if testing.Short() {
		t.Skip("starts nvim")
	}
	t.Setenv("SHELL", "/bin/sh")
	t.Setenv("PS1", "$ ")
	return New(t, Options{W: 120, H: 34, Nvim: true, Files: textFiles(),
		Config: map[string]string{"config.json": `{"mouse_hover":"off"}`}})
}

// noMouse fails when any step of the run was a mouse message.
func noMouse(t *testing.T, d *Driver) {
	t.Helper()
	for _, step := range d.History() {
		if strings.HasPrefix(step, "mouse ") {
			t.Fatalf("keyboard-only run sent a mouse step: %q", step)
		}
	}
}

// quickOpen opens rel with Ctrl+P, typing its name and pressing Enter.
func quickOpen(t *testing.T, d *Driver, rel string) {
	t.Helper()
	d.Key("ctrl+p")
	d.Type(rel)
	if !d.WaitForText(rel, 3*time.Second) {
		t.Fatalf("Ctrl+P does not list %s:\n%s", rel, screen(d))
	}
	d.Key("enter")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == rel }, 5*time.Second) {
		got, a := tabs(d)
		t.Fatalf("Ctrl+P %s: active %q (tabs %v)\n%s", rel, a, got, screen(d))
	}
}

// Spec: Given the app with the mouse disabled,
// when the user only uses the keyboard,
// then every main journey works: open files (Ctrl+P), edit and save
// (Ctrl+S), switch tabs (Ctrl+PgUp/PgDn), move focus (F6), hide and show
// the sidebar (Ctrl+B), run a command from the palette (F1) and close it
// (Esc), search all files (F8) and close the results (q), open and hide
// the terminal (Ctrl+T), and close a tab (Ctrl+W). No step needs a mouse.
func TestKeyboardOnlyMainJourneys(t *testing.T) {
	d := keyboardDriver(t)

	// Open two files.
	quickOpen(t, d, "a.txt")
	quickOpen(t, d, "b.txt")

	// Edit and save.
	d.Type("X")
	if !waitTabs(d, "a.txt", "b.txt*") {
		got, _ := tabs(d)
		t.Fatalf("typing did not mark b.txt modified: %v", got)
	}
	d.Key("ctrl+s")
	if !eventually(d, func() bool { return diskFile(d, "b.txt") == "Xone\ntwo\nthree\n" }) {
		t.Fatalf("Ctrl+S: b.txt = %q", diskFile(d, "b.txt"))
	}

	// Switch tabs.
	d.Key("ctrl+pgup")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "a.txt" }, 3*time.Second) {
		_, a := tabs(d)
		t.Fatalf("Ctrl+PgUp: active %q, want a.txt", a)
	}
	d.Key("ctrl+pgdown")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "b.txt" }, 3*time.Second) {
		_, a := tabs(d)
		t.Fatalf("Ctrl+PgDn: active %q, want b.txt", a)
	}

	// Move focus to the sidebar and back.
	d.Key("f6")
	if d.Model().FocusedEditor() {
		t.Fatalf("F6 left the focus on the editor:\n%s", screen(d))
	}
	d.Key("f6")
	if !d.Model().FocusedEditor() {
		t.Fatalf("second F6 did not give the focus back to the editor:\n%s", screen(d))
	}

	// Hide and show the sidebar.
	d.Key("ctrl+b")
	if strings.Contains(screen(d), "EXPLORER") {
		t.Fatalf("Ctrl+B did not hide the sidebar:\n%s", screen(d))
	}
	d.Key("ctrl+b")
	if !d.WaitForText("EXPLORER", 2*time.Second) {
		t.Fatalf("second Ctrl+B did not show the sidebar:\n%s", screen(d))
	}

	// Run a command from the palette, then close what it opened.
	d.Key("f1")
	d.Type("Help: Show Shortcuts")
	d.Key("enter")
	if !d.WaitForText("Keyboard Shortcuts", 3*time.Second) {
		t.Fatalf("F1 + Enter did not run Help: Show Shortcuts:\n%s", screen(d))
	}
	d.Key("esc")
	if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "Keyboard Shortcuts") }, 2*time.Second) {
		t.Fatalf("Esc did not close the shortcuts sheet:\n%s", screen(d))
	}

	// Search all files and close the results.
	d.Key("f8")
	if !d.WaitForText("Find in Files", 3*time.Second) {
		t.Fatalf("F8 did not open Find in Files:\n%s", screen(d))
	}
	d.Type("charlie")
	d.Key("enter")
	if !d.WaitForText("Find Results", 5*time.Second) || !strings.Contains(screen(d), "charlie") {
		t.Fatalf("F8 search shows no results:\n%s", screen(d))
	}
	d.Key("q")
	if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "Find Results") }, 3*time.Second) {
		t.Fatalf("q did not close Find Results:\n%s", screen(d))
	}

	// Open and hide the terminal.
	d.Key("ctrl+t")
	if !d.WaitForText("Terminal › sh", 5*time.Second) {
		t.Fatalf("Ctrl+T did not open the terminal:\n%s", screen(d))
	}
	d.Key("ctrl+t")
	if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "Terminal › sh") }, 3*time.Second) {
		t.Fatalf("second Ctrl+T did not hide the terminal:\n%s", screen(d))
	}

	// Close a tab.
	if !d.WaitFor(func() bool { _, a := tabs(d); return a != "" }, 3*time.Second) {
		t.Fatalf("no active tab after the terminal closed:\n%s", screen(d))
	}
	_, before := tabs(d)
	d.Key("ctrl+w")
	if !d.WaitFor(func() bool {
		got, _ := tabs(d)
		for _, g := range got {
			if strings.TrimSuffix(g, "*") == before {
				return false
			}
		}
		return true
	}, 3*time.Second) {
		got, _ := tabs(d)
		t.Fatalf("Ctrl+W did not close %s: tabs %v", before, got)
	}

	noMouse(t, d)
}
