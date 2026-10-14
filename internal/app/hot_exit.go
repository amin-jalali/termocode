package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Hot exit: like VSCode, unsaved buffers survive a crash, a kill or a
// lost terminal. nvim writes every modified file buffer to a backup dir a
// moment after each change (hotExitLua), so even a SIGKILL or an nvim
// that died with the terminal loses at most the last ~half second. On a
// signal or a panic cmd/termocode also asks for one last write
// (HotExitFlush). A clean Quit removes the backup; the next start in the
// same folder offers to restore whatever is left.
//
// Layout: <state>/termocode/backup/<workspace key>/<pid>/<hash>.bak — one
// dir per running termocode, so two instances on one folder never delete
// each other's files. Each .bak file is the buffer's path on the first
// line, then its lines. Leftovers of dead processes are moved to
// .../previous/ until the user answers Restore or Discard. A backup
// nobody restored ages out after hotExitMaxAge, and previous/ keeps at most
// hotExitMaxKept files (pruneHotBackups).

// hotExitMaxAge and hotExitMaxKept bound previous/: "Later" keeps the
// backups, so without a limit they would pile up forever.
const (
	hotExitMaxAge  = 7 * 24 * time.Hour
	hotExitMaxKept = 50
)

// hotExitNvim is the nvim the signal handler flushes (set on ReadyMsg).
var hotExitNvim atomic.Pointer[nvim.Client]

// hotExitRoot returns the backup dir of the workspace folder ws.
func hotExitRoot(ws string) (string, error) {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "termocode", "backup", session.Key(ws)), nil
}

// hotBackup is one restorable buffer.
type hotBackup struct {
	Path  string
	Lines []string
}

// parseHotBackup reads the .bak format: path, then the buffer lines.
func parseHotBackup(b []byte) (hotBackup, bool) {
	path, rest, ok := strings.Cut(string(b), "\n")
	if !ok || path == "" {
		return hotBackup{}, false
	}
	return hotBackup{Path: path, Lines: strings.Split(rest, "\n")}, true
}

// collectHotBackups moves the backups of finished processes (dead pid, or
// our own pid: Reload Window re-execs in the same process) into
// root/previous and returns everything there. A newer backup of the same
// file replaces an older one.
func collectHotBackups(root string, self int, alive func(pid int) bool) ([]hotBackup, error) {
	ents, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	prev := filepath.Join(root, "previous")
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || !e.IsDir() || (pid != self && alive(pid)) {
			continue
		}
		dir := filepath.Join(root, e.Name())
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".bak") {
				continue
			}
			if err := os.MkdirAll(prev, 0o700); err != nil {
				return nil, err
			}
			_ = os.Rename(filepath.Join(dir, f.Name()), filepath.Join(prev, f.Name()))
		}
		_ = os.RemoveAll(dir)
	}
	files, err := pruneHotBackups(prev, clock.Now())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return readHotBackups(prev, files), nil
}

// readHotBackups reads the .bak files among files in dir.
func readHotBackups(dir string, files []os.DirEntry) []hotBackup {
	var out []hotBackup
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".bak") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			continue
		}
		if hb, ok := parseHotBackup(b); ok {
			out = append(out, hb)
		}
	}
	return out
}

// pruneHotBackups deletes the files in prev older than hotExitMaxAge and
// all but the newest hotExitMaxKept (by modification time: the last write
// of the backup). It returns the files that are left.
func pruneHotBackups(prev string, now time.Time) ([]os.DirEntry, error) {
	files, err := os.ReadDir(prev)
	if err != nil {
		return nil, err
	}
	type aged struct {
		f   os.DirEntry
		mod time.Time
	}
	var keep []aged
	for _, f := range files {
		info, err := f.Info()
		if err != nil {
			continue
		}
		if now.Sub(info.ModTime()) > hotExitMaxAge {
			_ = os.Remove(filepath.Join(prev, f.Name()))
			continue
		}
		keep = append(keep, aged{f, info.ModTime()})
	}
	sort.SliceStable(keep, func(i, j int) bool { return keep[i].mod.After(keep[j].mod) })
	out := make([]os.DirEntry, 0, len(keep))
	for i, a := range keep {
		if i >= hotExitMaxKept {
			_ = os.Remove(filepath.Join(prev, a.f.Name()))
			continue
		}
		out = append(out, a.f)
	}
	if len(out) == 0 {
		_ = os.Remove(prev)
	}
	return out, nil
}

// hotExitFoundMsg carries the backups left by an earlier run.
type hotExitFoundMsg struct{ backups []hotBackup }

// hotExitReloadMark in a live backup dir says the run before was a Reload
// Window (the same process re-executed itself). Its backups are no crash:
// they come back at once, without the Restore question.
const hotExitReloadMark = "reload"

// startHotExit turns on the live backup in nvim and looks for backups of
// an earlier run (called on nvim.ReadyMsg).
func (m *Model) startHotExit() tea.Cmd {
	if m.nvim == nil {
		return nil
	}
	ws, err := os.Getwd()
	if err != nil {
		return nil
	}
	root, err := hotExitRoot(ws)
	if err != nil {
		return nil
	}
	m.hotExitRoot = root
	self := os.Getpid()
	live := filepath.Join(root, strconv.Itoa(self))
	// After a Reload Window the live dir of this pid holds the backups of
	// the image before; they are put back below. Otherwise (a re-exec
	// that did not flush, e.g. a crash handler) they count as leftovers.
	var reloaded []hotBackup
	if os.Remove(filepath.Join(live, hotExitReloadMark)) == nil {
		files, _ := os.ReadDir(live)
		reloaded = readHotBackups(live, files)
	}
	alive, collectSelf := pidAlive, self
	if len(reloaded) > 0 {
		alive = func(pid int) bool { return pid == self || pidAlive(pid) }
		collectSelf = 0 // keep our own dir: it is the live one again
	}
	backups, err := collectHotBackups(root, collectSelf, alive)
	if err != nil {
		recordError("hot exit: " + err.Error())
	}
	if err := m.nvim.ExecLuaArgs(hotExitLua, live); err != nil {
		recordError("hot exit: " + err.Error())
	}
	m.hotExitLive = live
	hotExitNvim.Store(m.nvim)
	var cmds []tea.Cmd
	if len(reloaded) > 0 {
		if err := m.restoreHotBackups(reloaded, true); err != nil {
			recordError("hot exit: reload restore: " + err.Error())
			// Not lost: ask about them like after a crash.
			more, _ := collectHotBackups(root, self, pidAlive)
			backups = more
		} else {
			cmds = append(cmds, m.fetchStateCmd())
		}
	}
	if len(backups) > 0 {
		cmds = append(cmds, func() tea.Msg { return hotExitFoundMsg{backups: backups} })
	}
	return tea.Batch(cmds...)
}

// restoreHotBackups puts the backed-up text into the buffers of its files
// (modified, so the user reviews and saves) and writes the live backup.
// keepCurrent leaves the current window alone (a reload: the session has
// already reopened the files); otherwise the first file is shown.
func (m *Model) restoreHotBackups(b []hotBackup, keepCurrent bool) error {
	paths := make([]string, len(b))
	contents := make([][]string, len(b))
	for i, x := range b {
		paths[i], contents[i] = x.Path, x.Lines
	}
	if err := m.nvim.ExecLuaArgs(hotExitRestoreLua, paths, contents, keepCurrent); err != nil {
		return err
	}
	return m.nvim.ExecLua(hotExitFlushLua)
}

// flushHotExitForReload writes every unsaved buffer to the live backup
// now (the debounce timer may not have fired yet: the last keys typed
// before the reload would be lost) and marks the dir as a reload, so the
// new image restores it without asking. Called right before nvim closes
// for the re-exec; nothing writes the dir after that.
func (m *Model) flushHotExitForReload() {
	if m.nvim == nil || m.hotExitLive == "" {
		return
	}
	if err := m.nvim.ExecLua(hotExitFlushLua); err != nil {
		recordError("hot exit: " + err.Error())
		return
	}
	if _, err := os.Stat(m.hotExitLive); err != nil {
		return // nothing unsaved
	}
	_ = os.WriteFile(filepath.Join(m.hotExitLive, hotExitReloadMark), nil, 0o600)
}

// leaveWorkspace closes nvim before Open Workspace re-execs termocode in
// another folder. The unsaved buffers are written to the live backup
// first (the debounce timer may not have fired yet), then moved to
// previous/: the re-exec keeps our pid alive in the other folder, so a
// live dir of this pid would look busy to the next start here. That
// start offers the text back.
func (m *Model) leaveWorkspace() {
	if m.nvim != nil && m.hotExitLive != "" {
		if err := m.nvim.ExecLua(hotExitFlushLua); err != nil {
			recordError("hot exit: " + err.Error())
		}
	}
	hotExitNvim.Store(nil)
	if m.nvim != nil {
		_ = m.nvim.Close()
	}
	if m.hotExitRoot != "" {
		if _, err := collectHotBackups(m.hotExitRoot, os.Getpid(), pidAlive); err != nil {
			recordError("hot exit: " + err.Error())
		}
	}
}

// showWaitingHotExit asks about the backups found at start once no other
// dialog is open: promptHotExit replaces m.confirm, so asking earlier
// would throw away the question on screen (and a prompt dialog takes the
// keys before a confirm).
func (m *Model) showWaitingHotExit() {
	if len(m.hotExitWaiting) == 0 || m.confirmOpen || m.promptOpen {
		return
	}
	b := m.hotExitWaiting
	m.hotExitWaiting = nil
	m.promptHotExit(b)
}

// promptHotExit asks what to do with the backups of an earlier run.
func (m *Model) promptHotExit(b []hotBackup) {
	m.hotExitPending = b
	names := make([]string, 0, len(b))
	for _, x := range b {
		names = append(names, filepath.Base(x.Path))
	}
	body := fmt.Sprintf("termocode did not quit cleanly last time. %q had unsaved changes.", names[0])
	if len(names) > 1 {
		shown := names
		if len(shown) > 3 {
			shown = append(shown[:3:3], "…")
		}
		body = fmt.Sprintf("termocode did not quit cleanly last time. %d files had unsaved changes: %s.", len(names), strings.Join(shown, ", "))
	}
	m.confirm = confirm.New("Restore unsaved changes?", body, []confirm.Button{
		{ID: "restore", Title: "Restore", Style: confirm.StylePrimary},
		{ID: "discard", Title: "Discard", Style: confirm.StyleDestructive},
		{ID: "later", Title: "Later"},
	})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindHotExit
}

// onHotExitConfirm handles Restore / Discard / Later.
func (m *Model) onHotExitConfirm(id string) tea.Cmd {
	backups := m.hotExitPending
	m.hotExitPending = nil
	prev := filepath.Join(m.hotExitRoot, "previous")
	var cmd tea.Cmd
	switch id {
	case "restore":
		if m.nvim == nil || len(backups) == 0 {
			return nil
		}
		if err := m.restoreHotBackups(backups, false); err != nil {
			m.toast, cmd = m.toast.PushDetail(toast.Errr, "Restore failed", err.Error())
			return cmd
		}
		// The live backup now holds them again (they are modified).
		_ = os.RemoveAll(prev)
		label := "Restored 1 unsaved file"
		if len(backups) > 1 {
			label = fmt.Sprintf("Restored %d unsaved files", len(backups))
		}
		m.toast, cmd = m.toast.Push(toast.Info, label)
		return tea.Batch(cmd, m.fetchStateCmd())
	case "discard":
		_ = os.RemoveAll(prev)
	}
	return nil
}

// dropHotExitBackup removes this run's live backup after a clean quit
// (nvim is closed first, so nothing writes it again).
func (m *Model) dropHotExitBackup() {
	hotExitNvim.Store(nil)
	if m.hotExitLive != "" {
		_ = os.RemoveAll(m.hotExitLive)
	}
	if m.hotExitRoot != "" {
		_ = os.Remove(m.hotExitRoot) // only when empty
	}
}

// HotExitFlush writes the unsaved buffers to the hot-exit backup now.
// cmd/termocode calls it on SIGHUP / SIGTERM and after a crash. A
// terminal hangup does not reach nvim (it runs in its own session, see
// nvim.New), so it is still there to write. If it is gone anyway, the
// last debounced backup stands.
func HotExitFlush(timeout time.Duration) {
	c := hotExitNvim.Load()
	if c == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.ExecLua(hotExitFlushLua)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
