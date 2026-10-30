//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The v2 pages under More (WEB-43 … WEB-46) against the fake hub.

// openMoreSub logs in and opens More › the link of sub, which asks for
// topic.
func openMoreSub(t *testing.T, vp Viewport, sub, topic string) (*Page, *HubConn) {
	t.Helper()
	s := StartServer(t)
	p := NewPage(t, s, vp)
	p.Media(map[string]string{"prefers-reduced-motion": "reduce"})
	c := p.Login("c-more2")
	p.GoTab("more")
	p.TapJS(`document.querySelector('.more-link[data-sub="` + sub + `"]')`)
	c.WaitSub(t, topic)
	return p, c
}

// Settings list the laptop's values by group, read only: a changed value
// shows its default, there is no field and no button that changes one.
func TestSettingsReadOnly(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openMoreSub(t, vp, "settings", "settings")
			c.Send(t, Snap("settings", 1, webproto.SettingsSnap{Items: []webproto.SettingItem{
				{Key: "theme", Group: "Appearance", Type: "string", Value: "one-dark", Default: "vscode-dark-plus", Description: "Color theme ID."},
				{Key: "tab_size", Group: "Editor", Type: "int", Value: "4", Default: "4"},
				{Key: "word_wrap", Group: "Editor", Type: "bool", Value: "<b>true</b>", Default: "false"},
			}}))
			p.WaitJS(`document.querySelectorAll(".kv-row").length === 3`)
			if n := Eval[int](p, `document.querySelectorAll(".settings .group-title").length`); n != 2 {
				t.Errorf("%d groups", n)
			}
			if v := p.Text(`.kv-row[data-key="theme"] .kv-value`); v != "one-dark" {
				t.Errorf("theme value %q", v)
			}
			if d := p.Text(`.kv-row[data-key="theme"] .kv-def`); d != "default vscode-dark-plus" {
				t.Errorf("theme default %q", d)
			}
			if n := Eval[int](p, `document.querySelectorAll('.kv-row[data-key="tab_size"] .kv-def').length`); n != 0 {
				t.Error("an unchanged value shows its default")
			}
			if n := Eval[int](p, `document.querySelectorAll(".settings b, .settings input, .settings select, .settings textarea, .settings .btn").length`); n != 0 {
				t.Fatalf("%d fields, buttons or HTML on the settings page", n)
			}
			if !strings.Contains(p.Text(".settings-note"), "Read only") {
				t.Errorf("note %q", p.Text(".settings-note"))
			}
			c.Send(t, Delta("settings", 2, webproto.SettingsSnap{Items: []webproto.SettingItem{{Key: "tab_size", Group: "Editor", Type: "int", Value: "8", Default: "4"}}}))
			p.WaitJS(`document.querySelectorAll(".kv-row").length === 1`)
			p.noOverflow("settings")
		})
	}
}
