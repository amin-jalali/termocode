package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
)

type liveSettingsState struct {
	hash                             [32]byte
	seen, aiPending, profilesPending bool
	// checked: when the files were last read (FIX-44).
	checked time.Time
}

// settingsCheckGap: a key or a click reads the settings files at most
// this often. No timer polls them, so an idle app uses no CPU (FIX-44).
const settingsCheckGap = 2 * time.Second

// settingsSnapshotReads counts settingsSnapshot calls (tests).
var settingsSnapshotReads atomic.Int64

// liveSettingsOnMsg reloads changed settings files when the user is
// back: on a focus-in at once, on a key or a click at most once per
// settingsCheckGap. Other messages only apply AI changes that waited for
// a request to finish (no file reads).
func (m *Model) liveSettingsOnMsg(msg tea.Msg, now time.Time) {
	st := &m.extras.liveSettings
	check := false
	switch msg.(type) {
	case tea.FocusMsg:
		check = true
	case tea.KeyMsg, tea.MouseMsg:
		check = now.Sub(st.checked) >= settingsCheckGap
	}
	if check {
		st.checked = now
		m.reloadLiveSettings()
		return
	}
	if st.aiPending || st.profilesPending {
		m.applyPendingLiveSettings()
	}
}

func settingsSnapshot() ([]byte, [32]byte) {
	settingsSnapshotReads.Add(1)
	path, _ := settingsConfigPath()
	b := reportRead(path, false)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		b = []byte("{}")
	}
	h := sha256.New()
	h.Write(b)
	user, _ := ai.ProfilesPath()
	for _, p := range []string{user, ai.ProjectProfilesPath(aiWorkspaceRoot())} {
		h.Write([]byte{0})
		h.Write(reportRead(p, false))
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return b, sum
}

func (m *Model) primeLiveSettings() {
	_, sum := settingsSnapshot()
	m.extras.liveSettings = liveSettingsState{hash: sum, seen: true}
}

// Invalid and half-written config files leave the last working setup alone.
// AI changes wait until all active requests finish. No reload writes config.
func (m *Model) reloadLiveSettings() tea.Cmd {
	b, sum := settingsSnapshot()
	st := &m.extras.liveSettings
	if !st.seen {
		m.primeLiveSettings()
		return nil
	}
	changed := sum != st.hash
	if changed {
		var cfg settingsConfig
		var nextAI ai.Settings
		var object map[string]json.RawMessage
		if json.Unmarshal(b, &cfg) != nil || json.Unmarshal(b, &nextAI) != nil || json.Unmarshal(b, &object) != nil || object == nil {
			return nil
		}
		st.hash = sum
		quiet := m.toast
		on := func(v *bool) bool { return v != nil && *v }
		if cfg.Theme != "" && cfg.Theme != m.themeID {
			if _, ok := theme.LookupTheme(cfg.Theme); ok {
				m.applyThemeLive(cfg.Theme)
			}
		}
		ApplyDisplaySettings(DisplayConfig{Icons: cfg.Icons, Animations: cfg.Animations, Images: cfg.Images, Logo: cfg.DashboardLogo, RTL: cfg.RTL})
		m.skipShell = keymap.SkipShellSet(cfg.TerminalSkipShell)
		if m.autoSaveOn != on(cfg.AutoSave) {
			_ = m.toggleAutoSave()
		}
		m.setWrapAll(on(cfg.WordWrap))
		if m.explorer.HiddenShown() != on(cfg.ShowHidden) {
			_ = m.toggleHiddenFiles()
		}
		if m.nvim != nil {
			n := 4
			if cfg.TabSize != nil && *cfg.TabSize >= 1 && *cfg.TabSize <= 16 {
				n = *cfg.TabSize
			}
			_ = m.nvim.Command(fmt.Sprintf("setglobal tabstop=%d shiftwidth=%d", n, n))
		}
		m.toast = quiet
		if m.ai != nil {
			if !reflect.DeepEqual(m.ai.settings, nextAI) {
				st.aiPending = true
			}
			st.profilesPending = true
		}
		m.applyLayout()
	}
	m.applyPendingLiveSettings()
	return nil
}

// applyPendingLiveSettings reloads the AI setup once no request runs.
func (m *Model) applyPendingLiveSettings() {
	st := &m.extras.liveSettings
	if st.aiPending && m.ai != nil && !m.ai.streaming && m.ai.actionCancel == nil && m.ai.inlineCancel == nil {
		m.ai.reload()
		st.aiPending = false
		st.profilesPending = false
	}
	if st.profilesPending && m.ai != nil && !m.ai.streaming {
		m.ai.reloadProfiles()
		st.profilesPending = false
	}
}
