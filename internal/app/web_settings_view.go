package app

import (
	"os"
	"reflect"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The settings topic (WEB-46, web design 2.9): the laptop's settings,
// for the allow-listed keys only. A key that holds or
// leads to a credential (ai_base_url, secret_*), changes trust
// (ai_auto_approve, mcp_server_auto_approve, mcp_trusted*) or belongs to
// the web panel itself (web_*) is never on the list: the list names what
// may be shown, so a new setting stays hidden until someone adds it here.
// settings.set (the phone's Settings page) changes a listed key only.
// The values come from the same code as the
// laptop's Settings rows; the list is sent again when the config files
// changed (checked on Update, at most every webSettingsEvery; never a
// web timer).

const webSettingsEvery = 500 * time.Millisecond

// webSettingsAllow is the allowlist of keys the page may read.
var webSettingsAllow = map[string]bool{
	"theme": true, "icons": true, "animations": true, "images": true, "dashboard_logo": true, "font_delta": true,
	"auto_save": true, "word_wrap": true, "show_hidden": true, "key_hints": true, "tab_size": true,
	"persistent_session": true, "auto_reconnect": true, "search_max_results": true,
	"ai_inline": true, "ai_next_edit": true, "ai_provider": true, "ai_model": true,
	"ai_until_done_rounds": true, "ai_until_done_budget_usd": true,
	"http_timeout": true, "http_max_redirects": true, "http_max_body_mb": true, "http_ai_max_kb": true,
	"http_tls_insecure": true, "http_cookie_jar": true, "http_history_max": true,
	"mcp_enabled": true, "mcp_idle_timeout": true, "mcp_call_timeout": true, "mcp_max_result_kb": true,
	"mcp_trust_annotations": true, "mcp_share": true, "mcp_strict": true,
	"update_check": true, "worktree_auto_archive_days": true, "worktree_max": true,
}

func init() {
	webTopics["settings"] = webTopicDef{snapshot: webSettingsSnapshot, apply: webApplyReplace[webproto.SettingsSnap]}
	webAfterUpdates = append(webAfterUpdates, (*Model).webPublishSettings)
	webOps[webproto.OpSettingsSet] = (*Model).webSettingsSet
}

// webSettingsDelta is a settings delta: the whole list; a newer one
// replaces a queued one.
type webSettingsDelta webproto.SettingsSnap

func (webSettingsDelta) coalesce(next any) (any, bool) {
	n, ok := next.(webSettingsDelta)
	return n, ok
}

// webSettingsPub is what the settings topic sent last (Update only).
type webSettingsPub struct {
	last    *webproto.SettingsSnap
	stamp   webSettingsStamp
	checked time.Time
}

// webSettingsStamp says "the config files changed" cheaply.
type webSettingsStamp struct {
	size, aiSize int64
	mod, aiMod   time.Time
}

func webSettingsStampNow() webSettingsStamp {
	var s webSettingsStamp
	if p, err := settingsConfigPath(); err == nil {
		if fi, err := os.Stat(p); err == nil {
			s.size, s.mod = fi.Size(), fi.ModTime()
		}
	}
	if p, err := ai.ConfigPath(); err == nil {
		if fi, err := os.Stat(p); err == nil {
			s.aiSize, s.aiMod = fi.Size(), fi.ModTime()
		}
	}
	return s
}

func webSettingsSnapshot(m *Model, _ string) (any, error) {
	snap := webSettingsBuild()
	if m.web != nil {
		p := &m.web.more.settings
		p.last, p.stamp, p.checked = &snap, webSettingsStampNow(), webNow()
	}
	return snap, nil
}

// webPublishSettings sends the list again when the config files changed.
func (m *Model) webPublishSettings(tea.Msg) tea.Cmd {
	h := m.web
	p := &h.more.settings
	if !h.webListens("settings") {
		p.last = nil
		return nil
	}
	now := webNow()
	if p.last == nil || now.Sub(p.checked) < webSettingsEvery {
		return nil
	}
	p.checked = now
	stamp := webSettingsStampNow()
	if stamp == p.stamp {
		return nil
	}
	p.stamp = stamp
	cur := webSettingsBuild()
	if reflect.DeepEqual(*p.last, cur) {
		return nil
	}
	p.last = &cur
	h.publish("settings", webSettingsDelta(cur))
	return nil
}

// webSettingsBuild lists the allow-listed settings in Settings order.
func webSettingsBuild() webproto.SettingsSnap {
	cfg := loadSettings()
	s := webproto.SettingsSnap{Items: []webproto.SettingItem{}}
	for _, e := range SettingsCatalog() {
		if !webSettingsAllow[e.Key] || e.ID == "" || len(s.Items) == webproto.MaxSettings {
			continue
		}
		r, ok := findSettingsRow(e.ID)
		if !ok {
			continue
		}
		typ := e.Type
		switch typ {
		case "bool", "int", "number", "enum":
		default:
			typ = "string"
		}
		var choices []string
		if typ == "enum" {
			choices = e.Choices
		}
		s.Items = append(s.Items, webproto.SettingItem{Key: e.Key, Group: e.Category, Type: typ,
			Value: currentSettingValue(r, cfg), Default: e.Default, Description: e.Description, Choices: choices})
	}
	return s
}

// webSettingsSet is settings.set (the phone's Settings page): one
// allow-listed key, checked like the AI's settings_set (type, range,
// choices), then saved and applied by the same code as the laptop's
// Settings row and read back. The list goes out again at once.
func (m *Model) webSettingsSet(call *webCall) tea.Cmd {
	a := call.args.(*webproto.SettingsSetArgs)
	var e SettingEntry
	for _, x := range SettingsCatalog() {
		if x.Key == a.Key && x.ID != "" {
			e = x
		}
	}
	if !webSettingsAllow[a.Key] || e.ID == "" {
		call.fail(webproto.CodeForbidden, "That setting is changed on the laptop only.")
		return nil
	}
	row, value, err := checkSettingValue(e, strings.TrimSpace(a.Value))
	if err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	cmd := m.applySetting(row.ID, value)
	now := currentSettingValue(row, loadSettings())
	if now != value {
		call.fail(webproto.CodeInternal, "The laptop could not save "+a.Key+".")
		return cmd
	}
	cur := webSettingsBuild()
	if h := m.web; h != nil && h.webListens("settings") {
		p := &h.more.settings
		p.last, p.stamp, p.checked = &cur, webSettingsStampNow(), webNow()
		h.publish("settings", webSettingsDelta(cur))
	}
	for _, it := range cur.Items {
		if it.Key == a.Key {
			call.reply(it)
			return cmd
		}
	}
	call.reply(webproto.SettingItem{Key: a.Key, Group: e.Category, Type: e.Type, Value: now, Default: e.Default})
	return cmd
}
