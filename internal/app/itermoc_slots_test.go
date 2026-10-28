package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/theme"
)

// imSlotCfg is a config.json with the user's own keys and one slot that
// has a field termocode does not know (canonical: sorted, two spaces).
const imSlotCfg = `{
  "ai_models": {
    "gpt": {
      "model": "gpt-5-mini",
      "note": "mine",
      "provider": "openai"
    }
  },
  "editor_theme": {
    "name": "dark",
    "size": 3
  },
  "zz_last": [
    1,
    2
  ]
}
`

// slotTestModel is the Itermoc test model with imSlotCfg on disk (its own
// config dir) and the settings read from it.
func slotTestModel(t *testing.T, w, h int) (*Model, string) {
	t.Helper()
	imIcons(t, theme.IconModeUnicode)
	m := itermocTestModel(t, 70, 40)
	m.w, m.h = w, h
	path, err := ai.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(imSlotCfg), 0o644); err != nil {
		t.Fatal(err)
	}
	m.ai.reload()
	m.itermocS().refresh(*m, true)
	return m, path
}

// slotSend sends one key (or click) to the open card and runs the message
// it gives back, the way the app's update loop does.
func slotSend(m *Model, msg tea.Msg) {
	nm, cmd := m.routeToConnect(msg)
	*m = nm
	if cmd == nil {
		return
	}
	if out := cmd(); out != nil {
		m.handleMCPConnectMsg(out)
	}
}

func slotType(m *Model, s string) {
	slotSend(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}

func slotKey(m *Model, k tea.KeyType) { slotSend(m, tea.KeyMsg{Type: k}) }

// slotCard is the open card's plain text.
func slotCard(m *Model) string { return ansi.Strip(m.connect.Box()) }

// rawKeys decodes a config file into compacted top-level values.
func rawKeys(t *testing.T, path string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for k, v := range raw {
		var c bytes.Buffer
		_ = json.Compact(&c, v)
		out[k] = c.String()
	}
	return out
}

// sameExceptModels fails when a key other than ai_models changed.
func sameExceptModels(t *testing.T, before, after map[string]string) {
	t.Helper()
	for k, v := range before {
		if k != "ai_models" && after[k] != v {
			t.Errorf("key %s changed: %s -> %s", k, v, after[k])
		}
	}
	if len(after) != len(before) && !(len(after) == len(before)+1 && after["ai_models"] != "" && before["ai_models"] == "") {
		t.Errorf("keys %v -> %v", before, after)
	}
}

// Spec: AI: Add Model Slot… (the expert path; a is Add AI now) opens the
// form; Save writes ai_models.<name> and
// nothing else; the new row shows key ✗; the card then offers Set key,
// which stores the key through the secret store. The key is never in the
// frame, config.json or models_list.
func TestItermocSlotAddThenSetKey(t *testing.T) {
	m, path := slotTestModel(t, 200, 50)
	before := rawKeys(t, path)
	m.dispatchItermocPalette("ai-model-slot-add")
	if !m.connectOpen || m.itermoc.form == nil {
		t.Fatal("the palette did not open the slot form")
	}
	f := slotCard(m)
	for _, want := range []string{"Add model slot", "Name", "Provider", "anthropic", "openai-compatible", "claude-oauth", "Model", "Save", "Cancel"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in the form\n%s", want, f)
		}
	}
	slotType(m, "work")
	slotKey(m, tea.KeyTab)   // provider
	slotKey(m, tea.KeyRight) // openai
	slotKey(m, tea.KeyTab)   // model
	if !strings.Contains(slotCard(m), "gpt-4.1-mini") {
		t.Errorf("no openai model suggestions\n%s", slotCard(m))
	}
	slotType(m, "gpt-5")
	slotKey(m, tea.KeyEnter)

	sameExceptModels(t, before, rawKeys(t, path))
	s := ai.LoadSettings(path)
	if sl := s.Models["work"]; sl.Provider != ai.KindOpenAI || sl.Model != "gpt-5" || sl.BaseURL != "" {
		t.Errorf("work = %+v", sl)
	}
	if s.Models["gpt"].Model != "gpt-5-mini" {
		t.Error("the other slot changed")
	}
	if _, errs := ai.ResolveSlots(s, ai.Config{}, ai.Credentials{}, func(string) string { return "" }); len(errs) > 0 {
		t.Errorf("ResolveSlots: %v", errs)
	}
	// The row is there with key ✗, and the card offers Set key.
	if sl, ok := m.itermoc.snap.slot("work"); !ok || sl.key != imKeyMissing || !sl.user {
		t.Errorf("row %+v %v", sl, ok)
	}
	if r, _ := m.itermoc.current(); r.kind != imRowSlot || r.name != "work" {
		t.Errorf("cursor on %+v", r)
	}
	card := slotCard(m)
	if !m.connectOpen || !strings.Contains(card, "Set key") || !strings.Contains(card, "OpenAI key") {
		t.Fatalf("no Set key offer\n%s", card)
	}
	slotKey(m, tea.KeyEnter) // Set key is the primary button
	if m.connectOpen || !m.promptOpen || m.promptKind != promptKindAIKey || m.ai.cfgKind != ai.KindOpenAI || !m.ai.keyOnly {
		t.Fatalf("no masked key prompt: connect=%v prompt=%v kind=%v cfgKind=%q", m.connectOpen, m.promptOpen, m.promptKind, m.ai.cfgKind)
	}
	m.promptOpen = false
	m.onAIKeyEntered(imPlantedKey)
	creds, _ := ai.LoadCredentials(mustCredPath(t))
	if creds.Keys[ai.KindOpenAI] != imPlantedKey {
		t.Error("the key was not stored in the secret store")
	}
	if sl, _ := m.itermoc.snap.slot("work"); sl.key != imKeyOK {
		t.Errorf("work key state %v after Set key", sl.key)
	}
	cfg, _ := os.ReadFile(path)
	var call aiIDECaller = func(_ context.Context, fn aiIDEFunc) (string, error) { out, _, err := fn(m); return out, err }
	tools := aiItermocRun(t, m, call, "models_list", nil)
	for name, text := range map[string]string{"frame": imFrame(m), "config.json": string(cfg), "models_list": tools} {
		if strings.Contains(text, imPlantedKey) || strings.Contains(text, "PLANTED") {
			t.Errorf("%s shows the key", name)
		}
	}
	if !strings.Contains(tools, `"work"`) {
		t.Errorf("models_list misses the new slot: %s", tools)
	}
}

func mustCredPath(t *testing.T) string {
	t.Helper()
	p, err := ai.CredentialsPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Spec: a bad name, a missing or bad base URL, or a name in use stay in
// the form as an inline error; nothing is written. A base URL's password
// is never repeated.
func TestItermocSlotAddInvalid(t *testing.T) {
	cases := []struct {
		name, url, want string
		right           int // provider index
	}{
		{name: "bad name", want: "slot name"},
		{name: "main", want: "built in"},
		{name: "", want: "Type a name"},
		{name: "gpt", right: 1, want: "exists"},
		{name: "proxy", right: 5, want: "needs base_url"},
		{name: "proxy", right: 5, url: "llm.local:8000", want: "http"},
		{name: "proxy", right: 5, url: "https://me:s3cr3tpass@llm.local/v1", want: "user or password"},
	}
	for _, c := range cases {
		m, path := slotTestModel(t, 200, 50)
		old, _ := os.ReadFile(path)
		m.openItermocSlotAdd()
		slotType(m, c.name)
		slotKey(m, tea.KeyTab)
		for i := 0; i < c.right; i++ {
			slotKey(m, tea.KeyRight)
		}
		if c.url != "" {
			m.connect.SetInputValue("url", c.url)
		}
		slotKey(m, tea.KeyEnter)
		card := slotCard(m)
		flat := strings.Join(strings.Fields(strings.ReplaceAll(card, "│", " ")), " ")
		if !m.connectOpen || !strings.Contains(flat, c.want) {
			t.Errorf("%q: no inline %q\n%s", c.name, c.want, card)
		}
		if strings.Contains(card, "s3cr3tpass") && !strings.Contains(card, "•") && strings.Count(card, "s3cr3tpass") > 1 {
			t.Errorf("%q: the error repeats the password", c.name)
		}
		if now, _ := os.ReadFile(path); !bytes.Equal(old, now) {
			t.Errorf("%q: config.json changed", c.name)
		}
	}
}

// Spec: Cancel and Esc close the form; config.json is byte-equal.
func TestItermocSlotAddCancel(t *testing.T) {
	for _, how := range []string{"cancel", "esc"} {
		m, path := slotTestModel(t, 200, 50)
		old, _ := os.ReadFile(path)
		m.openItermocSlotAdd()
		slotType(m, "work")
		if how == "esc" {
			slotKey(m, tea.KeyEsc)
		} else {
			m.handleMCPConnectMsg(connectcardButton("cancel"))
		}
		if m.connectOpen || m.itermoc.form != nil {
			t.Errorf("%s: the form stays open", how)
		}
		if now, _ := os.ReadFile(path); !bytes.Equal(old, now) {
			t.Errorf("%s: config.json changed", how)
		}
	}
}

// Spec: Edit… opens the same form with the slot's values; Save keeps the
// slot's unknown fields. main goes to Configure Provider; quick without an
// entry gets an override form.
func TestItermocSlotEdit(t *testing.T) {
	m, path := slotTestModel(t, 200, 50)
	imCursorTo(t, m, imRowSlot, "gpt")
	imPress(m, "e")
	if !m.connectOpen || m.itermoc.form == nil || !m.itermoc.form.edit {
		t.Fatal("e did not open the edit form")
	}
	if card := slotCard(m); !strings.Contains(card, "Edit model slot gpt") || m.connect.InputValue("model") != "gpt-5-mini" || m.connect.Choice("provider") != 1 {
		t.Fatalf("not prefilled: model %q provider %d\n%s", m.connect.InputValue("model"), m.connect.Choice("provider"), card)
	}
	m.connect.SetInputValue("model", "gpt-5")
	m.handleMCPConnectMsg(connectcardButton("save"))
	var cfg struct {
		Models map[string]map[string]any `json:"ai_models"`
	}
	b, _ := os.ReadFile(path)
	_ = json.Unmarshal(b, &cfg)
	if g := cfg.Models["gpt"]; g["note"] != "mine" || g["model"] != "gpt-5" || g["provider"] != "openai" {
		t.Errorf("gpt = %v", g)
	}
	m.closeItermocSlotForm()

	imVerb(m, imRowSlot, ai.SlotMain, "edit")
	if m.connectOpen || !m.pickerOpen || m.pickerKind != pickerKindAIProvider {
		t.Errorf("Edit… on main: connect=%v picker=%v kind=%v", m.connectOpen, m.pickerOpen, m.pickerKind)
	}
	m.pickerOpen = false
	imVerb(m, imRowSlot, ai.SlotQuick, "edit")
	if !m.connectOpen || !strings.Contains(slotCard(m), "Override model slot quick") {
		t.Errorf("no override form for quick\n%s", slotCard(m))
	}
}

// Spec: Remove… asks with Keep first and focused, names the profiles that
// use the slot; Keep (and Esc) writes nothing; Remove deletes only that
// slot. Built-in slots without an entry have no Remove….
func TestItermocSlotRemove(t *testing.T) {
	m, path := slotTestModel(t, 200, 50)
	m.ai.profFake = []ai.Profile{{Name: "second-opinion", Title: "GPT checks", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{{ID: "a", Title: "Answer"}, {ID: "b", Title: "Check", Slot: "gpt"}}}}
	m.ai.reloadProfiles()
	m.itermocRefreshNow()
	before := rawKeys(t, path)
	old, _ := os.ReadFile(path)
	imCursorTo(t, m, imRowSlot, "gpt")
	imPress(m, "D")
	if !m.confirmOpen || m.confirmKind != confirmKindAISlotRemove {
		t.Fatal("D did not open the remove card")
	}
	if bs := m.confirm.Buttons(); len(bs) < 2 || bs[0].ID != "keep" || m.confirm.SafeID() != "keep" {
		t.Errorf("buttons %+v", bs)
	}
	text := strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(m.confirm.Box()), "│", " ")), " ")
	for _, want := range []string{"Remove model slot gpt?", "second-opinion", "preflight"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in the card: %s", want, text)
		}
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc
	if now, _ := os.ReadFile(path); !bytes.Equal(old, now) {
		t.Error("Esc changed config.json")
	}
	imPress(m, "D")
	m.confirmOpen = false
	m.handleAIConfirm("remove")
	if _, ok := ai.LoadSettings(path).Models["gpt"]; ok {
		t.Error("gpt stays")
	}
	after := rawKeys(t, path)
	for k, v := range before {
		if k != "ai_models" && after[k] != v {
			t.Errorf("key %s changed", k)
		}
	}
	if _, ok := m.itermoc.snap.slot("gpt"); ok {
		t.Error("the row stays")
	}
	_, acts := m.itermocActionsFor(imRow{kind: imRowSlot, sec: imSecModels, name: ai.SlotMain})
	for _, a := range acts {
		if a.id == "remove" {
			t.Error("Remove… offered for main")
		}
	}
}

// Spec: the form and the cards fit 80x24 (the widest form: openai-
// compatible with a base URL and an error).
func TestItermocSlotForm80x24(t *testing.T) {
	m, _ := slotTestModel(t, 80, 24)
	m.openItermocSlotAdd()
	slotType(m, "bad name")
	slotKey(m, tea.KeyTab)
	for i := 0; i < 5; i++ {
		slotKey(m, tea.KeyRight)
	}
	slotKey(m, tea.KeyEnter)
	box := m.connect.Box()
	lines := strings.Split(box, "\n")
	if !strings.Contains(ansi.Strip(box), "Base URL") {
		t.Fatalf("no base URL row\n%s", ansi.Strip(box))
	}
	if len(lines) > 24 {
		t.Errorf("%d rows at 80x24\n%s", len(lines), ansi.Strip(box))
	}
	for i, l := range lines {
		if ansi.StringWidth(l) > 80 {
			t.Errorf("line %d is %d wide", i, ansi.StringWidth(l))
		}
	}
	if _, y := m.connect.Origin(); y+len(lines) > 24 {
		t.Errorf("the card ends at row %d", y+len(lines))
	}
}

// Spec: a click on the "+ Add AI…" row opens the Add AI card; the slot
// form stays in the palette (AI: Add Model Slot…); the key card lists a,
// e, k, D, E.
func TestItermocSlotMouseAndPalette(t *testing.T) {
	m, _ := slotTestModel(t, 200, 50)
	var hit *hitTarget
	for _, h := range m.itermocHits(m.explorerWidth - 1) {
		if h.ID == "im:row:"+imSecModels+"/+add" {
			h := h
			hit = &h
		}
	}
	if hit == nil {
		t.Fatal("no hit target for the add row")
	}
	nm, _ := m.handleItermocSidebarMouse(hit.X, hit.Y, tea.MouseLeft)
	*m = nm
	if !m.addAIOpen {
		t.Fatal("the click did not open the Add AI card")
	}
	m.closeAddAI()
	if _, ok := m.dispatchItermocPalette("ai-model-slot-add"); !ok || !m.connectOpen || m.itermoc.form == nil {
		t.Error("the palette command did not open the form")
	}
	if name, key, _, ok := itermocTooltip("im:row:" + imSecModels + "/+add"); !ok || name != imText("Add AI…") || key != "Enter" {
		t.Errorf("tooltip %q %q", name, key)
	}
	var keys []string
	for _, r := range itermocKeyRows() {
		keys = append(keys, r.Key)
	}
	all := strings.Join(keys, " ")
	for _, k := range []string{"a", "e", "D", "E"} {
		if !strings.Contains(" "+all+" ", " "+k+" ") {
			t.Errorf("the key card misses %q: %s", k, all)
		}
	}
}

func connectcardButton(id string) tea.Msg { return connectcard.ButtonMsg{ID: id} }
