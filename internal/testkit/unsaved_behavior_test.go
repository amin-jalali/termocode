package testkit

import (
	"testing"
	"time"
)

// Spec: Given auto save off and a.txt edited (not saved),
// when the user switches to b.txt,
// then a.txt on disk is unchanged and its tab still shows it as modified.
func TestSwitchTabDoesNotSaveWhenAutoSaveOff(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "b.txt")
	open(d, "a.txt")
	d.Type("Hi ")
	if !waitTabs(d, "b.txt", "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("a.txt not modified: %v", got)
	}
	d.Key("ctrl+pgup")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "b.txt" }, 3*time.Second) {
		_, a := tabs(d)
		t.Fatalf("active tab %q, want b.txt", a)
	}
	d.Settle(200 * time.Millisecond)
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("switching tabs saved a.txt with auto save off: %q", got)
	}
	if !waitTabs(d, "b.txt", "a.txt*") {
		got, _ := tabs(d)
		t.Errorf("a.txt lost its modified mark: %v", got)
	}
}

// Spec: Given a.txt edited (not saved), when the user quits, then the app
// asks first; Cancel keeps it running; Don't Save quits and leaves the file
// unchanged.
func TestQuitAsksAboutUnsavedFiles(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Type("Hi ")
	waitTabs(d, "a.txt*")
	d.Key("ctrl+q")
	if d.Quit() {
		t.Fatalf("quit without asking about unsaved a.txt")
	}
	if _, _, ok := find(d, "unsaved"); !ok {
		t.Fatalf("no unsaved-changes question:\n%s", screen(d))
	}
	clickButton(d, "Cancel")
	if d.Quit() {
		t.Fatalf("Cancel quit the app")
	}
	d.Key("ctrl+q")
	clickButton(d, "Don't Save")
	if !d.Quit() {
		t.Fatalf("Don't Save did not quit:\n%s", screen(d))
	}
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("Don't Save wrote the file: %q", got)
	}
}

// Spec: Given a.txt edited (not saved), when the user quits and picks Save
// All, then the file is saved and the app quits.
func TestQuitSaveAll(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Type("Hi ")
	waitTabs(d, "a.txt*")
	d.Key("ctrl+q")
	clickButton(d, "Save All")
	if !d.Quit() {
		t.Fatalf("Save All did not quit:\n%s", screen(d))
	}
	if got := diskFile(d, "a.txt"); got != "Hi alpha\nbravo\ncharlie\n" {
		t.Errorf("Save All did not save: %q", got)
	}
}

// Spec: Given no unsaved files, when the user quits, then the app quits at
// once (no question).
func TestQuitCleanQuitsAtOnce(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Key("ctrl+q")
	if !d.Quit() {
		t.Fatalf("clean quit asked a question:\n%s", screen(d))
	}
}
