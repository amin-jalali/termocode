package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
)

// covModify opens path in nvim and changes its text without saving.
func covModify(t *testing.T, m Model, path, text string) {
	t.Helper()
	if err := m.nvim.OpenFile("edit", path); err != nil {
		t.Fatal(err)
	}
	if err := m.nvim.ExecLuaArgs(`vim.api.nvim_buf_set_lines(0, 0, -1, false, {...})`, text); err != nil {
		t.Fatal(err)
	}
}

func covIsQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// Quit with unsaved files asks first; a file that cannot be saved is
// asked about again; Don't Save quits.
func TestCovCoreQuitWithUnsavedFiles(t *testing.T) {
	m := covNvimModel(t, 120, 40)
	dir := t.TempDir()
	gone := filepath.Join(dir, "gone")
	if err := os.Mkdir(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		p := filepath.Join(dir, n)
		_ = os.WriteFile(p, []byte("old\n"), 0o644)
		paths = append(paths, p)
	}
	lost := filepath.Join(gone, "d.txt")
	_ = os.WriteFile(lost, []byte("old\n"), 0o644)
	paths = append(paths, lost)
	for _, p := range paths {
		covModify(t, m, p, "new")
	}

	cmd := m.quit()
	if cmd != nil || !m.confirmOpen || m.confirmKind != confirmKindQuit {
		t.Fatalf("quit with unsaved files: cmd %v confirm %v", cmd != nil, m.confirmOpen)
	}
	if v := ansiPlain(m.confirm.View()); !strings.Contains(v, "4 files have unsaved changes: a.txt, b.txt, c.txt, …") {
		t.Errorf("question:\n%s", v)
	}
	// The folder of d.txt goes away: Save All cannot write it.
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	m, cmd = m.update(confirm.SelectMsg{ID: "save"})
	if covIsQuit(cmd) {
		t.Fatal("quit although d.txt was not saved")
	}
	if !m.confirmOpen || !strings.Contains(ansiPlain(m.confirm.View()), "Could not save d.txt") {
		t.Fatalf("not asked again about d.txt:\n%s", ansiPlain(m.confirm.View()))
	}
	for _, p := range paths[:3] {
		if b, _ := os.ReadFile(p); strings.TrimSpace(string(b)) != "new" {
			t.Errorf("%s not saved: %q", p, b)
		}
	}
	// Cancel keeps the editor.
	m, cmd = m.update(confirm.SelectMsg{ID: "cancel"})
	if covIsQuit(cmd) {
		t.Error("cancel quit")
	}
	m.promptQuit([]string{lost})
	m, cmd = m.update(confirm.SelectMsg{ID: "discard"})
	if !covIsQuit(cmd) {
		t.Error("Don't Save did not quit")
	}
}

// Quit with nothing unsaved quits at once and drops the live backup.
func TestCovCoreQuitCleanDropsBackup(t *testing.T) {
	m := covNvimModel(t, 100, 30)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_ = m.startHotExit()
	if m.hotExitLive == "" {
		t.Fatal("hot exit did not start")
	}
	_ = os.MkdirAll(m.hotExitLive, 0o700)
	if !covIsQuit(m.quit()) {
		t.Fatal("clean quit did not quit")
	}
	if _, err := os.Stat(m.hotExitLive); !os.IsNotExist(err) {
		t.Errorf("live backup left: %v", err)
	}
}

// ansiPlain is a dialog's text on one line, without colours and borders.
func ansiPlain(s string) string {
	s = stripANSI(s)
	for _, b := range []string{"│", "╭", "╮", "╰", "╯", "─"} {
		s = strings.ReplaceAll(s, b, " ")
	}
	return strings.Join(strings.Fields(s), " ")
}

// covWriteBackup leaves a backup of a finished run (pid) for path.
func covWriteBackup(t *testing.T, root string, pid int, name, path string, lines ...string) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := path + "\n" + strings.Join(lines, "\n")
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covDeadPID is a pid no process has.
const covDeadPID = 2147483000

// After a crash: the next start asks; Restore puts the text back into
// modified buffers; Discard and Later do what they say.
func TestCovCoreHotExitRestore(t *testing.T) {
	for _, answer := range []string{"restore", "discard", "later"} {
		t.Run(answer, func(t *testing.T) {
			m := covNvimModel(t, 120, 40)
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			root, err := hotExitRootFor(m.workspace().ID)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			var files []string
			for i, n := range []string{"w.go", "x.go", "y.go", "z.go"} {
				p := filepath.Join(dir, n)
				_ = os.WriteFile(p, []byte("saved\n"), 0o644)
				files = append(files, p)
				covWriteBackup(t, root, covDeadPID, strconv.Itoa(i)+".bak", p, "unsaved "+n)
			}
			// Not backups: ignored.
			_ = os.WriteFile(filepath.Join(root, strconv.Itoa(covDeadPID), "notes.txt"), []byte("x"), 0o600)
			_ = os.WriteFile(filepath.Join(root, "not-a-pid"), nil, 0o600)
			covWriteBackup(t, root, covDeadPID, "empty.bak", "")

			cmd := m.startHotExit()
			var found hotExitFoundMsg
			for _, msg := range gateCmdMsgs(cmd) {
				if f, ok := msg.(hotExitFoundMsg); ok {
					found = f
				}
			}
			if len(found.backups) != 4 {
				t.Fatalf("found %d backups", len(found.backups))
			}
			m, _ = m.update(found)
			if !m.confirmOpen || m.confirmKind != confirmKindHotExit {
				t.Fatalf("no restore question (confirm %v)", m.confirmOpen)
			}
			if v := ansiPlain(m.confirm.View()); !strings.Contains(v, "4 files had unsaved changes") || !strings.Contains(v, "…") {
				t.Errorf("question:\n%s", v)
			}
			m, cmd = m.update(confirm.SelectMsg{ID: answer})
			prev := filepath.Join(root, "previous")
			_, prevErr := os.Stat(prev)
			switch answer {
			case "restore":
				if !strings.Contains(toastText(m), "Restored 4 unsaved files") {
					t.Errorf("toast:\n%s", toastText(m))
				}
				covWaitLua(t, m, `return table.concat(vim.fn.getbufline(vim.fn.bufnr('`+files[2]+`'), 1, '$'), '\n')`, "unsaved y.go")
				covWaitLua(t, m, `return vim.bo[vim.fn.bufnr('`+files[2]+`')].modified and '1' or '0'`, "1")
				if prevErr == nil {
					t.Error("previous/ kept after the restore")
				}
			case "discard":
				if prevErr == nil {
					t.Error("previous/ kept after Discard")
				}
			case "later":
				if prevErr != nil {
					t.Error("Later dropped the backups")
				}
			}
		})
	}
}

// One backup: the question names the file; a restore without nvim or
// without backups does nothing.
func TestCovCoreHotExitOneFile(t *testing.T) {
	m := covNvimModel(t, 100, 30)
	m.promptHotExit([]hotBackup{{Path: "/w/only.go", Lines: []string{"x"}}})
	if v := ansiPlain(m.confirm.View()); !strings.Contains(v, `"only.go" had unsaved changes`) {
		t.Errorf("question:\n%s", v)
	}
	m.hotExitPending = nil
	if m.onHotExitConfirm("restore") != nil {
		t.Error("restore with nothing pending did something")
	}
	var none Model
	if none.startHotExit() != nil {
		t.Error("hot exit without nvim")
	}
	none.flushHotExitForReload()
	HotExitFlush(2 * time.Second) // the nvim of this model
	hotExitNvim.Store(nil)
	HotExitFlush(time.Second) // none: nothing to do
}

// Reload Window: the unsaved text is flushed and marked, and the next
// image puts it back without asking.
func TestCovCoreHotExitReload(t *testing.T) {
	m := covNvimModel(t, 100, 30)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_ = m.startHotExit()
	p := filepath.Join(t.TempDir(), "r.go")
	_ = os.WriteFile(p, []byte("saved\n"), 0o644)
	covModify(t, m, p, "typed just now")
	m.flushHotExitForReload()
	if _, err := os.Stat(filepath.Join(m.hotExitLive, hotExitReloadMark)); err != nil {
		t.Fatalf("no reload mark: %v", err)
	}
	_ = m.nvim.Close() // the re-exec closes nvim right after the flush

	// The new image (same pid) on a fresh nvim.
	n := covNvimModel(t, 100, 30)
	cmd := n.startHotExit()
	for _, msg := range gateCmdMsgs(cmd) {
		if _, ok := msg.(hotExitFoundMsg); ok {
			t.Error("a reload asked about its own backups")
		}
	}
	covWaitLua(t, n, `return table.concat(vim.fn.getbufline(vim.fn.bufnr('`+p+`'), 1, '$'), '\n')`, "typed just now")
}

// Open Workspace elsewhere: the unsaved text moves to previous/, where
// the next start here finds it.
func TestCovCoreHotExitLeaveWorkspace(t *testing.T) {
	m := covNvimModel(t, 100, 30)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_ = m.startHotExit()
	p := filepath.Join(t.TempDir(), "l.go")
	_ = os.WriteFile(p, []byte("saved\n"), 0o644)
	covModify(t, m, p, "left behind")
	m.leaveWorkspace()
	b, err := collectHotBackups(m.hotExitRoot, os.Getpid(), pidAlive)
	if err != nil || len(b) != 1 || b[0].Path != p {
		t.Fatalf("backups after leaving: %+v %v", b, err)
	}
}

// Backups that cannot be read are skipped; a backup folder that is a
// file is an error; no home folder means no hot exit.
func TestCovCoreHotExitDiskTrouble(t *testing.T) {
	m := covNvimModel(t, 80, 24) // before HOME goes: nvim would log into the work folder
	root := t.TempDir()
	prev := filepath.Join(root, "previous")
	_ = os.MkdirAll(filepath.Join(prev, "dir.bak"), 0o700)
	_ = os.WriteFile(filepath.Join(prev, "notes.txt"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(prev, "ok.bak"), []byte("/w/a.go\nline"), 0o600)
	b, err := collectHotBackups(root, os.Getpid(), pidAlive)
	if err != nil || len(b) != 1 || b[0].Path != "/w/a.go" {
		t.Errorf("backups %+v err %v", b, err)
	}
	file := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(file, nil, 0o600)
	if _, err := collectHotBackups(file, 1, pidAlive); err == nil {
		t.Error("a file as the backup root gave no error")
	}
	// A dead run whose files cannot move: previous/ is a file.
	root2 := t.TempDir()
	covWriteBackup(t, root2, covDeadPID, "a.bak", "/w/a.go", "x")
	_ = os.WriteFile(filepath.Join(root2, "previous"), nil, 0o600)
	if _, err := collectHotBackups(root2, 1, pidAlive); err == nil {
		t.Error("previous/ as a file gave no error")
	}

	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("HOME", "")
	if _, err := hotExitRootFor("x"); err == nil {
		t.Error("a backup root without HOME")
	}
	if m.startHotExit() != nil || m.hotExitLive != "" {
		t.Error("hot exit started without a home folder")
	}
}
