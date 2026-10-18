package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/help"
	"github.com/amin-jalali/termocode/internal/keymap"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Spec: every focus topic, every Settings row and every palette command
// points at a real section (and row text) of the built-in guide.
func TestHelpContextTopicsResolve(t *testing.T) {
	var topics []string
	for name, topic := range helpContexts {
		if topic == "" {
			t.Errorf("%s: empty topic", name)
		}
		topics = append(topics, topic)
	}
	for _, r := range newSettingsModal().rows {
		if !r.Header {
			topics = append(topics, settingsTopic(r.Category, r.Row.Label))
		}
	}
	for _, e := range PaletteCatalog() {
		topics = append(topics, paletteTopic(e.Title))
	}
	for _, err := range help.CheckAll(topics) {
		t.Error(err)
	}
	if got := settingsTopic("Appearance", "theme"); got != "reference/settings.md#appearance?`theme`" {
		t.Errorf("settingsTopic = %q", got)
	}
	if got := paletteTopic("Git: Stage Hunk"); got != "reference/commands.md#git?Git: Stage Hunk" {
		t.Errorf("paletteTopic = %q", got)
	}
	if got := paletteTopic("User: deploy"); got != helpContexts["palette"] {
		t.Errorf("a user command = %q, want the run-time section", got)
	}
}

// Spec (plan §2.3): Shift+F1 opens the section for what has the focus;
// the first match wins, a shown hover label before anything else.
func TestHelpTopicForFocus(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	base := hoverModel(t)
	cases := []struct {
		name string
		set  func(m *Model)
		want string
	}{
		{"welcome page", func(*Model) {}, helpContexts["welcome"]},
		{"hover label", func(m *Model) {
			*m = showTipOn(t, *m, "activity:Explorer")
			m.settingsModalOpen = true
		}, activityHelp["Explorer"]},
		{"settings on theme", func(m *Model) {
			m.settingsModal = newSettingsModal()
			for i, r := range m.settingsModal.rows {
				if !r.Header && r.Row.Label == "theme" {
					m.settingsModal.cursor = i
				}
			}
			m.settingsModalOpen = true
		}, "reference/settings.md#appearance?`theme`"},
		{"palette on a command", func(m *Model) {
			m.picker = picker.NewItems("palette", []picker.Item{{ID: "git-stage-hunk", Title: "Git: Stage Hunk"}})
			m.pickerOpen, m.pickerKind = true, pickerKindPalette
		}, "reference/commands.md#git?Git: Stage Hunk (at cursor)"},
		{"keybinding picker", func(m *Model) {
			m.picker = picker.NewItems("keys", []picker.Item{{ID: "x", Title: "x"}})
			m.pickerOpen, m.pickerKind = true, pickerKindKeybinding
		}, helpContexts["keybindings"]},
		{"theme picker", func(m *Model) {
			m.picker = picker.NewItems("themes", []picker.Item{{ID: "x", Title: "x"}})
			m.pickerOpen, m.pickerKind = true, pickerKindTheme
		}, helpContexts["theme"]},
		{"review card", func(m *Model) { m.reviewOpen = true }, helpContexts["review"]},
		{"agent confirm", func(m *Model) { m.confirmOpen, m.confirmKind = true, confirmKindAITool }, helpContexts["agent-confirm"]},
		{"connect card", func(m *Model) { m.connectOpen = true }, helpContexts["connect"]},
		{"find bar", func(m *Model) { m.findOpen = true }, helpContexts["find"]},
		{"replace bar", func(m *Model) { m.replaceOpen = true }, helpContexts["replace"]},
		{"search overlay", func(m *Model) { m.searchOpen = true }, helpContexts["search"]},
		{"AI chat", func(m *Model) { m.ai = newAIState(); m.ai.focused = true }, helpContexts["chat"]},
		{"AGENTS", func(m *Model) { m.ai = newAIState(); m.ai.focused, m.ai.agents.on = true, true }, helpContexts["agents"]},
		{"explorer", func(m *Model) { m.showExp, m.focus = true, FocusExplorer }, activityHelp["Explorer"]},
		{"source control", func(m *Model) {
			m.showExp, m.focus = true, FocusExplorer
			m.activity.SetActive(activity.ViewGit)
		}, activityHelp["Source Control"]},
		{"services view", func(m *Model) {
			m.showExp, m.focus = true, FocusExplorer
			m.activity.SetActive(activity.ViewServices)
		}, activityHelp["Services"]},
		{"problems tab", func(m *Model) { m.panelFocused, m.panelActive = true, panelKindProblems }, helpContexts["problems"]},
		{"debug console", func(m *Model) { m.panelFocused, m.panelActive = true, panelKindDebugConsole }, helpContexts["debug-console"]},
		{"terminal", func(m *Model) { m.inTerminal = true }, helpContexts["terminal"]},
		{"side-by-side diff", func(m *Model) { m.gitDiffActive = true }, helpContexts["diff"]},
	}
	for _, c := range cases {
		m := base
		m.focus = FocusEditor
		c.set(&m)
		if got := m.helpTopicForFocus(); got != c.want {
			t.Errorf("%s: topic %q, want %q", c.name, got, c.want)
		}
	}
}

// Spec: Shift+F1 opens the guide at the focused item's section, also
// while an overlay (here the Settings modal) has the keys; Esc in the
// guide goes back to that overlay. The palette has "Help: Explain
// This…" with the same key.
func TestContextHelpKeyOpensGuide(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := hoverModel(t)
	m.settingsModal = newSettingsModal()
	for i, r := range m.settingsModal.rows {
		if !r.Header && r.Row.Label == "theme" {
			m.settingsModal.cursor = i
		}
	}
	m.settingsModalOpen = true
	shiftF1 := tea.KeyMsg{Type: tea.KeyF13}
	if m.keys.Match(shiftF1) != keymap.ActionContextHelp {
		t.Fatalf("f13 is not ContextHelp")
	}
	nm, cmd := m.Update(shiftF1)
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("Shift+F1 gave no command")
	}
	msg := findPreviewMsg(cmd)
	if msg == nil {
		t.Fatal("Shift+F1 did not load a guide page")
	}
	nm, _ = m.Update(*msg)
	m = nm.(Model)
	if !m.helpActive() || m.help.cur.Anchor != "appearance" || m.help.cur.Find != "`theme`" {
		t.Errorf("guide at %+v, active %v", m.help.cur, m.helpActive())
	}
	if !m.settingsModalOpen {
		t.Errorf("the Settings modal closed under the guide")
	}
	// The guide is drawn over the modal (it takes the keys too).
	if top := ansi.Strip(strings.SplitN(m.View(), "\n", 2)[0]); !strings.Contains(top, "Guide") || !strings.Contains(top, "Settings") {
		t.Errorf("guide not on screen over the modal: %q", top)
	}

	if a, ok := paletteActions["help-context"]; !ok || a != keymap.ActionContextHelp {
		t.Errorf("palette help-context runs %v", a)
	}
	found := false
	for _, it := range paletteItems() {
		if it.ID == "help-context" {
			found = strings.HasPrefix(it.Title, "Help: ")
		}
	}
	if !found {
		t.Errorf("no Help: palette command help-context")
	}
	if !strings.Contains(cheatSheetBody(keymap.Default()), "Shift+F1") {
		t.Errorf("cheat sheet TIPS do not mention Shift+F1")
	}
}

// findPreviewMsg runs cmd (and batches) until a PreviewMsg comes out. A
// command that takes longer than a second (a tick) is skipped.
func findPreviewMsg(cmd tea.Cmd) *PreviewMsg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var out tea.Msg
	select {
	case out = <-ch:
	case <-time.After(time.Second):
		return nil
	}
	switch msg := out.(type) {
	case PreviewMsg:
		return &msg
	case tea.BatchMsg:
		for _, c := range msg {
			if p := findPreviewMsg(c); p != nil {
				return p
			}
		}
	}
	return nil
}
