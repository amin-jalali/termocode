package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/keyhint"
)

// Spec: key_hints is on until the user turns it off.
func TestKeyHintsDefaultOn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if !keyHintsEnabled(loadSettings()) {
		t.Fatal("key_hints must default to on")
	}
}

// Spec: "Help: Toggle Key Hints" flips the setting, saves it and says so.
func TestToggleKeyHintsPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := New()
	m.toggleKeyHints()
	if keyHintsEnabled(loadSettings()) {
		t.Error("first toggle must turn it off")
	}
	m.toggleKeyHints()
	if !keyHintsEnabled(loadSettings()) {
		t.Error("second toggle must turn it on")
	}
}

// Spec: the palette lists it, and the Settings row reads the saved value.
func TestKeyHintsPaletteAndRow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	found := false
	for _, it := range paletteItems() {
		if it.ID == "toggle-key-hints" && strings.HasPrefix(it.Title, "Help:") {
			found = true
		}
	}
	if !found {
		t.Error("palette has no Help: Toggle Key Hints")
	}
	r, ok := findSettingsRow("setting-key-hints")
	if !ok || r.Label != "key_hints" || r.Default != "true" {
		t.Fatalf("row = %+v ok=%v", r, ok)
	}
	m := New()
	m.toggleKeyHints()
	if got := currentSettingValue(r, loadSettings()); got != "false" {
		t.Errorf("row value after toggle = %q", got)
	}
}

// fakeHints is the editor side of the popup: a leader, mappings, and a
// record of the keys sent back.
type fakeHints struct {
	maps []keyhint.Mapping
	sent []string
}

func (f *fakeHints) Leader() string          { return "<space>" }
func (f *fakeHints) Timeout() time.Duration  { return 0 }
func (f *fakeHints) Maps() []keyhint.Mapping { return f.maps }
func (f *fakeHints) Send(keys string)        { f.sent = append(f.sent, keys) }

func hintModel(t *testing.T) (Model, *fakeHints, *time.Time) {
	t.Helper()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	f := &fakeHints{maps: []keyhint.Mapping{
		{LHS: "<Space>f", Desc: "Format the buffer"},
		{LHS: "<Space>gs", Desc: "Stage hunk"},
		{LHS: "<Space>gp", Desc: "Push"},
	}}
	m := New()
	m.w, m.h = 80, 24
	m.focus = FocusEditor
	m.hintSrc = f
	return m, f, &now
}

func hintPress(m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	nm, cmd := m.handleKeyWithHints(k)
	return nm, cmd
}

func hintSpace() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeySpace} }
func hintRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func hintScreen(m Model) string {
	rows := make([]string, 24)
	for i := range rows {
		rows[i] = strings.Repeat(" ", 80)
	}
	return ansi.Strip(m.overlayKeyHints(strings.Join(rows, "\n")))
}

// Spec: leader, then a short rest: the popup lists the next keys of the
// editor's current mappings; it is not there before the delay.
func TestKeyHintsPopupAfterRest(t *testing.T) {
	m, _, now := hintModel(t)
	m, _ = hintPress(m, hintSpace())
	*now = now.Add(keyhint.Delay / 2)
	m.onKeyHintTick(keyHintTickMsg{m.hints.Gen()})
	if strings.Contains(hintScreen(m), "Format the buffer") {
		t.Fatal("popup shown before the delay")
	}
	*now = now.Add(keyhint.Delay)
	m.onKeyHintTick(keyHintTickMsg{m.hints.Gen()})
	got := hintScreen(m)
	for _, want := range []string{"Space", "Format the buffer", "+2 more"} {
		if !strings.Contains(got, want) {
			t.Errorf("popup lacks %q:\n%s", want, got)
		}
	}
}

// Spec: a user who types the next key at once never sees the popup.
func TestKeyHintsFastTypistSeesNothing(t *testing.T) {
	m, _, now := hintModel(t)
	m, _ = hintPress(m, hintSpace())
	gen := m.hints.Gen()
	m, _ = hintPress(m, hintRune('f'))
	*now = now.Add(2 * keyhint.Delay)
	m.onKeyHintTick(keyHintTickMsg{gen})
	if strings.Contains(hintScreen(m), "Format") {
		t.Error("popup shown for a fast sequence")
	}
}

// Spec: the popup shows what is bound now, not a fixed list.
func TestKeyHintsFollowLiveMappings(t *testing.T) {
	m, f, now := hintModel(t)
	f.maps = []keyhint.Mapping{{LHS: "<Space>z", Desc: "Brand new binding"}}
	m, _ = hintPress(m, hintSpace())
	*now = now.Add(keyhint.Delay)
	m.onKeyHintTick(keyHintTickMsg{m.hints.Gen()})
	got := hintScreen(m)
	if !strings.Contains(got, "Brand new binding") || strings.Contains(got, "Format") {
		t.Errorf("popup does not follow the live mappings:\n%s", got)
	}
}

// Spec: key_hints off means no tracking, and turning it off closes an open popup.
func TestKeyHintsOff(t *testing.T) {
	m, _, now := hintModel(t)
	m, _ = hintPress(m, hintSpace())
	*now = now.Add(keyhint.Delay)
	m.onKeyHintTick(keyHintTickMsg{m.hints.Gen()})
	m.setKeyHints(false)
	if strings.Contains(hintScreen(m), "Format") {
		t.Error("popup stayed after turning key_hints off")
	}
	m, cmd := hintPress(m, hintSpace())
	if cmd != nil || m.hints.Active() {
		t.Error("a sequence started with key_hints off")
	}
}

// Spec: keys outside the editor (explorer focus, insert mode) never start a sequence.
func TestKeyHintsOnlyInEditorNormalMode(t *testing.T) {
	m, _, _ := hintModel(t)
	m.focus = FocusExplorer
	if _, cmd := hintPress(m, hintSpace()); cmd != nil {
		t.Error("explorer focus started a sequence")
	}
	m.focus, m.editorMode = FocusEditor, "insert"
	if _, cmd := hintPress(m, hintSpace()); cmd != nil {
		t.Error("insert mode started a sequence")
	}
}

// Spec: Backspace in a deeper level goes up one level and sends the
// shorter prefix to the editor; Esc closes.
func TestKeyHintsBackspaceUpAndEsc(t *testing.T) {
	m, f, now := hintModel(t)
	m, _ = hintPress(m, hintSpace())
	*now = now.Add(keyhint.Delay)
	m.onKeyHintTick(keyHintTickMsg{m.hints.Gen()})
	m, _ = hintPress(m, hintRune('g'))
	m, _ = hintPress(m, tea.KeyMsg{Type: tea.KeyBackspace})
	if len(f.sent) != 1 || f.sent[0] != "<Esc><space>" {
		t.Errorf("sent = %q, want the prefix again after Esc", f.sent)
	}
	if !strings.Contains(hintScreen(m), "Format the buffer") {
		t.Error("popup is not back at level one")
	}
	m, _ = hintPress(m, tea.KeyMsg{Type: tea.KeyEsc})
	if strings.Contains(hintScreen(m), "Format") {
		t.Error("Esc did not close the popup")
	}
}

// Spec: a key that runs a termocode action ends the sequence.
func TestKeyHintsActionKeyEndsSequence(t *testing.T) {
	m, _, now := hintModel(t)
	m, _ = hintPress(m, hintSpace())
	*now = now.Add(keyhint.Delay)
	m.onKeyHintTick(keyHintTickMsg{m.hints.Gen()})
	m.keyHintKey(tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.hints.Active() || strings.Contains(hintScreen(m), "Format") {
		t.Error("an action key left the popup open")
	}
}

// Spec: against a real editor, the source reads the leader, and the
// leader mappings with their desc (a blank in the text is the Space key).
func TestNvimHintSourceReadsLeaderMappings(t *testing.T) {
	m := aiTestModel(t)
	c := aiTestNvim(t, &m)
	if err := c.ExecLua(`vim.g.mapleader = ' '
vim.keymap.set('n', '<leader>q', function() end, { desc = 'Quick thing' })
vim.keymap.set('n', '<leader>wv', '<Cmd>vsplit<CR>')`); err != nil {
		t.Fatal(err)
	}
	src := nvimHintSource{c}
	if got := src.Leader(); got != "<space>" {
		t.Errorf("Leader = %q", got)
	}
	got := keyhint.Entries(src.Maps(), []string{"<space>"})
	var q, w bool
	for _, e := range got {
		q = q || (e.Key == "q" && e.Desc == "Quick thing")
		w = w || (e.Key == "w" && e.Group)
	}
	if !q || !w {
		t.Errorf("entries = %+v", got)
	}
	if src.Timeout() <= 0 {
		t.Errorf("Timeout = %v, want nvim's timeoutlen", src.Timeout())
	}
}

// Spec: the developer screen "keyhintdemo" shows the leader level, one
// level deeper, and the "no binding" hint.
func TestKeyHintsDemoScreens(t *testing.T) {
	for arg, want := range map[string]string{"": "Format the buffer", "group": "Search in files", "notice": "no binding: q"} {
		m := New()
		m.w, m.h = 80, 24
		m.keyHintsDemo(arg)
		if got := hintScreen(m); !strings.Contains(got, want) {
			t.Errorf("keyhintdemo %q lacks %q:\n%s", arg, want, got)
		}
	}
}
