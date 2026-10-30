package app

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// Spec (WEB-12): the status replay golden: a branch switch, a busy
// terminal and an AI turn replay to the final snapshot.
func TestStatusReplayGolden(t *testing.T) {
	m := aiTestModel(t)
	setProcWorkspace(t, workspace.Workspace{Root: "/work/demo"})
	m.themeID = "dark"
	webTestPinPalette(t)
	now := webTestClock(t)
	h := webTestHub(t, &m)
	c := webReplayRecord(t, &m, h, "status", func() {
		m.gitBranch = git.Branch{Name: "main", Ahead: 1}
		m.webAfterUpdate(nil)
		*now = now.Add(time.Second)
		m.terminalTabs = []terminalTab{{BufID: 3, Busy: true}}
		m.gitFiles = []git.FileStatus{{Path: "a.go"}}
		m.webAfterUpdate(nil)
	})
	if len(c.Deltas) != 1 { // queued, the second replaced the first
		t.Fatalf("deltas %d, want 1 (whole-object deltas merge)", len(c.Deltas))
	}
	webReplayCheck(t, h.topics["status"], c)
	webReplayGolden(t, webReplayPath("status", "branch_terminal"), c)
}

// webTestPinPalette sets the palette fields status.theme reads to fixed
// colors (the default theme's) for one test, whatever ran before it.
func webTestPinPalette(t *testing.T) {
	t.Helper()
	pinned := []theme.Color256{234, 236, 233, 236, 235, 24, 32, 231, 252, 250, 102, 242, 238, 32, 141, 39, 78, 178, 203, 180, 78, 167, 62,
		74, 175, 187, 153, 79, 174, 151, 65, 167, 75, 252}
	old := make([]theme.Color256, len(webThemeFields))
	for i, f := range webThemeFields {
		old[i], *f.c = *f.c, pinned[i]
	}
	t.Cleanup(func() {
		for i, f := range webThemeFields {
			*f.c = old[i]
		}
	})
}

// Spec (WEB-12): the toasts replay golden: push, keyed update, dismiss.
func TestToastsReplayGolden(t *testing.T) {
	m := aiTestModel(t)
	h := webTestHub(t, &m)
	m.toast, _ = m.toast.Push(toast.Info, "Saved")
	c := webReplayRecord(t, &m, h, "toasts", func() {
		m.toast, _ = m.toast.PushKeyed("clone", toast.Warn, "Cloning", "10%")
		m.webAfterUpdate(nil)
		m.toast, _ = m.toast.PushKeyed("clone", toast.Warn, "Cloning", "90%")
		m.toast = m.toast.DismissID(1)
		m.webAfterUpdate(nil)
		m.toast, _ = m.toast.PushDetail(toast.Errr, "Build failed", "exit 2")
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["toasts"], c)
	webReplayGolden(t, webReplayPath("toasts", "push_update_dismiss"), c)
}
