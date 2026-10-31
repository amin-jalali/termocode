package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/anim"
	"github.com/amin-jalali/termocode/internal/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// keepDisplayGlobals puts back the display globals a live reload sets
// (icons, animations, RTL, logo), so later tests see the package defaults.
func keepDisplayGlobals(t *testing.T) {
	icons, animOn, rtlWas, logo := theme.CurrentIconMode, anim.Enabled, rtlMode, dashboardLogo
	t.Cleanup(func() {
		theme.CurrentIconMode, anim.Enabled, rtlMode, dashboardLogo = icons, animOn, rtlWas, logo
		invalidateWelcome()
	})
}

func TestLiveSettingsReloadAndInvalidJSON(t *testing.T) {
	keepDisplayGlobals(t)
	withConfigDir(t)
	path, _ := settingsConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte(`{"show_hidden":false}`), 0600)
	m := &Model{}
	m.primeLiveSettings()
	os.WriteFile(path, []byte(`{"show_hidden":true,"notifications":false}`), 0600)
	m.reloadLiveSettings()
	if !m.explorer.HiddenShown() {
		t.Fatal("external edit did not apply")
	}
	os.WriteFile(path, []byte(`{"show_hidden":`), 0600)
	m.reloadLiveSettings()
	if !m.explorer.HiddenShown() {
		t.Fatal("invalid edit reset settings")
	}
	os.WriteFile(path, []byte(`{"show_hidden":false}`), 0600)
	m.reloadLiveSettings()
	if m.explorer.HiddenShown() {
		t.Fatal("false did not apply")
	}
	b, _ := os.ReadFile(path)
	if string(b) != `{"show_hidden":false}` {
		t.Fatal("reload rewrote settings")
	}
}

func TestLiveSettingsDefersActiveAI(t *testing.T) {
	keepDisplayGlobals(t)
	m, _, _, _ := aiSkillsModel(t)
	path, _ := settingsConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte(`{"ai_provider":"ollama","ai_model":"old"}`), 0600)
	m.ai.settings = ai.Settings{Provider: "ollama", Model: "old"}
	m.primeLiveSettings()
	m.ai.streaming = true
	os.WriteFile(path, []byte(`{"ai_provider":"ollama","ai_model":"new"}`), 0600)
	m.reloadLiveSettings()
	if m.ai.settings.Model != "old" {
		t.Fatal("running AI configuration replaced")
	}
	m.ai.streaming = false
	m.reloadLiveSettings()
	if m.ai.settings.Model != "new" {
		t.Fatal("idle AI configuration not reloaded")
	}
}

func TestFilePromptChatTrust(t *testing.T) {
	m, rec, root, _ := aiSkillsModel(t)
	os.WriteFile(root+"/prompt.md", []byte("Repository instructions"), 0600)
	p := ai.Profile{Name: "from-file", Source: ai.ProfileSourceUser, Steps: []ai.ProfileStep{{ID: "one", System: "file://prompt.md"}}}
	m.ai.profiles.Add(p)
	m.ai.profile = p.Name
	m.sendAIChat("hello")
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileTrust || len(rec.reqs) != 0 {
		t.Fatal("repo prompt bypassed trust")
	}
	driveAIChat(t, m, m.answerAIProfileTrust("use"), nil)
	if len(rec.reqs) != 1 || !strings.Contains(rec.reqs[0].Messages[0].Content, "Repository instructions") {
		t.Fatal("file prompt not sent")
	}
	os.WriteFile(root+"/prompt.md", []byte("Changed instructions"), 0600)
	m.sendAIChat("again")
	if !m.confirmOpen || m.confirmKind != confirmKindAIProfileTrust || len(rec.reqs) != 1 {
		t.Fatal("changed file kept old trust")
	}
}

// FIX-44: no timer polls settings.json; idle messages never read it. A
// focus-in reads it at once, and a key or a click reads it at most once
// per settingsCheckGap.
func TestLiveSettingsNoIdlePoll(t *testing.T) {
	keepDisplayGlobals(t)
	withConfigDir(t)
	path, _ := settingsConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte(`{"show_hidden":false}`), 0600)
	m := &Model{}
	m.primeLiveSettings()
	reads := settingsSnapshotReads.Load()
	now := time.Unix(1000, 0)
	os.WriteFile(path, []byte(`{"show_hidden":true}`), 0600)
	for i := 0; i < 50; i++ {
		m.liveSettingsOnMsg(gitAutoRefreshMsg{}, now.Add(time.Duration(i)*time.Minute))
	}
	if settingsSnapshotReads.Load() != reads || m.explorer.HiddenShown() {
		t.Fatal("an idle message read settings.json")
	}
	m.liveSettingsOnMsg(tea.FocusMsg{}, now)
	if !m.explorer.HiddenShown() {
		t.Fatal("focus-in did not reload")
	}
	os.WriteFile(path, []byte(`{"show_hidden":false}`), 0600)
	reads = settingsSnapshotReads.Load()
	key := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}
	m.liveSettingsOnMsg(key, now.Add(time.Second))
	if settingsSnapshotReads.Load() != reads || !m.explorer.HiddenShown() {
		t.Fatal("a key right after a check read settings.json again")
	}
	m.liveSettingsOnMsg(key, now.Add(settingsCheckGap))
	if m.explorer.HiddenShown() {
		t.Fatal("a key after the gap did not reload")
	}
	src, _ := os.ReadFile("model.go")
	if strings.Contains(string(src), "settingsReload") {
		t.Fatal("Init still starts a settings timer")
	}
}
