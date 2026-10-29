package testkit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SCN-22: an AI tool call edits a file that is open in the editor with
// unsaved text and has a staged change in git; then "AI: Undo Last
// Turn"; then git status. The user's unsaved text is never lost
// silently, and the AI never writes over it without the review card.

// scn22Staged is r.txt as staged in git (HEAD has reviewOldA).
const scn22Staged = reviewOldA + "eleven\n"

// scn22Driver starts the AI scenario on a git repo where r.txt has a
// staged change (index and disk: scn22Staged).
func scn22Driver(t *testing.T, auto ...string) (*Driver, *aiScript) {
	t.Helper()
	s, srv := fakeToolModel(t, toolCall{"write_file", map[string]any{"path": "r.txt", "content": reviewNewA}})
	// No inline completions: they would take the fake model's replies.
	d := aiDriverConfig(t, srv.URL, reviewFiles(), true, `"ai_inline":false`, auto...)
	if err := writeAndStage(d.Dir, "r.txt", scn22Staged); err != nil {
		t.Fatal(err)
	}
	return d, s
}

func writeAndStage(dir, rel, body string) error {
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(body), 0o644); err != nil {
		return err
	}
	cmd := exec.Command("git", "add", "--", rel)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %v: %s", err, out)
	}
	return nil
}

// gitStatusOf is the porcelain status line of one path ("" when clean).
func gitStatusOf(d *Driver, rel string) string {
	d.T.Helper()
	cmd := exec.Command("git", "status", "--porcelain=v1", "--", rel)
	cmd.Dir = d.Dir
	out, err := cmd.Output()
	if err != nil {
		d.T.Fatalf("git status: %v", err)
	}
	return strings.TrimRight(string(out), "\n")
}

// gitIndexOf is the staged text of rel.
func gitIndexOf(d *Driver, rel string) string {
	d.T.Helper()
	out, err := exec.Command("git", "-C", d.Dir, "show", ":"+rel).Output()
	if err != nil {
		d.T.Fatalf("git show :%s: %v", rel, err)
	}
	return string(out)
}

// typeInEditor clicks into the r.txt tab (on line "ten") and types s.
func typeInEditor(d *Driver, s string) {
	d.T.Helper()
	clickText(d, " ten")
	d.Type(s)
	if !waitTabs(d, "r.txt*") {
		got, _ := tabs(d)
		d.T.Fatalf("r.txt not modified after typing: %v mode=%s buf=%q\n%s", got, lua(d, "return vim.fn.mode()"), bufText(d, "r.txt"), screen(d))
	}
}

// scn22Unchanged checks that disk, index and the unsaved tab are as the
// user left them.
func scn22Unchanged(t *testing.T, d *Driver, typed string) {
	t.Helper()
	if got := diskFile(d, "r.txt"); got != scn22Staged {
		t.Errorf("disk r.txt = %q, want the staged text", got)
	}
	if got := gitIndexOf(d, "r.txt"); got != scn22Staged {
		t.Errorf("index r.txt = %q", got)
	}
	if got := gitStatusOf(d, "r.txt"); got != "M  r.txt" {
		t.Errorf("git status %q, want the staged change only", got)
	}
	if got := bufText(d, "r.txt"); !strings.Contains(got, typed) {
		t.Errorf("unsaved text lost: tab %q", got)
	}
	if !waitTabs(d, "r.txt*") {
		got, _ := tabs(d)
		t.Errorf("tab no longer unsaved: %v", got)
	}
}

// Spec: Given r.txt staged and open with unsaved text, review mode,
// when the AI calls write_file on r.txt,
// then no review card opens, nothing is written, the model is told the
// file has unsaved changes; and AI: Undo Last Turn has nothing to undo
// and asks nothing. Disk, index and the unsaved tab stay as they were.
func TestSCN22AIWriteDirtyStagedReview(t *testing.T) {
	d, s := scn22Driver(t)
	open(d, "r.txt")
	typeInEditor(d, "Q")
	askAI(d, "change it")
	if !d.WaitForText("fake-done", 5*time.Second) {
		t.Fatalf("the turn did not finish:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "Review AI changes") {
		t.Errorf("review card for a file with unsaved edits:\n%s", screen(d))
	}
	if r := s.toolResults(); len(r) != 1 || !strings.Contains(r[0], "unsaved changes") {
		t.Errorf("model not told about the unsaved edits: %q\n%s", r, screen(d))
	}
	scn22Unchanged(t, d, "Q")
	d.Palette("ai-undo-turn")
	d.Settle(100 * time.Millisecond)
	if strings.Contains(screen(d), "Undo AI changes?") {
		t.Errorf("undo asks although the turn changed nothing:\n%s", screen(d))
	}
	scn22Unchanged(t, d, "Q")
}

// Spec: the same with file writes auto-approved (no review card): the
// write is refused, and Undo Last Turn neither asks nor says it would
// lose the unsaved text.
func TestSCN22AIWriteDirtyStagedAuto(t *testing.T) {
	d, s := scn22Driver(t, "fileWrite")
	open(d, "r.txt")
	typeInEditor(d, "Q")
	askAI(d, "change it")
	if !d.WaitForText("fake-done", 5*time.Second) {
		t.Fatalf("the turn did not finish:\n%s", screen(d))
	}
	if r := s.toolResults(); len(r) != 1 || !strings.Contains(r[0], "unsaved changes") {
		t.Errorf("model not told about the unsaved edits: %q", r)
	}
	scn22Unchanged(t, d, "Q")
	d.Palette("ai-undo-turn")
	d.Settle(100 * time.Millisecond)
	if strings.Contains(screen(d), "Undo AI changes?") {
		t.Errorf("undo asks although the turn changed nothing:\n%s", screen(d))
	}
	scn22Unchanged(t, d, "Q")
}

// acceptAIWrite runs the turn and accepts the whole file on the card.
func acceptAIWrite(t *testing.T, d *Driver) {
	t.Helper()
	open(d, "r.txt")
	askAI(d, "change it")
	if !d.WaitForText("Accept file", 5*time.Second) {
		t.Fatalf("no review card:\n%s", screen(d))
	}
	clickNth(d, "Accept file", 0)
	if !d.WaitForText("fake-done", 5*time.Second) {
		t.Fatalf("the turn did not finish:\n%s", screen(d))
	}
	if got := diskFile(d, "r.txt"); got != reviewNewA {
		t.Fatalf("accepted text not written: %q", got)
	}
	if got := gitStatusOf(d, "r.txt"); got != "MM r.txt" {
		t.Errorf("git status after the AI write %q", got)
	}
}

// Spec: Given the AI's accepted write to staged r.txt, when the user
// changes and saves r.txt and runs AI: Undo Last Turn,
// then Enter on the dialog is Cancel (the saved work stays); Restore puts
// back the staged text and git status shows the staged change only.
func TestSCN22UndoTurnAfterSaveAsksSafeFirst(t *testing.T) {
	d, _ := scn22Driver(t)
	acceptAIWrite(t, d)
	typeInEditor(d, "Q")
	d.Key("ctrl+s")
	if !eventually(d, func() bool { return strings.Contains(diskFile(d, "r.txt"), "Q") }) {
		t.Fatalf("save did not write: %q", diskFile(d, "r.txt"))
	}
	saved := diskFile(d, "r.txt")
	d.Palette("ai-undo-turn")
	if !d.WaitForText("Undo AI changes?", 2*time.Second) {
		t.Fatalf("no undo dialog:\n%s", screen(d))
	}
	d.Key("enter")
	d.Settle(100 * time.Millisecond)
	if got := diskFile(d, "r.txt"); got != saved {
		t.Fatalf("Enter restored over the user's saved work: %q", got)
	}
	d.Palette("ai-undo-turn")
	if !d.WaitForText("Undo AI changes?", 2*time.Second) {
		t.Fatalf("no undo dialog:\n%s", screen(d))
	}
	clickButton(d, "Restore")
	if !eventually(d, func() bool { return diskFile(d, "r.txt") == scn22Staged }) {
		t.Fatalf("Restore did not put the staged text back: %q", diskFile(d, "r.txt"))
	}
	if got := gitStatusOf(d, "r.txt"); got != "M  r.txt" {
		t.Errorf("git status after undo %q, want the staged change only", got)
	}
	if !eventually(d, func() bool { return bufLines(d)+"\n" == scn22Staged }) {
		t.Errorf("tab %q", bufLines(d))
	}
}

// Spec: Given the AI's accepted write to staged r.txt, when the user
// types in the tab (unsaved) and runs AI: Undo Last Turn,
// then the dialog says the unsaved text is kept (not lost), Enter is
// Cancel, and even Restore leaves the unsaved tab and the file alone.
func TestSCN22UndoTurnKeepsUnsavedText(t *testing.T) {
	d, _ := scn22Driver(t)
	acceptAIWrite(t, d)
	typeInEditor(d, "Q")
	d.Palette("ai-undo-turn")
	if !d.WaitForText("Undo AI changes?", 2*time.Second) {
		t.Fatalf("no undo dialog:\n%s", screen(d))
	}
	if strings.Contains(screen(d), "unsaved edits)") {
		t.Errorf("dialog lists the unsaved file as lost:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "unsaved edits, left as they are: r.txt") {
		t.Errorf("dialog does not say the unsaved file is kept:\n%s", screen(d))
	}
	d.Key("enter")
	d.Settle(100 * time.Millisecond)
	if strings.Contains(screen(d), "Undo AI changes of a turn") {
		t.Errorf("Enter ran the undo:\n%s", screen(d))
	}
	if got := bufText(d, "r.txt"); !strings.Contains(got, "Q") {
		t.Errorf("unsaved text lost: %q", got)
	}
	d.Palette("ai-undo-turn")
	if !d.WaitForText("Undo AI changes?", 2*time.Second) {
		t.Fatalf("no undo dialog:\n%s", screen(d))
	}
	clickButton(d, "Restore")
	d.Settle(100 * time.Millisecond)
	if got := bufText(d, "r.txt"); !strings.Contains(got, "Q") {
		t.Errorf("Restore dropped the unsaved text: %q", got)
	}
	if got := diskFile(d, "r.txt"); got != reviewNewA {
		t.Errorf("Restore wrote under the unsaved tab: %q", got)
	}
	if got := gitIndexOf(d, "r.txt"); got != scn22Staged {
		t.Errorf("index r.txt = %q", got)
	}
}
