package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/diffview"
	"github.com/amin-jalali/termocode/internal/localhist"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Local history (GITX-35; R1–R4, E26): every file the user saves in the
// editor is copied to $XDG_STATE_HOME/termocode/history (50 versions per
// file, 30 days, files up to 2 MB). File: Show Local History lists the
// versions of the active file to compare or restore; File: Recover
// Deleted File… brings back a file of this workspace that is gone; File:
// Put Label in Local History… names the file as it is now. The setting
// local_history (on by default) turns the copies off.

const localHistRowID = "setting-local-history"

func init() {
	settingsRows = append(settingsRows, settingsRow{ID: localHistRowID, Label: "local_history", Kind: "bool", Default: "true",
		Desc: "Keep a copy of each file you save (File: Show Local History, File: Recover Deleted File…): 50 versions per file for 30 days, files up to 2 MB, in $XDG_STATE_HOME/termocode/history. Off keeps no new copies."})
	aiSettingPolicies["local_history"] = aiNever("turns the user's safety copies of saved files off; only the user does that")
	registerNotifyHandler("termocode_local_history", func(m *Model, args []any) tea.Cmd {
		return localHistSaveCmd(notifyArgString(args, 0))
	})
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "local-history", Title: "File: Show Local History", Hint: "the saved versions of this file: compare, restore"},
			{ID: "local-history-label", Title: "File: Put Label in Local History…"},
			{ID: "local-history-deleted", Title: "File: Recover Deleted File…", Hint: "from the local history"},
		},
		policies: map[string]aiActionPolicy{
			"local-history":         aiAuto(),
			"local-history-label":   aiAuto(),
			"local-history-deleted": aiNever("writes a file back from the local history; only the user does that"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "local-history":
				return m.openLocalHistory(), true
			case "local-history-label":
				return m.localHistLabelPrompt(), true
			case "local-history-deleted":
				return m.openLocalHistDeleted(), true
			}
			return nil, false
		},
	})
}

// localHistLua sends each save of a file buffer to Go.
const localHistLua = `
vim.api.nvim_create_autocmd('BufWritePost', { group = vim.api.nvim_create_augroup('TermocodeLocalHistory', { clear = true }), callback = function(ev)
  if vim.bo[ev.buf].buftype == '' and _G.termocode_notify then
    _G.termocode_notify('termocode_local_history', vim.api.nvim_buf_get_name(ev.buf))
  end
end })`

// startLocalHistory installs the save hook (update.go, at start).
func (m *Model) startLocalHistory() {
	if m.nvim != nil {
		_ = m.nvim.ExecLua(localHistLua)
	}
}

// localHistStore is the history folder.
func localHistStore() *localhist.Store {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		state = filepath.Join(home, ".local", "state")
	}
	return &localhist.Store{Dir: filepath.Join(state, "termocode", "history")}
}

// localHistOn reads local_history from config.json (default on).
func localHistOn() bool {
	path, err := settingsConfigPath()
	if err != nil {
		return true
	}
	var s struct {
		On *bool `json:"local_history"`
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s.On == nil || *s.On
}

func localHistSettingValue(id string) (string, bool) {
	if id != localHistRowID {
		return gitxIssueLinkValue(id) // GITX-29 (gitx_autolink.go)
	}
	return strconv.FormatBool(localHistOn()), true
}

func (m *Model) applyLocalHistSetting(id, value string) (tea.Cmd, bool) {
	if id != localHistRowID {
		return m.applyGitxIssueLink(id, value) // GITX-29 (gitx_autolink.go)
	}
	b, err := parseBoolLoose(strings.TrimSpace(value))
	if err != nil {
		return m.pushToastDetail(toast.Errr, "local_history: expected true or false", ""), true
	}
	if err := saveHTTPSetting("local_history", b); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	return m.pushToastDetail(toast.Info, "local_history = "+strconv.FormatBool(b), ""), true
}

// localHistSaveCmd copies a saved file into the history off the UI loop.
func localHistSaveCmd(path string) tea.Cmd {
	if path == "" || !filepath.IsAbs(path) || !localHistOn() {
		return nil
	}
	s := localHistStore()
	if s == nil {
		return nil
	}
	return func() tea.Msg {
		if strings.HasPrefix(path, s.Dir+string(filepath.Separator)) {
			return nil
		}
		fi, err := os.Stat(path)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > localhist.DefaultMaxBytes {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if _, err := s.Save(path, b, time.Now()); err != nil {
			recordError("[local history] " + err.Error())
		}
		return nil
	}
}

// localHistActive is the active file's absolute path.
func (m *Model) localHistActive() (string, bool) {
	p := m.editor.Path()
	if p == "" {
		return "", false
	}
	if !filepath.IsAbs(p) {
		root, err := m.rootDir()
		if err != nil {
			return "", false
		}
		p = filepath.Join(root, p)
	}
	return filepath.Clean(p), true
}

// localHistWhen is a version's row title.
func localHistWhen(v localhist.Version) string {
	s := v.At.Local().Format("2006-01-02 15:04:05")
	if v.Label != "" {
		s += "  · " + v.Label
	}
	return s
}

func localHistAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d/time.Minute))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h ago", int(d/time.Hour))
	}
	return fmt.Sprintf("%d days ago", int(d/(24*time.Hour)))
}

// openLocalHistory lists the versions of the active file.
func (m *Model) openLocalHistory() tea.Cmd {
	abs, ok := m.localHistActive()
	if !ok {
		return m.gitxInfo("Open a file first")
	}
	return m.openLocalHistoryOf(abs)
}

// openLocalHistoryOf lists the versions of the file at abs.
func (m *Model) openLocalHistoryOf(abs string) tea.Cmd {
	s := localHistStore()
	if s == nil {
		return nil
	}
	vs, err := s.Versions(abs)
	if err != nil {
		return m.gitxFail("Could not read the local history", err)
	}
	if len(vs) == 0 {
		return m.gitxInfo("No local history for " + filepath.Base(abs) + " yet: each save keeps a copy")
	}
	items := make([]picker.Item, 0, len(vs))
	for _, v := range vs {
		items = append(items, picker.Item{ID: v.ID, Title: localHistWhen(v), Hint: localHistAgo(v.At) + " · " + humanBytes(v.Size)})
	}
	m.gitxPick(" Local History: "+filepath.Base(abs)+" ", items, func(m *Model, id string) tea.Cmd {
		for _, v := range vs {
			if v.ID == id {
				return m.localHistVersionActions(s, abs, v)
			}
		}
		return nil
	})
	return nil
}

// localHistVersionActions asks what to do with one version.
func (m *Model) localHistVersionActions(s *localhist.Store, abs string, v localhist.Version) tea.Cmd {
	items := []picker.Item{
		{ID: "compare", Title: "Compare with Current", Hint: "this version ↔ the file now"},
		{ID: "restore", Title: "Restore This Version…", Hint: "asks first; the file now is kept in the history"},
	}
	m.gitxPick(" "+localHistWhen(v)+" ", items, func(m *Model, id string) tea.Cmd {
		switch id {
		case "compare":
			return localHistCompareCmd(s, abs, v)
		case "restore":
			return m.localHistRestoreCard(s, abs, v)
		}
		return nil
	})
	return nil
}

func localHistCompareCmd(s *localhist.Store, abs string, v localhist.Version) tea.Cmd {
	return func() tea.Msg {
		old, err := s.Read(abs, v.ID)
		if err != nil {
			return ErrMsg{Err: fmt.Errorf("local history: %w", err)}
		}
		cur, err := os.ReadFile(abs)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return ErrMsg{Err: fmt.Errorf("local history: %w", err)}
		}
		return compareReadyMsg{title: "Local History", file: diffview.File{
			Name: localHistWhen(v) + " ↔ " + filepath.Base(abs), Old: string(old), New: string(cur)}}
	}
}

// localHistDirty reports unsaved edits of abs in the editor.
func (m *Model) localHistDirty(abs string) bool {
	if m.nvim == nil {
		return false
	}
	out, _ := m.nvim.EvalLuaStringArgs(`local b = vim.fn.bufnr(...) if b ~= -1 and vim.bo[b].modified then return 'yes' end return ''`, abs)
	return out == "yes"
}

func (m *Model) localHistRestoreCard(s *localhist.Store, abs string, v localhist.Version) tea.Cmd {
	if m.localHistDirty(abs) {
		return m.gitxInfo("Save or undo the edits of " + filepath.Base(abs) + " first")
	}
	m.gitxCard("Restore "+filepath.Base(abs)+"?",
		"The file goes back to the version of "+localHistWhen(v)+".\nThe file as it is now is kept in the local history, so you can go back.",
		func(m *Model, id string) tea.Cmd {
			if id != "restore" {
				return nil
			}
			return m.localHistRestore(s, abs, v)
		}, confirm.Button{ID: "restore", Title: "Restore", Style: confirm.StylePrimary})
	return nil
}

// localHistRestore writes a version back to abs (the text now is saved
// as a version first) and reloads the open tab.
func (m *Model) localHistRestore(s *localhist.Store, abs string, v localhist.Version) tea.Cmd {
	data, err := s.Read(abs, v.ID)
	if err != nil {
		return m.gitxFail("Restore failed", err)
	}
	if cur, err := os.ReadFile(abs); err == nil {
		if _, err := s.Save(abs, cur, time.Now()); err != nil {
			return m.gitxFail("Restore stopped: the file now could not be kept", err)
		}
	}
	if err := localHistWrite(abs, data); err != nil {
		return m.gitxFail("Restore failed", err)
	}
	_, _ = s.Save(abs, data, time.Now())
	m.localHistReload(abs)
	return tea.Batch(fetchGitCmd(), m.gitxInfo("Restored "+filepath.Base(abs)+" to "+localHistWhen(v)))
}

// localHistWrite writes data to abs through a temp file, keeping the mode.
func localHistWrite(abs string, data []byte) error {
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(abs); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	tmp := abs + ".termocode-restore"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// localHistReload reads abs again in its open tab.
func (m *Model) localHistReload(abs string) {
	if m.nvim != nil {
		_ = m.nvim.ExecLuaArgs(`local b = vim.fn.bufnr(...) if b ~= -1 and vim.api.nvim_buf_is_loaded(b) then vim.api.nvim_buf_call(b, function() vim.cmd('silent! edit!') end) end`, abs)
	}
}

func (m *Model) localHistLabelPrompt() tea.Cmd {
	abs, ok := m.localHistActive()
	if !ok {
		return m.gitxInfo("Open a file first")
	}
	m.gitxPrompt("Put Label in Local History", "Label for "+filepath.Base(abs)+" as saved now:", "", func(m *Model, value string) tea.Cmd {
		s := localHistStore()
		if s == nil || strings.TrimSpace(value) == "" {
			return nil
		}
		data, err := os.ReadFile(abs)
		if err != nil {
			return m.gitxFail("Label not kept", err)
		}
		if err := s.Label(abs, data, value, time.Now()); err != nil {
			return m.gitxFail("Label not kept", err)
		}
		return m.gitxInfo("Label kept: " + strings.TrimSpace(value))
	})
	return nil
}

// openLocalHistDeleted lists the files of this workspace that are gone.
func (m *Model) openLocalHistDeleted() tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		return nil
	}
	s := localHistStore()
	if s == nil {
		return nil
	}
	gone, err := s.Deleted(root)
	if err != nil {
		return m.gitxFail("Could not read the local history", err)
	}
	if len(gone) == 0 {
		return m.gitxInfo("No deleted file of this workspace is in the local history")
	}
	items := make([]picker.Item, 0, len(gone))
	for i, e := range gone {
		rel, _ := filepath.Rel(root, e.Path)
		items = append(items, picker.Item{ID: strconv.Itoa(i), Title: filepath.ToSlash(rel), Hint: "last saved " + localHistAgo(e.Last)})
	}
	m.gitxPick(" Recover Deleted File ", items, func(m *Model, id string) tea.Cmd {
		i, err := strconv.Atoi(id)
		if err != nil || i < 0 || i >= len(gone) {
			return nil
		}
		return m.localHistRecover(s, gone[i].Path)
	})
	return nil
}

// localHistRecover writes the newest version of a deleted file back and
// opens it. A file that is there again is never overwritten.
func (m *Model) localHistRecover(s *localhist.Store, abs string) tea.Cmd {
	if _, err := os.Lstat(abs); err == nil {
		return m.gitxInfo(filepath.Base(abs) + " is there again; nothing to recover")
	}
	vs, err := s.Versions(abs)
	if err != nil || len(vs) == 0 {
		return m.gitxFail("Recover failed", errors.New("no version kept"))
	}
	data, err := s.Read(abs, vs[0].ID)
	if err != nil {
		return m.gitxFail("Recover failed", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return m.gitxFail("Recover failed", err)
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return m.gitxFail("Recover failed", err)
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return m.gitxFail("Recover failed", werr)
	}
	if m.nvim != nil {
		m.ensureEditorWindowCurrent()
		_ = m.nvim.ExecLuaArgs(`vim.cmd('edit ' .. vim.fn.fnameescape(...))`, abs)
	}
	return tea.Batch(fetchGitCmd(), m.gitxInfo("Recovered "+filepath.Base(abs)+" ("+localHistWhen(vs[0])+")"))
}
