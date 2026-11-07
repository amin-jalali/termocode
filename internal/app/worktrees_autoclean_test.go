package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// CMP-16: the auto clean-up settings are Git rows, off by default, kept
// from the AI, and checked on set.
func TestWorktreeAutoCleanSettings(t *testing.T) {
	withConfigDir(t)
	cats := settingsCategories()
	for _, key := range []string{"worktree_auto_archive_days", "worktree_max"} {
		found := false
		for _, e := range SettingsCatalog() {
			if e.Key != key {
				continue
			}
			found = true
			if e.Default != "0" {
				t.Errorf("%s: default %q, want 0 (off)", key, e.Default)
			}
			if cats[e.ID] != "Git" {
				t.Errorf("%s: category %q, want Git", key, cats[e.ID])
			}
			r, _ := findSettingsRow(e.ID)
			if v := currentSettingValue(r, loadSettings()); v != "0" {
				t.Errorf("%s: value with no config %q", key, v)
			}
		}
		if !found {
			t.Fatalf("no %s row", key)
		}
		if g := aiSettingPolicies[key].Gate; g != aiGateNever {
			t.Errorf("%s: AI gate %q, want never", key, g)
		}
	}
	m := &Model{wt: newWTState()}
	if _, ok := m.applyAISetting(wtAutoDaysRowID, "30"); !ok {
		t.Fatal("the days row is not applied")
	}
	if _, ok := m.applyAISetting(wtAutoMaxRowID, "-1"); !ok {
		t.Fatal("the cap row is not applied")
	}
	if days, maxN := wtAutoSettings(); days != 30 || maxN != 0 {
		t.Fatalf("settings %d %d, want 30 0 (a bad cap is refused)", days, maxN)
	}
}

// CMP-16: with worktree_auto_archive_days on, reading the list archives
// the clean, idle worktrees termocode made: the folder goes (disk is
// freed), the branch stays; a dirty one and one made by hand stay. A
// toast names them; it runs at most once an hour; x restores one in one
// step.
func TestWorktreeAutoCleanArchivesIdle(t *testing.T) {
	withConfigDir(t)
	m, root, x, ext := wtRealModel(t)
	d := filepath.Join(filepath.Dir(x), "d")
	wtGitClean(t, root, "worktree", "add", "-q", "-b", "d", d)
	if err := os.WriteFile(filepath.Join(d, "draft.txt"), []byte("not saved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(x, "big.bin"), make([]byte, 4096), 0o644); err != nil {
		t.Fatal(err)
	}
	wtGitClean(t, x, "add", "big.bin")
	wtGitClean(t, x, "commit", "-q", "-m", "big")
	wtRun(t, m, m.worktreesReload())
	if c := m.wtAutoClean(); c != nil {
		t.Fatal("the auto clean-up ran while it is off")
	}

	if err := saveHTTPSetting("worktree_auto_archive_days", 7); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(30 * 24 * time.Hour)
	defer clock.Set(func() time.Time { return later })()
	cmd := m.wtAutoClean()
	if cmd == nil {
		t.Fatal("no auto clean-up")
	}
	msg, ok := cmd().(wtAutoCleanedMsg)
	if !ok {
		t.Fatal("not an auto clean-up result")
	}
	if msg.err != nil {
		t.Fatal(msg.err)
	}
	if len(msg.done) != 1 || msg.done[0].path != x || !strings.Contains(msg.done[0].why, "no activity for") {
		t.Fatalf("archived %+v, want only x", msg.done)
	}
	if msg.freed < 4096 {
		t.Fatalf("freed %d bytes, want at least 4096", msg.freed)
	}
	if _, err := os.Stat(x); !os.IsNotExist(err) {
		t.Fatalf("x is still there: %v", err)
	}
	for _, p := range []string{d, ext, root} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s is gone: %v", p, err)
		}
	}
	if _, ok := m.handleWTMsg(msg); !ok {
		t.Fatal("the result is not a WORKTREES message")
	}
	if m.wt.autoBusy {
		t.Fatal("still busy")
	}
	if c := m.wtAutoClean(); c != nil {
		t.Fatal("ran twice in one hour")
	}

	wtRun(t, m, m.worktreesReload())
	it, ok := wtItemAt(m, x)
	if !ok || it.e.Meta.State != worktrees.StateArchived || it.e.Meta.Branch != "feat/x" {
		t.Fatalf("x is not archived with its branch: %+v", it.e.Meta)
	}
	m.wt.cursorPath = x
	wtStep(t, m, wtKey(t, m, "x"))
	if _, err := os.Stat(filepath.Join(x, "big.bin")); err != nil {
		t.Fatalf("x was not restored in one step: %v", err)
	}
}

// CMP-16: the toast names what was archived, how much disk it freed
// and how to get it back.
func TestWorktreeAutoCleanToast(t *testing.T) {
	m := wtTestModel(t, t.TempDir())
	m.wt.off = true
	m.wt.autoBusy = true
	m.onWTAutoCleaned(wtAutoCleanedMsg{done: []wtAutoDone{{path: "/r/a", title: "a", why: "no activity for 9 days"}}, freed: 3 << 20})
	if m.wt.autoBusy {
		t.Fatal("still busy")
	}
	var got string
	for _, ts := range m.toast.Live() {
		got += ts.Title + "\n" + ts.Detail + "\n"
	}
	for _, want := range []string{"Auto-archived 1 worktree", "a — no activity for 9 days", "3.0 MB", "x on its row"} {
		if !strings.Contains(got, want) {
			t.Errorf("toast lacks %q:\n%s", want, got)
		}
	}
}
