package app

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/toast"
)

// The git_auto_fetch setting (GITX-12, H19): Toggle Auto Fetch saves it,
// so the timer comes back at the next start. settingsConfig does not know
// the key; saveSettings keeps unknown keys.

const gitxAutoFetchRowID = "setting-git-auto-fetch"

func init() {
	settingsRows = append(settingsRows, settingsRow{ID: gitxAutoFetchRowID, Label: "git_auto_fetch", Kind: "bool", Default: "false",
		Desc: "Fetch from the remote every 3 minutes in the background while termocode runs (Git: Toggle Auto Fetch). A failed fetch turns it off for this session and says why."})
	aiSettingPolicies["git_auto_fetch"] = aiNever("starts a timer that talks to the remote; only the user turns it on")
}

// gitxAutoFetchSaved reads git_auto_fetch from config.json (default off).
func gitxAutoFetchSaved() bool {
	path, err := settingsConfigPath()
	if err != nil {
		return false
	}
	var s struct {
		On *bool `json:"git_auto_fetch"`
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s)
	}
	return s.On != nil && *s.On
}

// gitxAutoFetchInit turns auto fetch on at start when the setting is on.
func gitxAutoFetchInit() {
	if gitxAutoFetchSaved() {
		gitxAutoFetchStart()
	}
}

// gitxSettingsCategories puts the row in the Git Settings group.
func gitxSettingsCategories(cats map[string]string) map[string]string {
	cats[gitxAutoFetchRowID] = "Git"
	for _, id := range []string{gitxChecksRowID, gitxFormatRowID, gitxTaskRowID, gitxLargeRowID} {
		cats[id] = "Git" // GITX-27 (gitx_commit_checks.go)
	}
	cats[localHistRowID] = "Editor" // GITX-35 (gitx_local_history.go)
	return cats
}

// gitxSettingValue renders the row's value. ok=false: not ours.
func gitxSettingValue(id string) (string, bool) {
	if id != gitxAutoFetchRowID {
		return gitxCheckSettingValue(id) // GITX-27 (gitx_commit_checks.go)
	}
	return strconv.FormatBool(gitxAutoFetchSaved()), true
}

// applyGitxSetting checks, saves and applies the row. ok=false: not ours.
func (m *Model) applyGitxSetting(id, value string) (tea.Cmd, bool) {
	if id != gitxAutoFetchRowID {
		return m.applyGitxCheckSetting(id, value) // GITX-27 (gitx_commit_checks.go)
	}
	b, err := parseBoolLoose(strings.TrimSpace(value))
	if err != nil {
		return m.pushToastDetail(toast.Errr, "git_auto_fetch: expected true or false", ""), true
	}
	if b == gitxAutoFetch.on {
		if err := saveHTTPSetting("git_auto_fetch", b); err != nil {
			return m.pushToastDetail(toast.Errr, "Settings error", err.Error()), true
		}
		return m.pushToastDetail(toast.Info, "git_auto_fetch = "+strconv.FormatBool(b), ""), true
	}
	return m.toggleAutoFetch(), true
}

// saveAutoFetch stores the timer's state as the setting.
func saveAutoFetch(on bool) error { return saveHTTPSetting("git_auto_fetch", on) }
