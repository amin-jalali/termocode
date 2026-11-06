package app

import (
	"os"
	"strings"
	"testing"
)

// GITX-12 (H19): git_auto_fetch is a saved setting (Settings → Git, off by
// default). Toggle Auto Fetch saves it, the Settings row turns the timer on
// and off, and a start with it on starts the timer.
func TestGitxAutoFetchSetting(t *testing.T) {
	path, err := settingsConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(path)
	t.Cleanup(func() { _ = os.Remove(path); gitxAutoFetch = gitxAuto{} })
	gitxAutoFetch = gitxAuto{}
	if gitxAutoFetchSaved() {
		t.Fatal("default: want off")
	}
	if v, ok := gitxSettingValue("setting-git-auto-fetch"); !ok || v != "false" {
		t.Fatalf("value %q %v", v, ok)
	}
	if c := settingsCategories()["setting-git-auto-fetch"]; c != "Git" {
		t.Fatalf("category %q", c)
	}
	if _, ok := aiSettingPolicies["git_auto_fetch"]; !ok {
		t.Fatal("no AI policy for git_auto_fetch")
	}
	if gitxAutoFetchInitCmd() != nil {
		t.Fatal("a start with the setting off starts the timer")
	}

	m, _, _ := gitxUpstreamRepo(t)
	m.dispatchPaletteAction("git-auto-fetch")
	if !m.gitxAutoFetchOn() || !gitxAutoFetchSaved() {
		t.Fatal("toggle on did not save")
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), `"git_auto_fetch": true`) {
		t.Fatalf("config.json:\n%s", b)
	}
	m.dispatchPaletteAction("git-auto-fetch")
	if m.gitxAutoFetchOn() || gitxAutoFetchSaved() {
		t.Fatal("toggle off did not save")
	}

	// The Settings row.
	m.applySetting("setting-git-auto-fetch", "on")
	if !m.gitxAutoFetchOn() || !gitxAutoFetchSaved() {
		t.Fatal("the row did not turn it on")
	}
	m.applySetting("setting-git-auto-fetch", "maybe")
	if !gitxAutoFetchSaved() {
		t.Fatal("a bad value changed the setting")
	}
	m.applySetting("setting-git-auto-fetch", "off")
	if m.gitxAutoFetchOn() || gitxAutoFetchSaved() {
		t.Fatal("the row did not turn it off")
	}

	// A start with the setting on.
	m.applySetting("setting-git-auto-fetch", "true")
	gitxAutoFetch = gitxAuto{}
	if gitxAutoFetchInitCmd() == nil || !gitxAutoFetch.on {
		t.Fatal("a start with the setting on did not start the timer")
	}
}
