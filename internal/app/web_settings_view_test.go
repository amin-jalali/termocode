package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func webTestSettings(t *testing.T, frames []map[string]any) (webproto.SettingsSnap, int) {
	t.Helper()
	var s webproto.SettingsSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "settings" {
			s = webproto.SettingsSnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

func webTestSetting(s webproto.SettingsSnap, key string) (webproto.SettingItem, bool) {
	for _, it := range s.Items {
		if it.Key == key {
			return it, true
		}
	}
	return webproto.SettingItem{}, false
}

// Spec (WEB-46, web design 2.9): the settings topic lists only the
// allow-listed keys, with the laptop's values; credential, trust and web
// keys never show, whatever the file holds. A change of the file sends
// the list again (checked on Update, at most twice a second); nothing
// changed sends nothing. There is no op that writes a setting.
func TestSettingsAllowlist(t *testing.T) {
	m := aiTestModel(t)
	withConfigDir(t)
	tok := "sk-" + "test-settings-0001"
	writeUserConfig(t, `{"theme":"one-dark","tab_size":2,"ai_base_url":"https://u:`+tok+`@x.example/v1",
		"secret_store":"keyring-zz","web_panel":"on","ai_auto_approve":["shell"],"mcp_server_auto_approve":{"a":["b"]},
		"mcp_trusted":{"x":true}}`)
	now := webTestClock(t)
	webTestHub(t, &m)
	c := webTestSub(t, &m, "settings")
	s, n := webTestSettings(t, webTestNow(t, c))
	if n != 1 || len(s.Items) == 0 {
		t.Fatalf("snap n=%d %+v", n, s)
	}
	for _, it := range s.Items {
		if !webSettingsAllow[it.Key] {
			t.Errorf("%s is not on the allowlist", it.Key)
		}
		for _, bad := range []string{"secret", "web_", "auto_approve", "trusted", "base_url", "api_key", "token"} {
			if strings.Contains(it.Key, bad) {
				t.Errorf("%s listed", it.Key)
			}
		}
	}
	if theme, _ := webTestSetting(s, "theme"); theme.Value != "one-dark" || theme.Group != "Appearance" {
		t.Errorf("theme %+v", theme)
	}
	if tab, _ := webTestSetting(s, "tab_size"); tab.Value != "2" || tab.Type != "int" || tab.Default != "4" {
		t.Errorf("tab_size %+v", tab)
	}
	if raw, _ := webData(s); strings.Contains(string(raw), tok) || strings.Contains(string(raw), "keyring-zz") {
		t.Fatalf("a secret or a hidden key's value left: %s", raw)
	}
	for _, p := range webproto.Ops() {
		if strings.HasPrefix(p.Op, "settings.") {
			t.Errorf("%s: the web cannot change settings", p.Op)
		}
	}

	m.webAfterUpdate(nil)
	if _, n := webTestSettings(t, webTestNow(t, c)); n != 0 {
		t.Fatal("a list sent with nothing changed")
	}
	writeUserConfig(t, `{"theme":"one-dark","tab_size":8}`)
	m.webAfterUpdate(nil) // checked too soon: waits
	if _, n := webTestSettings(t, webTestNow(t, c)); n != 0 {
		t.Fatal("checked before the half second")
	}
	*now = now.Add(time.Second)
	m.webAfterUpdate(nil)
	s, n = webTestSettings(t, webTestNow(t, c))
	if tab, _ := webTestSetting(s, "tab_size"); n != 1 || tab.Value != "8" {
		t.Fatalf("after the change n=%d tab=%+v", n, tab)
	}
}

// Spec (WEB-46, WEB-7): the settings replay golden (three keys, so a
// new setting elsewhere does not move it).
func TestSettingsReplayGolden(t *testing.T) {
	old := webSettingsAllow
	webSettingsAllow = map[string]bool{"theme": true, "word_wrap": true, "tab_size": true}
	t.Cleanup(func() { webSettingsAllow = old })
	m := aiTestModel(t)
	withConfigDir(t)
	writeUserConfig(t, `{"theme":"one-dark"}`)
	now := webTestClock(t)
	h := webTestHub(t, &m)
	c := webReplayRecord(t, &m, h, "settings", func() {
		writeUserConfig(t, `{"theme":"one-dark","word_wrap":true,"tab_size":2}`)
		*now = now.Add(time.Second)
		m.webAfterUpdate(nil)
	})
	if len(c.Deltas) != 1 {
		t.Fatalf("deltas %d", len(c.Deltas))
	}
	webReplayCheck(t, h.topics["settings"], c)
	webReplayGolden(t, webReplayPath("settings", "change"), c)
}
