package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
)

type settingsReloadTickMsg struct{}
type liveSettingsState struct {
	hash                             [32]byte
	seen, aiPending, profilesPending bool
}

func settingsReloadTick() tea.Cmd {
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return settingsReloadTickMsg{} })
}

func settingsSnapshot() ([]byte, [32]byte) {
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
	if st.aiPending && m.ai != nil && !m.ai.streaming && m.ai.actionCancel == nil && m.ai.inlineCancel == nil {
		m.ai.reload()
		st.aiPending = false
		st.profilesPending = false
	}
	if st.profilesPending && m.ai != nil && !m.ai.streaming {
		m.ai.reloadProfiles()
		st.profilesPending = false
	}
	return nil
}
