package app

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// G-UPD: the update_check setting (Settings → Updates). settingsConfig
// does not know the key; saveSettings keeps unknown keys.

const updateCheckRowID = "setting-update-check"

func init() {
	settingsRows = append(settingsRows, settingsRow{ID: updateCheckRowID, Label: "update_check", Kind: "bool", Default: "true",
		Desc: "Look for a newer termocode release once a day, in the background (GitHub, no account needed). Dev builds never look."})
}

// updateCheckOn reads update_check from config.json (default true).
func updateCheckOn() bool {
	path, err := settingsConfigPath()
	if err != nil {
		return true
	}
	var s struct {
		UpdateCheck *bool `json:"update_check"`
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s.UpdateCheck == nil || *s.UpdateCheck
}

// updateSettingsCategories puts the row in its own Settings group.
func updateSettingsCategories(cats map[string]string) map[string]string {
	cats[updateCheckRowID] = "Updates"
	return cats
}

// updateSettingValue renders the row's value. ok=false: not ours.
func updateSettingValue(id string) (string, bool) {
	if id != updateCheckRowID {
		return "", false
	}
	return strconv.FormatBool(updateCheckOn()), true
}

// applyUpdateSetting checks and saves the row. ok=false: not ours.
func (m *Model) applyUpdateSetting(id, value string) (tea.Cmd, bool) {
	if id != updateCheckRowID {
		return nil, false
	}
	b, err := parseBoolLoose(strings.TrimSpace(value))
	if err != nil {
		return m.pushToastDetail(toast.Errr, "update_check: expected true or false", ""), true
	}
	if err := saveHTTPSetting("update_check", b); err != nil {
		return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
	}
	return m.pushToastDetail(toast.Info, "update_check = "+strconv.FormatBool(b), ""), true
}
