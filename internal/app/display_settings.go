package app

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/anim"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/termimg"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// Display settings: icons, animations, images (+ the Color Theme picker's
// live preview). cmd/termocode resolves them once at startup — the icon and
// graphics probes need the raw TTY before Bubble Tea owns it — and the
// Settings modal re-applies them live.

// Setting choices (first = default). Docs and the Settings picker read
// these lists.
var (
	iconChoices      = []string{"auto", "nerd", "unicode", "ascii"}
	animationChoices = []string{"auto", "on", "off"}
	imageChoices     = []string{"auto", "kitty", "sixel", "blocks", "off"}
	sessionChoices   = []string{"auto", "on", "off"}
)

// settingChoices returns the fixed choices of an enum setting row.
func settingChoices(id string) []string {
	switch id {
	case "setting-icons":
		return iconChoices
	case "setting-animations":
		return animationChoices
	case "setting-images":
		return imageChoices
	case "setting-persistent-session":
		return sessionChoices
	}
	return nil
}

// DisplayConfig is what cmd/termocode needs before the TUI starts.
type DisplayConfig struct {
	Icons, Animations, Images string
}

// ConfigDisplay reads the display settings from config.json ("" = auto).
func ConfigDisplay() DisplayConfig {
	c := loadSettings()
	return DisplayConfig{Icons: c.Icons, Animations: c.Animations, Images: c.Images}
}

// termCaps is the terminal's graphics support, probed at startup.
var termCaps = termimg.Caps{Protocol: termimg.Blocks}

// unicodeProbeOK is the startup Unicode width verdict. No probe runs
// today (cmd/termocode passes true for icons=auto) and nothing reads it;
// it is kept so a future ProbeUnicode result has a home.
var unicodeProbeOK bool

// nerdFontHint is whether a Nerd Font was detected at startup (cached
// fc-list, see setup.HasNerdFont). ResolveIconMode ignores it today.
var nerdFontHint bool

// SetTerminalCaps records the startup probe results (cmd/termocode).
func SetTerminalCaps(c termimg.Caps, unicodeOK, nerdFont bool) {
	termCaps = c
	unicodeProbeOK = unicodeOK
	nerdFontHint = nerdFont
}

// imageProtocol is the protocol images use with the current setting.
func imageProtocol() termimg.Protocol {
	return termimg.Resolve(termimg.ParseMode(loadSettings().Images), termCaps, theme.TrueColor())
}

// ApplyDisplaySettings resolves icons + animations from the settings and
// the startup probes. Called by cmd/termocode and after a settings change.
func ApplyDisplaySettings(cfg DisplayConfig) {
	theme.CurrentIconMode = theme.ResolveIconMode(cfg.Icons, nerdFontHint, nil, os.Getenv)
	anim.Enabled = anim.Resolve(anim.ParseMode(cfg.Animations), theme.TrueColor(), os.Getenv)
}

// displaySettingValue is the current value of a display setting row.
func displaySettingValue(id string, cfg settingsConfig) (string, bool) {
	v := ""
	switch id {
	case "setting-icons":
		v = cfg.Icons
	case "setting-animations":
		v = cfg.Animations
	case "setting-images":
		v = cfg.Images
	case "setting-persistent-session":
		v = cfg.PersistentSession
	default:
		return "", false
	}
	if v == "" {
		v = "auto"
	}
	return v, true
}

// applyDisplaySetting validates, saves and applies a display setting.
func (m *Model) applyDisplaySetting(id, value string) (tea.Cmd, bool) {
	choices := settingChoices(id)
	if choices == nil {
		return nil, false
	}
	var toastCmd tea.Cmd
	ok := false
	for _, c := range choices {
		if c == value {
			ok = true
		}
	}
	row, _ := findSettingsRow(id)
	if !ok {
		m.toast, toastCmd = m.toast.Push(toast.Errr, fmt.Sprintf("%s: expected one of %v", row.Label, choices))
		return toastCmd, true
	}
	cfg := loadSettings()
	switch id {
	case "setting-icons":
		cfg.Icons = value
	case "setting-animations":
		cfg.Animations = value
	case "setting-images":
		cfg.Images = value
	case "setting-persistent-session":
		cfg.PersistentSession = value
	}
	if err := saveSettings(cfg); err != nil {
		m.toast, toastCmd = m.toast.PushDetail(toast.Errr, "Settings error", err.Error())
		return toastCmd, true
	}
	ApplyDisplaySettings(DisplayConfig{Icons: cfg.Icons, Animations: cfg.Animations, Images: cfg.Images})
	m.applyLayout()
	detail := ""
	switch id {
	case "setting-icons":
		detail = "icons: " + iconModeName(theme.CurrentIconMode)
	case "setting-animations":
		detail = "animations " + map[bool]string{true: "on", false: "off"}[anim.Enabled]
	case "setting-images":
		detail = "images: " + imageProtocol().String()
	case "setting-persistent-session":
		detail = "applies the next time termocode starts"
	}
	m.toast, toastCmd = m.toast.PushDetail(toast.Info, row.Label+" = "+value, detail)
	return toastCmd, true
}

func iconModeName(mo theme.IconMode) string {
	switch mo {
	case theme.IconModeNerdFont:
		return "nerd"
	case theme.IconModeUnicode:
		return "unicode"
	}
	return "ascii"
}

// openSettingChoicePicker shows the fixed choices of an enum setting.
func (m *Model) openSettingChoicePicker(row settingsRow, current string) tea.Cmd {
	items := make([]picker.Item, 0, len(settingChoices(row.ID)))
	for _, c := range settingChoices(row.ID) {
		hint := ""
		if c == current {
			hint = "current"
		}
		items = append(items, picker.Item{ID: c, Title: c, Hint: hint})
	}
	m.picker = picker.NewItems(" "+row.Label+" ", items)
	m.picker.SetSize(m.w, m.h)
	m.picker.SetCursorID(current)
	m.pickerOpen = true
	m.pickerKind = pickerKindSettingChoice
	m.settingChoiceRow = row.ID
	return m.picker.Init()
}

// previewThemeUnderCursor applies the highlighted theme while the Color
// Theme picker is open (live preview, not saved).
func (m *Model) previewThemeUnderCursor() {
	if !m.themePreviewing || m.pickerKind != pickerKindTheme || !m.pickerOpen {
		return
	}
	it, ok := m.picker.Current()
	if !ok || it.ID == m.themeID {
		return
	}
	t, _ := theme.ApplyTheme(it.ID)
	m.theme = t.Styles
	m.themeID = t.ID
	m.applyNvimTheme()
	m.refreshAIHighlights()
}

// endThemePreview stops the live preview; unless the user picked a theme
// (keep = true) the theme from before the picker is restored.
func (m *Model) endThemePreview(keep bool) {
	if !m.themePreviewing {
		return
	}
	m.themePreviewing = false
	if keep || m.themePreviewFrom == "" || m.themePreviewFrom == m.themeID {
		return
	}
	t, _ := theme.ApplyTheme(m.themePreviewFrom)
	m.theme = t.Styles
	m.themeID = t.ID
	m.applyNvimTheme()
	m.refreshAIHighlights()
}
