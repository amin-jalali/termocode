package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clistep"
)

// addAIKey is a fake key, split so no scanner takes it for a real one.
var addAIKey = "sk-" + "ant-test-" + "0000aaaa1111"

// addAIServer is a fake model list: Anthropic's /v1/models answers the
// good key and refuses any other with a body that repeats the key it
// got; /models (OpenAI style) answers with or without a key.
func addAIServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			if got := r.Header.Get("x-api-key"); got != addAIKey {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid x-api-key ` + got + `"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-5-5"},{"id":"claude-opus-5-5"}]}`))
		case "/v1/models/", "/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"my-model"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// fakeAddAIList routes every model list to srv and records the keys it
// was given; refuse makes a local server "not running".
type fakeAddAIList struct {
	mu     sync.Mutex
	keys   []string
	refuse bool
}

func (f *fakeAddAIList) install(t *testing.T, srv *httptest.Server) {
	t.Helper()
	old := aiListModels
	aiListModels = func(ctx context.Context, cfg ai.Config) ([]ai.ModelInfo, error) {
		f.mu.Lock()
		f.keys = append(f.keys, cfg.APIKey)
		refuse := f.refuse
		f.mu.Unlock()
		if cfg.Kind == ai.KindOllama || cfg.Kind == ai.KindLMStudio {
			if refuse {
				return nil, errors.New("dial tcp 127.0.0.1:11434: connect: connection refused")
			}
			return []ai.ModelInfo{{ID: "llama3.2:3b"}, {ID: "qwen3:14b"}}, nil
		}
		if cfg.Kind != ai.KindCompatible {
			cfg.BaseURL = srv.URL
		}
		return ai.ListModels(ctx, cfg)
	}
	t.Cleanup(func() { aiListModels = old })
}

func addAITestModel(t *testing.T) *Model {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	m := aiTestModel(t)
	m.w, m.h = 120, 40
	return &m
}

// addAIRun runs a command the way the app's loop does, and the messages
// it gives back, until nothing is left.
func addAIRun(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for cmd != nil {
		out := cmd()
		if out == nil {
			return
		}
		next, ok := m.handleAddAIMsg(out)
		if !ok {
			t.Fatalf("%T is not handled", out)
		}
		cmd = next
	}
}

// addAISend sends one key (or click) to the open card and runs what it
// gives back.
func addAISend(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	nm, cmd := m.routeToAddAI(msg)
	*m = nm
	addAIRun(t, m, cmd)
}

func addAIKeyT(t *testing.T, m *Model, k tea.KeyType) { addAISend(t, m, tea.KeyMsg{Type: k}) }

func addAICardText(m *Model) string { return ansi.Strip(m.addAI.Box()) }

// addAIPick opens the card from the palette and picks kind.
func addAIPick(t *testing.T, m *Model, kind string) {
	t.Helper()
	cmd, ok := m.dispatchItermocPalette("ai-add")
	if !ok {
		t.Fatal("no palette command ai-add")
	}
	addAIRun(t, m, cmd)
	if !m.addAIOpen {
		t.Fatal("the card did not open")
	}
	for _, e := range addai.Catalog() {
		if e.Kind == kind {
			break
		}
		addAIKeyT(t, m, tea.KeyDown)
	}
	addAIKeyT(t, m, tea.KeyEnter)
}

func addAIProviders(t *testing.T) map[string]ai.ProviderEntry {
	t.Helper()
	p, err := ai.ProvidersPath()
	if err != nil {
		t.Fatal(err)
	}
	ps, err := ai.LoadProviders(p)
	if err != nil {
		return nil
	}
	return ps
}

func addAIStoredKey(t *testing.T, kind string) string {
	t.Helper()
	creds, _ := ai.LoadCredentials(mustCredPath(t))
	return creds.Keys[kind]
}

// Spec (AIP-3 accept): a user connects Anthropic with a key in one card
// and sees its real model list. The typed key goes only to the model
// list call; a refused key is one sentence with no key in it and nothing
// saved; only the key that works is saved, in the secret store, with the
// providers.json entry and the model cache. config.json is never written.
func TestAddAIKeyPath(t *testing.T) {
	m := addAITestModel(t)
	f := &fakeAddAIList{}
	f.install(t, addAIServer(t))
	addAIPick(t, m, ai.KindAnthropic)
	addAIKeyT(t, m, tea.KeyDown) // Use an API key
	addAIKeyT(t, m, tea.KeyEnter)
	if m.addAI.Step() != addai.StepKey {
		t.Fatalf("step %v", m.addAI.Step())
	}

	bad := "sk-" + "ant-test-" + "WRONG9999"
	addAISend(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(bad)})
	addAIKeyT(t, m, tea.KeyEnter)
	s := addAICardText(m)
	if !strings.Contains(s, "That key was not accepted.") {
		t.Fatalf("card after a refused key:\n%s", s)
	}
	if strings.Contains(s, bad) || strings.Contains(s, "WRONG") || strings.Contains(s, "401") {
		t.Errorf("the card shows the key or a raw error:\n%s", s)
	}
	if m.addAI.Value("key") != bad {
		t.Error("the refused key left the field")
	}
	if addAIStoredKey(t, ai.KindAnthropic) != "" || addAIProviders(t) != nil {
		t.Fatal("a refused key was saved")
	}

	m.addAI.SetValue("key", addAIKey)
	addAIKeyT(t, m, tea.KeyEnter)
	s = addAICardText(m)
	if m.addAI.Step() != addai.StepDone || !strings.Contains(s, "Anthropic is ready") ||
		!strings.Contains(s, "claude-opus-5-5") || !strings.Contains(s, "claude-sonnet-5-5") {
		t.Fatalf("done card:\n%s", s)
	}
	if got := addAIStoredKey(t, ai.KindAnthropic); got != addAIKey {
		t.Errorf("stored key %q", got)
	}
	if p := addAIProviders(t)[ai.KindAnthropic]; p.Method != ai.MethodKey || p.AddedAt == 0 {
		t.Errorf("providers.json entry %+v", p)
	}
	cache, _ := ai.LoadModelsCache()
	if e := cache[ai.KindAnthropic]; len(e.Models) != 2 {
		t.Errorf("models cache %+v", cache)
	}
	f.mu.Lock()
	keys := append([]string(nil), f.keys...)
	f.mu.Unlock()
	if len(keys) != 2 || keys[0] != bad || keys[1] != addAIKey {
		t.Errorf("model list calls got keys %q", keys)
	}
	cfgPath, _ := ai.ConfigPath()
	if _, err := os.Stat(cfgPath); err == nil {
		t.Error("the card wrote config.json")
	}
	for _, p := range []string{mustProvidersPath(t), mustCachePath(t)} {
		b, _ := os.ReadFile(p)
		if strings.Contains(string(b), "test-0000") {
			t.Errorf("%s holds the key", p)
		}
	}
	addAIKeyT(t, m, tea.KeyEnter) // Done
	if m.addAIOpen {
		t.Error("Done did not close the card")
	}
	if p, ok := m.aiS().providers[ai.KindAnthropic]; !ok || p.Method != ai.MethodKey {
		t.Errorf("the app does not see the new provider: %+v", m.aiS().providers)
	}
}

func mustProvidersPath(t *testing.T) string {
	t.Helper()
	p, err := ai.ProvidersPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustCachePath(t *testing.T) string {
	t.Helper()
	p, err := ai.ModelsCachePath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Spec (AIP-3 accept): Esc at any step leaves nothing behind, also while
// the key is being checked: the answer that comes after is dropped.
func TestAddAIEscLeavesNothing(t *testing.T) {
	m := addAITestModel(t)
	old := aiListModels
	t.Cleanup(func() { aiListModels = old })
	// A server that answers although the card stopped waiting.
	aiListModels = func(context.Context, ai.Config) ([]ai.ModelInfo, error) {
		return []ai.ModelInfo{{ID: "claude-opus-5-5"}}, nil
	}
	addAIPick(t, m, ai.KindAnthropic)
	addAIKeyT(t, m, tea.KeyDown)
	addAIKeyT(t, m, tea.KeyEnter)
	addAISend(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(addAIKey)})
	nm, cmd := m.routeToAddAI(tea.KeyMsg{Type: tea.KeyEnter})
	*m = nm
	// The request goes out; its check is still running when Esc comes.
	req := cmd()
	check, _ := m.handleAddAIMsg(req)
	addAIKeyT(t, m, tea.KeyEsc)
	if m.addAIOpen {
		t.Fatal("Esc did not close the card")
	}
	addAIRun(t, m, check) // the late answer
	if addAIStoredKey(t, ai.KindAnthropic) != "" || addAIProviders(t) != nil {
		t.Error("a late answer saved the key")
	}
	cache, _ := ai.LoadModelsCache()
	if len(cache) != 0 {
		t.Errorf("a late answer filled the cache: %+v", cache)
	}

	// Esc on each step before a request.
	for _, kind := range []string{ai.KindOpenAI, ai.KindOpenRouter, ai.KindCompatible} {
		m := addAITestModel(t)
		addAIPick(t, m, kind)
		addAIKeyT(t, m, tea.KeyEsc)
		if m.addAIOpen || addAIProviders(t) != nil {
			t.Errorf("%s: Esc left the card open or wrote providers.json", kind)
		}
	}
}

// Spec: a local AI that does not answer is "not running" with Try again
// (no command is run); once it runs, Try again connects it with no key.
func TestAddAILocal(t *testing.T) {
	m := addAITestModel(t)
	f := &fakeAddAIList{refuse: true}
	f.install(t, addAIServer(t))
	addAIPick(t, m, ai.KindOllama)
	s := addAICardText(m)
	if !strings.Contains(s, "Ollama is not running on this machine.") || !strings.Contains(s, "Try again") ||
		strings.Contains(s, "refused") || strings.Contains(s, "11434") {
		t.Fatalf("card:\n%s", s)
	}
	f.mu.Lock()
	f.refuse = false
	f.mu.Unlock()
	addAIKeyT(t, m, tea.KeyEnter) // Try again is lit
	if m.addAI.Step() != addai.StepDone || !strings.Contains(addAICardText(m), "qwen3:14b") {
		t.Fatalf("after Try again:\n%s", addAICardText(m))
	}
	if p := addAIProviders(t)[ai.KindOllama]; p.Method != ai.MethodLocal {
		t.Errorf("providers.json entry %+v", p)
	}
	creds, _ := ai.LoadCredentials(mustCredPath(t))
	if len(creds.Keys) != 0 {
		t.Errorf("a local AI saved a key: %v", len(creds.Keys))
	}
}

// Spec: Other server takes an OpenAI-style URL with an optional key; a
// URL with a user or password is refused before any request.
func TestAddAIServerURL(t *testing.T) {
	m := addAITestModel(t)
	f := &fakeAddAIList{}
	srv := addAIServer(t)
	f.install(t, srv)
	addAIPick(t, m, ai.KindCompatible)
	m.addAI.SetValue("url", strings.Replace(srv.URL, "http://", "http://bob:"+"hunter2@", 1))
	addAIKeyT(t, m, tea.KeyEnter)
	if len(f.keys) != 0 || !strings.Contains(addAICardText(m), "user or password") {
		t.Fatalf("a URL with a password went out (%d calls):\n%s", len(f.keys), addAICardText(m))
	}
	m.addAI.SetValue("url", srv.URL)
	m.addAI.SetValue("key", addAIKey)
	addAIKeyT(t, m, tea.KeyEnter)
	if m.addAI.Step() != addai.StepDone || !strings.Contains(addAICardText(m), "my-model") {
		t.Fatalf("card:\n%s", addAICardText(m))
	}
	p := addAIProviders(t)[ai.KindCompatible]
	if p.Method != ai.MethodURL || p.BaseURL != srv.URL {
		t.Errorf("providers.json entry %+v", p)
	}
	if addAIStoredKey(t, ai.KindCompatible) != addAIKey {
		t.Error("the server's key was not stored")
	}
}

// Spec: the first entry written keeps the providers the setup already
// had (a stored key), so the file does not forget them.
func TestAddAIKeepsDerivedProviders(t *testing.T) {
	m := addAITestModel(t)
	creds, _ := ai.LoadCredentials(mustCredPath(t))
	creds.Keys = map[string]string{ai.KindOpenAI: "sk-" + "test-" + "openai0000"}
	if err := ai.SaveCredentials(mustCredPath(t), creds); err != nil {
		t.Fatal(err)
	}
	m.aiS().reload()
	f := &fakeAddAIList{}
	f.install(t, addAIServer(t))
	addAIPick(t, m, ai.KindOllama)
	ps := addAIProviders(t)
	if ps[ai.KindOllama].Method != ai.MethodLocal || ps[ai.KindOpenAI].Method != ai.MethodKey {
		t.Errorf("providers.json %+v", ps)
	}
}

// Spec: Sign in uses the CLI's own sign-in when it is there (its built-in
// models); when it is not, one sentence with Use an API key / Try again.
func TestAddAISignIn(t *testing.T) {
	m := addAITestModel(t)
	old := aiCLICheck
	t.Cleanup(func() { aiCLICheck = old })
	status := clistep.StatusLoggedOut
	aiCLICheck = func(_ context.Context, cli string, _ []string) clistep.Status { return status }
	addAIPick(t, m, ai.KindOpenAI)
	addAIKeyT(t, m, tea.KeyEnter) // Sign in with ChatGPT Plus / Pro
	s := addAICardText(m)
	if !strings.Contains(s, "Codex is not signed in on this machine.") || !strings.Contains(s, "Use an API key") {
		t.Fatalf("card:\n%s", s)
	}
	if addAIProviders(t) != nil {
		t.Fatal("a failed sign-in check wrote providers.json")
	}
	status = clistep.StatusReady
	addAIKeyT(t, m, tea.KeyEnter) // Try again
	if m.addAI.Step() != addai.StepDone || !strings.Contains(addAICardText(m), "gpt-5-codex") {
		t.Fatalf("card:\n%s", addAICardText(m))
	}
	if p := addAIProviders(t)[ai.KindOpenAI]; p.Method != ai.MethodCLI || p.CLI != ai.CLICodex {
		t.Errorf("providers.json entry %+v", p)
	}
}

// Spec: the hit map names every button and every AI of the list, with
// its title drawn there, and a click there reaches it.
func TestAddAIHitTargets(t *testing.T) {
	m := addAITestModel(t)
	f := &fakeAddAIList{refuse: true}
	f.install(t, addAIServer(t))
	cmd, _ := m.dispatchItermocPalette("ai-add")
	addAIRun(t, m, cmd)
	check := func(step string, want ...string) {
		t.Helper()
		ids := map[string]bool{}
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		for _, h := range m.hitTargets() {
			ids[h.ID] = true
			if h.Y < len(lines) && !strings.Contains(lines[h.Y], h.Label) {
				t.Errorf("%s: %s: %q not drawn on row %d", step, h.ID, h.Label, h.Y)
			}
			if got := m.probeClick(h.X, h.Y); got != h.ID {
				t.Errorf("%s: a click on %s reaches %q", step, h.ID, got)
			}
		}
		for _, w := range want {
			if !ids[w] {
				t.Errorf("%s: no target %s (%v)", step, w, ids)
			}
		}
	}
	check("pick", "addai:cancel", "addai:pick:anthropic", "addai:pick:openai-compatible")
	addAIKeyT(t, m, tea.KeyEnter)
	check("method", "addai:back", "addai:cancel", "addai:pick:cli", "addai:pick:key")
	addAIKeyT(t, m, tea.KeyDown)
	addAIKeyT(t, m, tea.KeyEnter)
	check("key", "addai:back", "addai:cancel", "addai:connect")
}

// Spec: the palette has AI: Add AI… next to AI: Add Model Slot…, and the
// AI can never run it (keys stay with the user).
func TestAddAIPaletteEntry(t *testing.T) {
	items := itermocPaletteItems()
	at := -1
	for i, it := range items {
		if it.ID == "ai-add" && it.Title == "AI: Add AI…" {
			at = i
		}
	}
	if at < 0 || at+1 >= len(items) || items[at+1].ID != "ai-model-slot-add" {
		t.Fatalf("ai-add not next to ai-model-slot-add: %+v", items)
	}
	if p, ok := aiActionPolicies["ai-add"]; !ok || p.Gate != aiGateNever {
		t.Errorf("policy %+v %v", p, ok)
	}
}
