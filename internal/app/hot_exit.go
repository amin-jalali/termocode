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
	var out []hotBackup
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(prev, f.Name()))
		if err != nil {
			continue
		}
		if hb, ok := parseHotBackup(b); ok {
			out = append(out, hb)
		}
	}
	return out, nil
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
	live := filepath.Join(root, strconv.Itoa(os.Getpid()))
	// Leftovers first: the live dir of this pid may hold the backups of
	// the image before a Reload Window.
	backups, err := collectHotBackups(root, os.Getpid(), pidAlive)
	if err != nil {
		recordError("hot exit: " + err.Error())
	}
	if err := m.nvim.ExecLuaArgs(hotExitLua, live); err != nil {
		recordError("hot exit: " + err.Error())
	}
	m.hotExitLive = live
	hotExitNvim.Store(m.nvim)
	if len(backups) == 0 {
		return nil
	}
	return func() tea.Msg { return hotExitFoundMsg{backups: backups} }
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
		paths := make([]string, len(backups))
		contents := make([][]string, len(backups))
		for i, b := range backups {
			paths[i], contents[i] = b.Path, b.Lines
		}
		if err := m.nvim.ExecLuaArgs(hotExitRestoreLua, paths, contents); err != nil {
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
// cmd/termocode calls it on SIGHUP / SIGTERM and after a crash. nvim may
// be gone already (it gets the same hangup); then the last periodic
// backup stands.
func HotExitFlush(timeout time.Duration) {
	c := hotExitNvim.Load()
	if c == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.ExecLua(`if _G.termocode_hot_exit_flush then _G.termocode_hot_exit_flush() end`)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
