package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// K2: every setting the AI may change asks first with class settings;
// the others are never, with a reason. No setting is "planned" any more.
func TestAISettingPolicyExposedOrNever(t *testing.T) {
	for _, s := range SettingsCatalog() {
		p := aiSettingPolicies[s.Key]
		switch p.Gate {
		case aiGateConfirm:
			if p.Class != ai.ClassSettings {
				t.Errorf("%s: class %q, want %q", s.Key, p.Class, ai.ClassSettings)
			}
		case aiGateNever:
		default:
			t.Errorf("%s: gate %q, want confirm or never", s.Key, p.Gate)
		}
		if strings.Contains(p.Reason, "planned") {
			t.Errorf("%s: still planned: %q", s.Key, p.Reason)
		}
	}
	for _, k := range []string{"theme", "tab_size", "icons", "word_wrap", "auto_save", "dashboard_logo"} {
		if aiSettingPolicies[k].Gate != aiGateConfirm {
			t.Errorf("%s must be exposed", k)
		}
	}
	for _, k := range []string{"ai_provider", "ai_model", "ai_base_url", "ai_inline_model", "ai_auto_approve", "ai_max_steps"} {
		if aiSettingPolicies[k].Gate != aiGateNever {
			t.Errorf("%s must be never", k)
		}
	}
}

func TestAIValidateSetting(t *testing.T) {
	withConfigDir(t)
	ok := []struct{ key, in, want string }{
		{"theme", "tokyo-night", "tokyo-night"},
		{"tab_size", "2", "2"},
		{"word_wrap", "yes", "true"},
		{"icons", "ascii", "ascii"},
		{"search_max_results", "0", "0"},
	}
	for _, c := range ok {
		_, got, err := aiValidateSetting(c.key, c.in)
		if err != nil || got != c.want {
			t.Errorf("%s=%s: got %q err %v, want %q", c.key, c.in, got, err, c.want)
		}
	}
	bad := []struct{ key, in, wantErr string }{
		{"theme", "no-such-theme", "tokyo-night"},        // lists the themes
		{"tab_size", "0", "1..16"},                       // range
		{"tab_size", "four", "integer"},                  // kind
		{"word_wrap", "maybe", "true or false"},          // kind
		{"icons", "emoji", "auto, nerd, unicode, ascii"}, // choices
		{"ai_provider", "openai", "not for the AI"},      // never
		{"ai_auto_approve", "run", "not for the AI"},     // never
		{"api_key", "sk-1", "not for the AI"},            // secret-like
		{"no_such", "1", "theme"},                        // unknown: lists keys
	}
	for _, c := range bad {
		_, _, err := aiValidateSetting(c.key, c.in)
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("%s=%s: err %v, want it to contain %q", c.key, c.in, err, c.wantErr)
		}
	}
}

func TestAISettingsListJSON(t *testing.T) {
	withConfigDir(t)
	var rows []struct {
		Key, Type, Value, Description string
		Choices                       []string
	}
	if err := json.Unmarshal([]byte(aiSettingsList()), &rows); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for i, r := range rows {
		got[r.Key] = i
	}
	for _, never := range []string{"ai_provider", "ai_auto_approve", "ai_base_url", "terminal_skip_shell"} {
		if _, ok := got[never]; ok {
			t.Errorf("never setting %s listed", never)
		}
	}
	th, ok := got["theme"]
	if !ok {
		t.Fatalf("no theme row: %+v", rows)
	}
	if r := rows[th]; r.Value != "vscode-dark-plus" || !contains(r.Choices, "tokyo-night") || r.Description == "" {
		t.Errorf("theme row %+v", r)
	}
	if r := rows[got["tab_size"]]; r.Type != "int" || r.Value != "4" {
		t.Errorf("tab_size row %+v", r)
	}
	if r := rows[got["icons"]]; !contains(r.Choices, "nerd") {
		t.Errorf("icons row %+v", r)
	}
}

// settings_set asks with class settings; a never key is refused without
// asking.
func TestAISettingsSetGate(t *testing.T) {
	run := func(key, value string) (asked []string, results []string) {
		t.Helper()
		p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("settings_set", map[string]any{"key": key, "value": value})}}
		m, _ := aiIDETestModel(t, p)
		driveAIChat(t, m, m.sendAIChat("set"), func(c aiChatConfirmMsg) string {
			asked = append(asked, c.tool.Class)
			return "deny"
		})
		return asked, p.toolResults()
	}
	if asked, r := run("ai_auto_approve", "run"); len(asked) != 0 || len(r) != 1 || !strings.Contains(r[0], "not for the AI") {
		t.Fatalf("never: asked=%v results %v", asked, r)
	}
	if asked, r := run("tab_size", "99"); len(asked) != 0 || len(r) != 1 || !strings.Contains(r[0], "1..16") {
		t.Fatalf("bad value: asked=%v results %v", asked, r)
	}
	if asked, _ := run("search_max_results", "10"); len(asked) != 1 || asked[0] != ai.ClassSettings {
		t.Fatalf("exposed: asked=%v", asked)
	}
}

// Allowed, settings_set saves like the Settings UI does.
func TestAISettingsSetSaves(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("settings_set", map[string]any{"key": "search_max_results", "value": 7})}}
	m, _ := aiIDETestModel(t, p)
	driveAIChat(t, m, m.sendAIChat("set"), func(aiChatConfirmMsg) string { return "allow" })
	if r := p.toolResults(); len(r) != 1 || !strings.Contains(r[0], "search_max_results = 7") {
		t.Fatalf("results %v", r)
	}
	if c := loadSettings(); c.SearchMaxResults == nil || *c.SearchMaxResults != 7 {
		t.Errorf("not saved: %+v", c.SearchMaxResults)
	}
}

// layout_set runs without asking, clamps sizes like a mouse drag, saves
// the sidebar width, and refuses to hide the AI panel.
func TestAILayoutSet(t *testing.T) {
	run := func(args map[string]any) (*Model, []string, int) {
		t.Helper()
		p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("layout_set", args)}}
		m, _ := aiIDETestModel(t, p)
		m.showExp = true
		asked := 0
		driveAIChat(t, m, m.sendAIChat("layout"), func(aiChatConfirmMsg) string { asked++; return "deny" })
		return m, p.toolResults(), asked
	}
	m, r, asked := run(map[string]any{"sidebar_width": 500})
	if asked != 0 || m.explorerWidth != explorerMaxWidth || len(r) != 1 || !strings.Contains(r[0], "clamped") {
		t.Fatalf("asked=%d width=%d results %v", asked, m.explorerWidth, r)
	}
	if s := loadSession(); s.ExplorerWidth != explorerMaxWidth {
		t.Errorf("width not saved: %d", s.ExplorerWidth)
	}
	m, _, _ = run(map[string]any{"sidebar": false})
	if m.showExp {
		t.Error("sidebar still shown")
	}
	m, r, _ = run(map[string]any{"ai_panel": false})
	if len(r) != 1 || !strings.Contains(r[0], "AI panel") || !strings.Contains(r[0], "Alt+A") {
		t.Errorf("ai_panel=false: %v", r)
	}
	_, r, _ = run(map[string]any{})
	if len(r) != 1 || !strings.Contains(r[0], "nothing to change") {
		t.Errorf("empty: %v", r)
	}
}

// get_ide_state reports the IDE as JSON without asking.
func TestAIGetIDEState(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("get_ide_state", nil)}}
	m, _ := aiIDETestModel(t, p)
	m.showExp, m.explorerWidth, m.themeID = true, 33, "tokyo-night"
	asked := 0
	driveAIChat(t, m, m.sendAIChat("state"), func(aiChatConfirmMsg) string { asked++; return "deny" })
	r := p.toolResults()
	if asked != 0 || len(r) != 1 {
		t.Fatalf("asked=%d results %v", asked, r)
	}
	var st map[string]any
	if err := json.Unmarshal([]byte(r[0]), &st); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, r[0])
	}
	for _, k := range []string{"focus", "sidebar", "panel", "terminal_tabs", "ai_panel", "zen", "theme", "icons",
		"git", "lsp_clients", "problems", "debug", "tests_running", "tasks_running"} {
		if _, ok := st[k]; !ok {
			t.Errorf("no %q in %s", k, r[0])
		}
	}
	if st["theme"] != "tokyo-night" {
		t.Errorf("theme %v", st["theme"])
	}
	if sb, _ := st["sidebar"].(map[string]any); sb["width"] != float64(33) || sb["view"] != "explorer" {
		t.Errorf("sidebar %v", st["sidebar"])
	}
}
