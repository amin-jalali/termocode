package testkit

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Spec: Given a.txt and b.txt open, b.txt active and unchanged,
// when b.txt is deleted outside the app (rm, git checkout) and the user
// switches to the a.txt tab,
// then b.txt is not written back to disk.
func TestExternalDeleteNotRecreatedOnTabSwitch(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	open(d, "b.txt")
	if err := os.Remove(filepath.Join(d.Dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	d.Key("ctrl+pgup")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "a.txt" }, 3*time.Second) {
		got, a := tabs(d)
		t.Fatalf("Ctrl+PgUp: active %q (tabs %v), want a.txt", a, got)
	}
	if exists(d, "b.txt") {
		t.Errorf("leaving the b.txt tab wrote the deleted file back")
	}
}

// Spec: Given a.txt open,
// when the user types "Hi " then the tab shows a dirty dot and the file on
// disk is unchanged;
// when the user presses Ctrl+S, then the file holds the new text and the
// dot is gone.
func TestEditTypeAndSave(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Type("Hi ")
	if !waitTabs(d, "a.txt*") {
		got, _ := tabs(d)
		t.Fatalf("tab not marked modified: %v", got)
	}
	if _, _, ok := find(d, "a.txt ●"); !ok {
		t.Errorf("tab bar shows no dirty dot:\n%s", screen(d))
	}
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Fatalf("file changed before save: %q", got)
	}
	d.Key("ctrl+s")
	if got := diskFile(d, "a.txt"); got != "Hi alpha\nbravo\ncharlie\n" {
		t.Fatalf("after Ctrl+S file = %q", got)
	}
	if !waitTabs(d, "a.txt") {
		got, _ := tabs(d)
		t.Errorf("tab still modified after save: %v", got)
	}
	if _, _, ok := find(d, "a.txt ●"); ok {
		t.Errorf("dirty dot still shown after save:\n%s", screen(d))
	}
}

// Spec: Given a.txt and b.txt both edited and unsaved (a.txt changed in
// the background, so leaving its tab did not save it),
// when the user runs File: Save All,
// then both files hold their edits on disk and no tab is dirty.
func TestEditSaveAll(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	open(d, "b.txt")
	d.Type("2")
	lua(d, `vim.api.nvim_buf_set_lines(vim.fn.bufnr('a.txt'), 0, 1, false, {'1alpha'}) return ''`)
	if !waitTabs(d, "a.txt*", "b.txt*") {
		got, _ := tabs(d)
		t.Fatalf("tabs = %v, want both dirty", got)
	}
	d.Palette("save-all")
	if a, b := diskFile(d, "a.txt"), diskFile(d, "b.txt"); a != "1alpha\nbravo\ncharlie\n" || b != "2one\ntwo\nthree\n" {
		t.Fatalf("after Save All: a=%q b=%q", a, b)
	}
	if !waitTabs(d, "a.txt", "b.txt") {
		got, _ := tabs(d)
		t.Errorf("tabs still dirty after Save All: %v", got)
	}
}

// Spec: Given a.txt open and the user typed "abc",
// when the user presses Ctrl+Z, then the typed text is gone;
// when the user presses Ctrl+Y, then it is back.
func TestEditUndoRedo(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Type("abc")
	d.Key("ctrl+z")
	if got := bufLines(d); got != "alpha\nbravo\ncharlie" {
		t.Fatalf("after Ctrl+Z buffer = %q", got)
	}
	d.Key("ctrl+y")
	if got := bufLines(d); got != "abcalpha\nbravo\ncharlie" {
		t.Fatalf("after Ctrl+Y buffer = %q", got)
	}
}

// Spec: Given a.txt open, the user typed "abc" and pressed Esc (normal
// mode, as after closing a popup),
// when the user presses Ctrl+Z,
// then the typed text is undone (not a jump to an older cursor position).
func TestEditUndoAfterEsc(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Type("abc")
	d.Key("esc")
	d.Key("ctrl+z")
	if got := bufLines(d); got != "alpha\nbravo\ncharlie" {
		t.Fatalf("Ctrl+Z after Esc: buffer = %q", got)
	}
}

// Spec: Given a.txt open with the cursor on line 2 and no selection,
// when the user presses Ctrl+C, moves to line 1 and presses Ctrl+V,
// then line 2 is pasted as a whole new line above line 1.
func TestClipboardWholeLineCopyPaste(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Key("down")
	d.Key("ctrl+c")
	d.Key("up")
	d.Key("ctrl+v")
	if got := bufLines(d); got != "bravo\nalpha\nbravo\ncharlie" {
		t.Fatalf("buffer = %q, want bravo pasted as a line above alpha", got)
	}
}

// Spec: Given a.txt open with the cursor on line 1 and no selection,
// when the user presses Ctrl+X,
// then line 1 is removed; and Ctrl+V on the (new) line 2 puts it back
// above that line.
func TestClipboardWholeLineCut(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Key("ctrl+x")
	if got := bufLines(d); got != "bravo\ncharlie" {
		t.Fatalf("after Ctrl+X buffer = %q", got)
	}
	d.Key("down")
	d.Key("ctrl+v")
	if got := bufLines(d); got != "bravo\nalpha\ncharlie" {
		t.Fatalf("after Ctrl+V buffer = %q", got)
	}
}

// Spec: Given a.txt open,
// when the user presses Ctrl+G and enters 3,
// then the cursor is on line 3.
func TestGotoLine(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Key("ctrl+g")
	d.Type("3")
	d.Key("enter")
	if got := cursor(d); got != "3:1" {
		t.Fatalf("cursor at %s, want 3:1", got)
	}
}

// Spec: Given a workspace with notes/todo.md,
// when the user presses Ctrl+P, types "todo" and Enter,
// then notes/todo.md is the active tab.
func TestQuickOpenFile(t *testing.T) {
	d := editorDriver(t, nil)
	d.Key("ctrl+p")
	d.Type("todo")
	if !d.WaitForText("todo.md", 3*time.Second) {
		t.Fatalf("quick open does not list todo.md:\n%s", screen(d))
	}
	d.Key("enter")
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "notes/todo.md" }, 5*time.Second) {
		got, a := tabs(d)
		t.Fatalf("active %q (tabs %v), want notes/todo.md", a, got)
	}
}

// Spec: Given auto save turned on,
// when the user types in a.txt and waits,
// then the edit reaches the disk without Ctrl+S.
func TestAutoSaveWritesEdits(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Palette("toggle-auto-save")
	d.Type("auto ")
	if !d.WaitFor(func() bool { return diskFile(d, "a.txt") == "auto alpha\nbravo\ncharlie\n" }, 6*time.Second) {
		t.Fatalf("auto save did not write: disk %q", diskFile(d, "a.txt"))
	}
}

// Spec: Given a.txt open,
// when the user sets tab_size to 2 in Settings,
// then a Tab key indents by 2 columns in a newly opened file, and after a
// restart the setting still holds.
func TestSettingTabSize(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Action("setting:setting-tab-size=2")
	open(d, "b.txt")
	if got := lua(d, `return vim.bo.shiftwidth .. ',' .. vim.bo.tabstop`); got != "2,2" {
		t.Fatalf("b.txt shiftwidth,tabstop = %s, want 2,2", got)
	}
	d.Restart()
	open(d, "c.txt")
	if got := lua(d, `return vim.bo.shiftwidth .. ',' .. vim.bo.tabstop`); got != "2,2" {
		t.Errorf("after restart shiftwidth,tabstop = %s, want 2,2", got)
	}
}

// Spec: Given a.txt open,
// when the user turns word_wrap on in Settings,
// then the a.txt window wraps, a file opened next wraps too, and so does
// one opened after a restart; turning it off unwraps them again.
func TestSettingWordWrap(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	wrap := func() string { return lua(d, `return tostring(vim.wo.wrap)`) }
	d.Action("setting:setting-word-wrap=true")
	if got := wrap(); got != "true" {
		t.Fatalf("a.txt wrap = %s", got)
	}
	open(d, "b.txt")
	if got := wrap(); got != "true" {
		t.Errorf("b.txt opened after the setting: wrap = %s", got)
	}
	d.Restart()
	open(d, "c.txt")
	if got := wrap(); got != "true" {
		t.Errorf("after restart: wrap = %s", got)
	}
	d.Action("setting:setting-word-wrap=false")
	open(d, "a.txt")
	if got := wrap(); got != "false" {
		t.Errorf("after turning it off: wrap = %s", got)
	}
}

// Spec: Given a.txt open,
// when the user toggles word wrap with Alt+W twice,
// then wrap goes on, then off.
func TestToggleWordWrapKey(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	before := lua(d, `return tostring(vim.wo.wrap)`)
	d.Key("alt+w")
	mid := lua(d, `return tostring(vim.wo.wrap)`)
	d.Key("alt+w")
	after := lua(d, `return tostring(vim.wo.wrap)`)
	if before == mid || mid == after {
		t.Fatalf("Alt+W did not toggle wrap: %s → %s → %s", before, mid, after)
	}
}

// Spec: Given a.txt open and unchanged, and the user idle,
// when another program rewrites a.txt (git pull, another editor),
// then the buffer shows the new text without any key press — so the next
// key does not edit (and later save over) the old text.
func TestExternalChangeReloads(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	time.Sleep(1100 * time.Millisecond) // a new mtime second
	if err := os.WriteFile(filepath.Join(d.Dir, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !d.WaitFor(func() bool { return bufLines(d) == "changed" }, 5*time.Second) {
		t.Fatalf("buffer not reloaded: %q", bufLines(d))
	}
	if !d.WaitForText("changed", 2*time.Second) {
		t.Errorf("editor does not show the new text:\n%s", screen(d))
	}
}

// Spec: Given a.txt open,
// when another program deletes a.txt and the app keeps checking files,
// then the user is warned once (not every check), the editor keeps the
// text and the file is not written back.
// Slow (waits for three checks): runs with TERMOCODE_WALK=full.
func TestExternalDeleteWarnsOnce(t *testing.T) {
	if os.Getenv("TERMOCODE_WALK") != "full" {
		t.Skip("slow; set TERMOCODE_WALK=full")
	}
	d := editorDriver(t, nil)
	open(d, "a.txt")
	lua(d, `_G.tc_warns = 0
local orig = vim.notify
vim.notify = function(msg, ...)
  if tostring(msg):find('deleted on disk', 1, true) then _G.tc_warns = _G.tc_warns + 1 end
  return orig(msg, ...)
end
return ''`)
	if err := os.Remove(filepath.Join(d.Dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	d.WaitFor(func() bool { return false }, 6500*time.Millisecond)
	if got := lua(d, `return tostring(_G.tc_warns)`); got != "1" {
		t.Errorf("warned %s times, want once", got)
	}
	if got := bufLines(d); got != "alpha\nbravo\ncharlie" {
		t.Errorf("buffer = %q, want the old text kept", got)
	}
	if exists(d, "a.txt") {
		t.Errorf("a.txt written back")
	}
}

// Spec: Given a.txt open with unsaved edits,
// when the user runs File: Revert / Reload from Disk,
// then the edits are dropped, the buffer matches the file and the tab is
// not dirty.
func TestRevertDropsUnsavedEdits(t *testing.T) {
	d := editorDriver(t, nil)
	open(d, "a.txt")
	d.Type("junk")
	if !waitTabs(d, "a.txt*") {
		t.Fatal("tab not dirty after typing")
	}
	d.Palette("reload-buffer")
	if got := bufLines(d); got != "alpha\nbravo\ncharlie" {
		t.Fatalf("after revert buffer = %q", got)
	}
	if !waitTabs(d, "a.txt") {
		got, _ := tabs(d)
		t.Errorf("tab still dirty after revert: %v", got)
	}
}
