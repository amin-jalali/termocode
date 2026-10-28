package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/profileform"
	"github.com/amin-jalali/termocode/internal/theme"
)

// provKey and provOtherKey are fake keys, split so no scanner takes them
// for real ones.
var (
	provKey      = "sk-" + "ant-test-" + "prov0000aaaa"
	provOtherKey = "sk-" + "test-" + "openai1111bbbb"
)

// provTestModel is Itermoc with two connected AIs on disk: Anthropic by
// an API key (stored in the test secret store) and Ollama on this
// machine, their model lists in the cache (Anthropic's fetched 2 min
// ago), and an OpenAI key that is stored but not connected.
func provTestModel(t *testing.T) *Model {
	t.Helper()
	imIcons(t, theme.IconModeUnicode)
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	t.Cleanup(clock.Set(func() time.Time { return now }))
	m := itermocTestModel(t, 40, 50)
	if err := ai.SaveCredentials(mustCredPath(t), ai.Credentials{Keys: map[string]string{
		ai.KindAnthropic: provKey, ai.KindOpenAI: provOtherKey}}); err != nil {
		t.Fatal(err)
	}
	path := mustProvidersPath(t)
	if err := ai.SaveProviders(path, map[string]ai.ProviderEntry{
		ai.KindAnthropic: {Kind: ai.KindAnthropic, Method: ai.MethodKey},
		ai.KindOllama:    {Kind: ai.KindOllama, Method: ai.MethodLocal},
	}); err != nil {
		t.Fatal(err)
	}
	cache := ai.ModelsCache{}
	cache.Put(ai.KindAnthropic, []ai.ModelInfo{{ID: "claude-fable-5-1"}, {ID: "claude-haiku-5-5"}, {ID: "claude-opus-5-5"}, {ID: "claude-sonnet-5-5"}},
		now.Add(-2*time.Minute))
	cache.Put(ai.KindOllama, []ai.ModelInfo{{ID: "llama3.2:3b"}, {ID: "qwen3:14b"}}, now.Add(-3*time.Hour))
	if err := ai.SaveModelsCache(cache); err != nil {
		t.Fatal(err)
	}
	m.ai.reload()
	m.ai.slotFake = map[string]aiSlot{ai.SlotMain: {cfg: ai.Config{Kind: ai.KindOllama, Model: "fake-1"}}}
	m.itermocS().refresh(*m, true)
	return m
}

// provFake counts model list calls and answers with fixed models.
type provFake struct {
	calls atomic.Int32
	keys  []string
	fail  bool
}

func (f *provFake) install(t *testing.T) {
	t.Helper()
	old := aiListModels
	aiListModels = func(_ context.Context, cfg ai.Config) ([]ai.ModelInfo, error) {
		f.calls.Add(1)
		f.keys = append(f.keys, cfg.APIKey)
		if f.fail {
			return nil, errors.New("401 Unauthorized: invalid x-api-key " + cfg.APIKey)
		}
		return []ai.ModelInfo{{ID: "claude-opus-5-5"}, {ID: "claude-sonnet-5-5"}, {ID: "claude-tiny-6"}}, nil
	}
	t.Cleanup(func() { aiListModels = old })
}

// provRun runs a command and the messages it gives back.
func provRun(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		out := cmd()
		if out == nil {
			return
		}
		next, ok := m.handleItermocProvMsg(out)
		if !ok {
			t.Fatalf("%T is not handled", out)
		}
		cmd = next
	}
}

func provCard(m *Model) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(ansi.Strip(m.confirm.Box()), "│", " ")), " ")
}

// Spec (AIP-4): the section is AI PROVIDERS: one row per connected AI with
// its model count, a key from the environment only as a row "via
// environment", the user's own config.json slots after them, and the
// last row "+ Add AI…". The built-in slots are not rows. "models" still
// names the section. No key is in the frame or the snapshot.
func TestItermocProvidersRows(t *testing.T) {
	m := provTestModel(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-"+"or-test-"+"2222") // after the model: it clears the variables
	m.ai.settings.Models = map[string]ai.ModelSlot{"gpt": {Provider: ai.KindOpenAI, Model: "gpt-5-mini"}}
	m.ai.slotFake["gpt"] = aiSlot{cfg: ai.Config{Kind: ai.KindOpenAI, Model: "gpt-5-mini"}}
	m.itermocS().refresh(*m, true)
	if itermocSectionByName("models") != imSecModels || itermocSectionTitle(imSecModels) != "AI PROVIDERS" {
		t.Fatalf("section %q titled %q", itermocSectionByName("models"), itermocSectionTitle(imSecModels))
	}
	f := imFrame(m)
	for _, want := range []string{"AI PROVIDERS", "Anthropic", "4 models", "Ollama", "2 models", "OpenRouter", "environment",
		"from config.json", "gpt", "+ Add AI…"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in\n%s", want, f)
		}
	}
	for _, r := range m.itermoc.rows() {
		if r.kind == imRowSlot && r.name != "gpt" {
			t.Errorf("a built-in slot is a row: %+v", r)
		}
	}
	// OpenAI's key is stored but OpenAI is not connected: no row.
	if strings.Contains(f, "OpenAI") {
		t.Errorf("a stored key that is not connected is a row:\n%s", f)
	}
	for name, text := range map[string]string{"frame": f, "snapshot": stringsOf(m.itermoc.snap), "rows": stringsOf(m.itermoc.rows())} {
		for _, k := range []string{provKey, provOtherKey, "or-test"} {
			if strings.Contains(text, k) {
				t.Errorf("%s leaks a key", name)
			}
		}
	}
}

// Spec: Enter (or a click) on a provider row opens it: its models, when
// they were fetched, Refresh and Disconnect…. Enter again closes it.
func TestItermocProviderExpand(t *testing.T) {
	m := provTestModel(t)
	imCursorTo(t, m, imRowProvider, itermocProvName(ai.KindAnthropic))
	imPress(m, "enter")
	f := imFrame(m)
	for _, want := range []string{"claude-opus-5-5", "claude-sonnet-5-5", "Updated 2 min ago", "Refresh", "Disconnect…"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q in the open row:\n%s", want, f)
		}
	}
	if m.pickerOpen {
		t.Error("Enter on a provider opened a menu instead of the row")
	}
	imPress(m, "enter")
	if f := imFrame(m); strings.Contains(f, "claude-opus-5-5") {
		t.Errorf("Enter again did not close the row:\n%s", f)
	}
	// A click on the row opens it too.
	for _, h := range m.itermocHits(m.explorerWidth - 1) {
		if h.ID == "im:row:"+imSecModels+"/"+itermocProvName(ai.KindOllama) {
			nm, _ := m.handleItermocSidebarMouse(h.X, h.Y, tea.MouseLeft)
			*m = nm
		}
	}
	if f := imFrame(m); !strings.Contains(f, "qwen3:14b") || !strings.Contains(f, "Updated 3 h ago") {
		t.Errorf("a click did not open Ollama:\n%s", f)
	}
}

// Spec: r (or the Refresh row) fetches the provider's models once with its
// stored key, writes the cache and says "Updated just now"; the new
// model is listed. A failed refresh says so in one line without the key.
func TestItermocProviderRefresh(t *testing.T) {
	m := provTestModel(t)
	var fake provFake
	fake.install(t)
	imCursorTo(t, m, imRowProvider, itermocProvName(ai.KindAnthropic))
	provRun(t, m, imPress(m, "r"))
	if n := fake.calls.Load(); n != 1 {
		t.Fatalf("model list called %d times, want 1", n)
	}
	if len(fake.keys) != 1 || fake.keys[0] != provKey {
		t.Errorf("the refresh did not use the stored key")
	}
	cache, _ := ai.LoadModelsCache()
	if e := cache[ai.KindAnthropic]; len(e.Models) != 3 || e.At != clock.Now().Unix() {
		t.Errorf("cache entry %+v", e)
	}
	imPress(m, "enter")
	f := imFrame(m)
	for _, want := range []string{"3 models", "claude-tiny-6", "Updated just now"} {
		if !strings.Contains(f, want) {
			t.Errorf("no %q after Refresh:\n%s", want, f)
		}
	}
	// The Refresh row runs the same.
	imCursorTo(t, m, imRowProvAct, itermocProvName(ai.KindAnthropic)+":refresh")
	provRun(t, m, imPress(m, "enter"))
	if n := fake.calls.Load(); n != 2 {
		t.Fatalf("the Refresh row: %d calls", n)
	}
	fake.fail = true
	provRun(t, m, imPress(m, "r"))
	f = imFrame(m)
	if !strings.Contains(f, "not refreshed") {
		t.Errorf("no failure line:\n%s", f)
	}
	if strings.Contains(stringsOf(m.itermoc.snap), provKey) || strings.Contains(f, provKey) {
		t.Error("the failure shows the key")
	}
}

// Spec (Q2): D asks first with Keep first and focused and names the
// profiles that use the provider. Keep (and Esc) change nothing.
// Disconnect deletes that kind's key through the secret store (the other
// keys stay), its providers.json entry and its cache entry; the row goes.
func TestItermocProviderDisconnect(t *testing.T) {
	m := provTestModel(t)
	m.ai.profFake = []ai.Profile{{Name: "second-opinion", Title: "Checks", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{{ID: "a", Title: "Answer"}, {ID: "b", Title: "Check", Provider: ai.KindAnthropic, Model: "claude-opus-5-5"}}}}
	m.ai.reloadProfiles()
	m.itermocRefreshNow()
	imCursorTo(t, m, imRowProvider, itermocProvName(ai.KindAnthropic))
	imPress(m, "D")
	if !m.confirmOpen || m.confirmKind != confirmKindAIProviderDisconnect {
		t.Fatal("D did not open the Disconnect card")
	}
	if bs := m.confirm.Buttons(); len(bs) < 2 || bs[0].ID != "keep" || m.confirm.SafeID() != "keep" {
		t.Errorf("buttons %+v", bs)
	}
	text := provCard(m)
	for _, want := range []string{"Disconnect Anthropic?", "second-opinion", "secret store"} {
		if !strings.Contains(text, want) {
			t.Errorf("no %q in the card: %s", want, text)
		}
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc
	if addAIStoredKey(t, ai.KindAnthropic) != provKey || addAIProviders(t)[ai.KindAnthropic].Method == "" {
		t.Fatal("Esc changed something")
	}
	imPress(m, "D")
	m.confirmOpen = false
	m.handleAIConfirm("disconnect")
	if addAIStoredKey(t, ai.KindAnthropic) != "" {
		t.Error("the key stays in the secret store")
	}
	if addAIStoredKey(t, ai.KindOpenAI) != provOtherKey {
		t.Error("another key went too")
	}
	ps := addAIProviders(t)
	if _, ok := ps[ai.KindAnthropic]; ok {
		t.Error("the providers.json entry stays")
	}
	if _, ok := ps[ai.KindOllama]; !ok {
		t.Error("Ollama went too")
	}
	if cache, _ := ai.LoadModelsCache(); len(cache[ai.KindAnthropic].Models) != 0 {
		t.Error("the cache entry stays")
	}
	if strings.Contains(imFrame(m), "Anthropic") {
		t.Errorf("the row stays:\n%s", imFrame(m))
	}
}

// Spec (Q2): Disconnect of a signed-in AI never runs the CLI (no logout):
// the card says the CLI stays signed in; only termocode forgets it.
func TestItermocProviderDisconnectCLIKeepsSignIn(t *testing.T) {
	m := provTestModel(t)
	if err := ai.SaveProvider(mustProvidersPath(t), ai.ProviderEntry{Kind: ai.KindOpenAI, Method: ai.MethodCLI, CLI: ai.CLICodex}); err != nil {
		t.Fatal(err)
	}
	m.ai.reload()
	m.itermocS().refresh(*m, true)
	old := aiCLICheck
	t.Cleanup(func() { aiCLICheck = old })
	aiCLICheck = nil // any CLI run panics
	imCursorTo(t, m, imRowProvider, itermocProvName(ai.KindOpenAI))
	imPress(m, "D")
	if text := provCard(m); !strings.Contains(text, "Codex stays signed in") {
		t.Errorf("card: %s", text)
	}
	m.confirmOpen = false
	m.handleAIConfirm("disconnect")
	if _, ok := addAIProviders(t)[ai.KindOpenAI]; ok {
		t.Error("the entry stays")
	}
	if addAIStoredKey(t, ai.KindOpenAI) != provOtherKey {
		t.Error("a sign-in Disconnect deleted the API key of that kind")
	}
}

// Spec: a key that only the environment gives is a row with no
// Disconnect (D does nothing there); Refresh works.
func TestItermocProviderEnvOnly(t *testing.T) {
	m := provTestModel(t)
	t.Setenv("OPENROUTER_API_KEY", "sk-"+"or-test-"+"3333")
	m.itermocS().refresh(*m, true)
	row := itermocProvName(ai.KindOpenRouter)
	imCursorTo(t, m, imRowProvider, row)
	imPress(m, "D")
	if m.confirmOpen {
		t.Fatal("D opened a card on an environment key")
	}
	imPress(m, "enter")
	f := imFrame(m)
	if !strings.Contains(f, "Refresh") || strings.Contains(f, "Disconnect") {
		t.Errorf("open env row:\n%s", f)
	}
	for _, r := range m.itermoc.rows() {
		if r.kind == imRowProvAct && strings.HasSuffix(r.name, ":disconnect") {
			t.Errorf("a Disconnect row: %+v", r)
		}
	}
}

// Spec: the snapshot is built from the providers and the model cache in
// memory: drawing again and again does not read the cache file.
func TestItermocProvidersNoDiskPerFrame(t *testing.T) {
	m := provTestModel(t)
	var reads atomic.Int32
	old := itermocLoadModelsCache
	itermocLoadModelsCache = func() (ai.ModelsCache, error) { reads.Add(1); return old() }
	t.Cleanup(func() { itermocLoadModelsCache = old })
	m.itermocRefreshNow() // the cache is already in memory
	for i := 0; i < 20; i++ {
		m.itermoc.snapAt = time.Time{} // stale: each frame builds again
		_ = imFrame(m)
	}
	if n := reads.Load(); n != 0 {
		t.Errorf("the cache file was read %d times while drawing", n)
	}
	if !strings.Contains(imFrame(m), "4 models") {
		t.Error("no model count")
	}
}

// Spec: "+ Add AI…" (and a) opens the Add AI card; a provider added
// there is a row on Done without a restart.
func TestItermocAddAIRow(t *testing.T) {
	m := provTestModel(t)
	imCursorTo(t, m, imRowAdd, "+add")
	for _, r := range m.itermoc.rows() {
		if r.kind == imRowAdd && r.sec == imSecModels {
			m.itermoc.cursorTo(r)
		}
	}
	imPress(m, "enter")
	if !m.addAIOpen {
		t.Fatal("+ Add AI did not open the card")
	}
	m.closeAddAI()
	imPress(m, "a")
	if !m.addAIOpen {
		t.Fatal("a did not open the card")
	}
	// The card saves OpenRouter (its own test covers how); Done shows it.
	if err := ai.SaveProvider(mustProvidersPath(t), ai.ProviderEntry{Kind: ai.KindOpenRouter, Method: ai.MethodKey}); err != nil {
		t.Fatal(err)
	}
	m.ai.reload()
	if _, ok := m.handleAddAIMsg(addai.DoneMsg{}); !ok {
		t.Fatal("DoneMsg not handled")
	}
	if f := imFrame(m); !strings.Contains(f, "OpenRouter") {
		t.Errorf("the new AI is not a row after Done:\n%s", f)
	}
}

// Spec: what needs you: a profile step on an AI that is not connected is
// "OpenAI · not connected" with an Add pill that opens the card on
// OpenAI; a sign-in AI whose CLI is signed out is "Sign in" that opens
// the card straight on that AI's sign-in step.
func TestItermocProviderNeeds(t *testing.T) {
	m := provTestModel(t)
	m.ai.profFake = []ai.Profile{{Name: "gpt-check", Source: ai.ProfileSourceUser,
		Steps: []ai.ProfileStep{{ID: "a", Title: "Answer"}, {ID: "b", Title: "Check", Provider: ai.KindOpenAI, Model: "gpt-5-mini"}}}}
	m.ai.reloadProfiles()
	m.itermocRefreshNow()
	var add *imNeed
	for _, n := range m.itermoc.snap.needs() {
		if n.fix.verb == "addai:"+ai.KindOpenAI {
			n := n
			add = &n
		}
	}
	if add == nil || add.name != "OpenAI" || !strings.Contains(add.what, "gpt-check") || add.fix.label != "Add" {
		t.Fatalf("no Add need: %+v", m.itermoc.snap.needs())
	}
	m.itermocRun(add.fix.verb, add.row)
	if !m.addAIOpen || m.addAI.Kind() != ai.KindOpenAI {
		t.Fatalf("Add opened %v on %q", m.addAIOpen, m.addAI.Kind())
	}
	m.closeAddAI()

	if err := ai.SaveProvider(mustProvidersPath(t), ai.ProviderEntry{Kind: ai.KindOpenAI, Method: ai.MethodCLI, CLI: ai.CLICodex}); err != nil {
		t.Fatal(err)
	}
	m.ai.reload()
	m.ai.cliStates = map[string]profileform.CLIState{ai.CLICodex: profileform.CLILoggedOut}
	m.itermocRefreshNow()
	var sign *imNeed
	for _, n := range m.itermoc.snap.needs() {
		if n.row.kind == imRowProvider && n.row.name == itermocProvName(ai.KindOpenAI) {
			n := n
			sign = &n
		}
	}
	if sign == nil || !strings.Contains(sign.what, "Codex") || sign.fix.label != "Sign in" {
		t.Fatalf("no sign-in need: %+v", m.itermoc.snap.needs())
	}
	if badge, needs := m.itermoc.sectionBadge(imSecModels); !needs {
		t.Errorf("badge %q", badge)
	}
	m.itermocRun(sign.fix.verb, sign.row)
	if !m.addAIOpen || m.addAI.Kind() != ai.KindOpenAI || m.addAI.Step() != addai.StepSignIn || m.addAI.Method() != ai.MethodCLI {
		t.Fatalf("Sign in opened the card at %v (%q) on %q, want the sign-in step", m.addAI.Step(), m.addAI.Method(), m.addAI.Kind())
	}
}

// Spec: the AI tool models_list lists the connected AIs with how they are
// connected and their model ids; never a key or a URL's user info.
func TestAIModelsListProviders(t *testing.T) {
	m := provTestModel(t)
	t.Setenv("OPENROUTER_API_KEY", provKey)
	if err := ai.SaveProvider(mustProvidersPath(t), ai.ProviderEntry{Kind: ai.KindCompatible, Method: ai.MethodURL,
		BaseURL: "https://llm.example.com:8443/v1", Title: "vLLM"}); err != nil {
		t.Fatal(err)
	}
	m.ai.reload()
	out, err := aiModelsInfo(m, itermocBuild(*m, true))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(out)
	var got struct {
		Providers []struct {
			Kind    string   `json:"kind"`
			Name    string   `json:"name"`
			Method  string   `json:"method"`
			Models  []string `json:"models"`
			Base    string   `json:"base_host"`
			EnvOnly bool     `json:"from_environment"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	byKind := map[string]int{}
	for i, p := range got.Providers {
		byKind[p.Kind] = i
	}
	a, ok := byKind[ai.KindAnthropic]
	if !ok || got.Providers[a].Method != ai.MethodKey || len(got.Providers[a].Models) != 4 || got.Providers[a].Name != "Anthropic" {
		t.Errorf("anthropic: %+v", got.Providers)
	}
	if i, ok := byKind[ai.KindOpenRouter]; !ok || !got.Providers[i].EnvOnly {
		t.Errorf("openrouter from the environment: %+v", got.Providers)
	}
	if i, ok := byKind[ai.KindCompatible]; !ok || got.Providers[i].Base != "https://llm.example.com:8443" {
		t.Errorf("compatible: %+v", got.Providers)
	}
	for _, k := range []string{provKey, provOtherKey} {
		if strings.Contains(string(data), k) {
			t.Error("models_list leaks a key")
		}
	}
	if !strings.Contains(string(data), `"slots"`) {
		t.Error("the slots are gone from models_list")
	}
}

// Spec: opening Itermoc loads, in the background, only the model lists
// older than an hour (Ollama's is 3 h old, Anthropic's 2 min).
func TestItermocProvidersRefreshOnOpen(t *testing.T) {
	m := provTestModel(t)
	var kinds []string
	old := aiListModels
	aiListModels = func(_ context.Context, cfg ai.Config) ([]ai.ModelInfo, error) {
		kinds = append(kinds, cfg.Kind)
		return []ai.ModelInfo{{ID: "llama3.2:3b"}}, nil
	}
	t.Cleanup(func() { aiListModels = old })
	cmd := m.revealItermocView(true, "")
	if cmd == nil {
		t.Fatal("no background refresh")
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			provRun(t, m, c)
		}
	} else if msg != nil {
		provRun(t, m, func() tea.Msg { return msg })
	}
	if len(kinds) != 1 || kinds[0] != ai.KindOllama {
		t.Fatalf("refreshed %v, want only ollama", kinds)
	}
	if cache, _ := ai.LoadModelsCache(); cache[ai.KindOllama].At != clock.Now().Unix() {
		t.Error("the cache was not written")
	}
}

// Spec: ai_view_open lands on a connected AI by its kind or its name.
func TestAIViewOpenProviderRow(t *testing.T) {
	m := provTestModel(t)
	for _, row := range []string{"anthropic", "Anthropic"} {
		if got := aiViewOpen(m, imSecModels, row); !strings.Contains(got, "row "+row) {
			t.Errorf("%q: %s", row, got)
		}
		if r, _ := m.itermoc.current(); r.kind != imRowProvider || r.name != itermocProvName(ai.KindAnthropic) {
			t.Errorf("%q: cursor on %+v", row, r)
		}
	}
}

func stringsOf(v any) string { return fmt.Sprintf("%+v %#v", v, v) }
