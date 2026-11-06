package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/picker"
)

// gitxBisectRepo: first, then c2..c8; from c5 on, bug.txt says "bug".
// Its tasks.json has a "check" task that fails on the bug.
func gitxBisectRepo(t *testing.T) (*Model, string, []string) {
	t.Helper()
	m, dir := covGitRepo(t)
	hs := []string{covGit(t, dir, "rev-parse", "HEAD")}
	for i := 2; i <= 8; i++ {
		v := "ok"
		if i >= 5 {
			v = "bug"
		}
		covWrite(t, dir, "bug.txt", v+"\n")
		covCommit(t, dir, fmt.Sprintf("c%d", i))
		hs = append(hs, covGit(t, dir, "rev-parse", "HEAD"))
	}
	covWrite(t, dir, ".termocode/tasks.json", `{"version":"2.0.0","tasks":[{"label":"check","type":"shell","command":"grep -q ok bug.txt"}]}`)
	covGit(t, dir, "add", "-A")
	covGit(t, dir, "commit", "-q", "-m", "tasks")
	hs = append(hs, covGit(t, dir, "rev-parse", "HEAD"))
	covRefresh(m)
	return m, dir, hs
}

func gitxBug(t *testing.T, dir string) bool {
	b, _ := os.ReadFile(filepath.Join(dir, "bug.txt"))
	return strings.TrimSpace(string(b)) == "bug"
}

// GITX-19 (M12): Bisect… marks HEAD bad, asks a good commit, then a
// picker answers Good / Bad / Skip until git names the first bad commit;
// End Bisect goes back.
func TestGitxBisectByHand(t *testing.T) {
	m, dir, hs := gitxBisectRepo(t)
	m.dispatchPaletteAction("git-bisect")
	if !m.pickerOpen || len(gitxPickerIDs(m)) < 8 {
		t.Fatalf("good picker %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "good-" + hs[0]})
	for i := 0; i < 10 && !m.confirmOpen; i++ {
		if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "bisect-good,bisect-bad,bisect-skip,bisect-run,bisect-reset" {
			t.Fatalf("step picker %v", gitxPickerIDs(m))
		}
		v := "bisect-good"
		if gitxBug(t, dir) {
			v = "bisect-bad"
		}
		m.pickerOpen = false
		m.handlePickerSelect(picker.SelectMsg{ID: v})
	}
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,open,reset" || !strings.Contains(m.confirm.View(), "c5") {
		t.Fatalf("result card %q\n%s", gitxCardIDList(m), m.confirm.View())
	}
	m.confirmOpen = false
	m.handleConfirmSelect("reset")
	if covGit(t, dir, "rev-parse", "HEAD") != hs[len(hs)-1] || covGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Error("End Bisect did not go back to main")
	}
	// Bisect… while one runs shows the step picker again.
	m.dispatchPaletteAction("git-bisect")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "good-" + hs[0]})
	m.pickerOpen = false
	m.dispatchPaletteAction("git-bisect")
	if !m.pickerOpen || gitxPickerIDs(m)[0] != "bisect-good" {
		t.Errorf("no step picker for the running bisect: %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "bisect-reset"})
	if covGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Error("reset left a detached HEAD")
	}
}

// GITX-19 (M13): Run a Task to Decide… picks a task and asks on a
// Cancel-first card; the run names the first bad commit.
func TestGitxBisectRunTask(t *testing.T) {
	m, dir, hs := gitxBisectRepo(t)
	m.dispatchPaletteAction("git-bisect")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "good-" + hs[0]})
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "bisect-run"})
	if !m.pickerOpen || !strings.Contains(strings.Join(gitxPickerIDs(m), ","), "task-check") {
		t.Fatalf("task picker %v", gitxPickerIDs(m))
	}
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "task-check"})
	if !m.confirmOpen || gitxCardIDList(m) != "cancel,run" || m.confirm.SafeID() != "cancel" {
		t.Fatalf("run card %q", gitxCardIDList(m))
	}
	m.confirmOpen = false
	done := covMsg[gitxDoneMsg](t, m.handleConfirmSelect("run"))
	done.apply(m)
	if !m.confirmOpen || !strings.Contains(m.confirm.View(), "c5") {
		t.Fatalf("no result:\n%s", m.confirm.View())
	}
	m.confirmOpen = false
	m.handleConfirmSelect("reset")
	if covGit(t, dir, "rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Error("not back on main")
	}
}

// GITX-24: Compare Branches… picks a ref, then what to compare: its
// changes since the split, ours since the split, the two tips, or the
// working tree.
func TestGitxCompareBranches(t *testing.T) {
	m, dir := covGitRepo(t)
	base := covGit(t, dir, "rev-parse", "HEAD")
	covGit(t, dir, "checkout", "-q", "-b", "feature")
	covWrite(t, dir, "f.txt", "f\n")
	covCommit(t, dir, "feature work")
	covGit(t, dir, "checkout", "-q", "main")
	covWrite(t, dir, "m.txt", "m\n")
	covCommit(t, dir, "main work")
	covRefresh(m)
	feature := covGit(t, dir, "rev-parse", "feature")
	main := covGit(t, dir, "rev-parse", "main")
	pick := func(mode string) commitViewReadyMsg {
		t.Helper()
		m.dispatchPaletteAction("git-compare-branches")
		if !m.pickerOpen || strings.Join(gitxPickerIDs(m), ",") != "ref-feature" {
			t.Fatalf("refs %v", gitxPickerIDs(m))
		}
		m.pickerOpen = false
		m.handlePickerSelect(picker.SelectMsg{ID: "ref-feature"})
		if strings.Join(gitxPickerIDs(m), ",") != "mode-theirs,mode-ours,mode-tips,mode-worktree" {
			t.Fatalf("modes %v", gitxPickerIDs(m))
		}
		m.pickerOpen = false
		return covMsg[commitViewReadyMsg](t, m.handlePickerSelect(picker.SelectMsg{ID: mode}))
	}
	files := func(msg commitViewReadyMsg) string {
		var p []string
		for _, f := range msg.info.Files {
			p = append(p, f.Path)
		}
		return strings.Join(p, ",")
	}
	if msg := pick("mode-theirs"); msg.compare == nil || msg.compare.a != base || msg.compare.b != feature || files(msg) != "f.txt" {
		t.Errorf("theirs %+v %s", msg.compare, files(msg))
	}
	if msg := pick("mode-ours"); msg.compare.a != base || msg.compare.b != main || files(msg) != "m.txt" {
		t.Errorf("ours %+v %s", msg.compare, files(msg))
	}
	if msg := pick("mode-tips"); msg.compare.a != main || msg.compare.b != feature || files(msg) != "f.txt,m.txt" {
		t.Errorf("tips %+v %s", msg.compare, files(msg))
	}
	// working tree vs feature: a read-only diff card with every file
	covWrite(t, dir, "a.txt", "edited\n")
	m.dispatchPaletteAction("git-compare-branches")
	m.pickerOpen = false
	m.handlePickerSelect(picker.SelectMsg{ID: "ref-feature"})
	m.pickerOpen = false
	covMsg[gitxDoneMsg](t, m.handlePickerSelect(picker.SelectMsg{ID: "mode-worktree"})).apply(m)
	if !m.reviewOpen || m.aiS().review == nil || m.aiS().review.card.Files() != 3 {
		t.Errorf("working tree card: open %v", m.reviewOpen)
	}
}
