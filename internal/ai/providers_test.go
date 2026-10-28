package ai

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestProvidersPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	p, err := ProvidersPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "termocode", "ai", "providers.json"); p != want {
		t.Fatalf("path %q, want %q", p, want)
	}
}

func TestProvidersMissingFile(t *testing.T) {
	ps, err := LoadProviders(filepath.Join(t.TempDir(), "providers.json"))
	if !errors.Is(err, fs.ErrNotExist) || len(ps) != 0 {
		t.Fatalf("missing file: %v %v", ps, err)
	}
}

func TestProvidersRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai", "providers.json")
	for _, p := range []ProviderEntry{
		{Kind: KindAnthropic, Method: MethodKey, AddedAt: 1700000000},
		{Kind: KindOpenAI, Method: MethodCLI, CLI: CLICodex},
		{Kind: KindOllama, Method: MethodLocal, BaseURL: "http://localhost:11434/v1"},
		{Kind: KindCompatible, Method: MethodURL, BaseURL: "http://localhost:8000/v1", Title: "vLLM"},
	} {
		if err := SaveProvider(path, p); err != nil {
			t.Fatal(err)
		}
	}
	ps, err := LoadProviders(path)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]ProviderEntry{
		KindAnthropic:  {Kind: KindAnthropic, Method: MethodKey, AddedAt: 1700000000},
		KindOpenAI:     {Kind: KindOpenAI, Method: MethodCLI, CLI: CLICodex},
		KindOllama:     {Kind: KindOllama, Method: MethodLocal, BaseURL: "http://localhost:11434/v1"},
		KindCompatible: {Kind: KindCompatible, Method: MethodURL, BaseURL: "http://localhost:8000/v1", Title: "vLLM"},
	}
	if !reflect.DeepEqual(ps, want) {
		t.Fatalf("got %+v\nwant %+v", ps, want)
	}
	if err := RemoveProvider(path, KindOpenAI); err != nil {
		t.Fatal(err)
	}
	if err := RemoveProvider(path, KindOpenAI); err != nil {
		t.Fatalf("removing a provider that is not there: %v", err)
	}
	ps, _ = LoadProviders(path)
	if _, ok := ps[KindOpenAI]; ok || len(ps) != 3 {
		t.Fatalf("after remove: %+v", ps)
	}
}

// A newer termocode may write keys this one does not know: a save keeps
// them, at the top and inside an entry.
func TestProvidersKeepUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	old := `{"version": 2, "providers": {"anthropic": {"method": "key", "note": "work"}, "ollama": {"method": "local", "x": [1, 2]}}}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveProvider(path, ProviderEntry{Kind: KindAnthropic, Method: MethodCLI, CLI: CLIClaude}); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version   int                                   `json:"version"`
		Providers map[string]map[string]json.RawMessage `json:"providers"`
	}
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 2 || string(doc.Providers["anthropic"]["note"]) != `"work"` || strings.Join(strings.Fields(string(doc.Providers["ollama"]["x"])), "") != "[1,2]" {
		t.Fatalf("unknown keys lost: %s", data)
	}
	if string(doc.Providers["anthropic"]["method"]) != `"cli"` || string(doc.Providers["anthropic"]["cli"]) != `"claude"` {
		t.Fatalf("entry not replaced: %s", data)
	}
}

func TestSaveProviderRejects(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	cases := []ProviderEntry{
		{Kind: "gemini", Method: MethodKey},
		{Kind: KindAnthropic, Method: "magic"},
		{Kind: KindOpenAI, Method: MethodCLI, CLI: "nope"},
		{Kind: KindCompatible, Method: MethodURL, BaseURL: "http://user:pw@host/v1"},
		{Kind: KindCompatible, Method: MethodURL, BaseURL: "ftp://host"},
	}
	for _, p := range cases {
		if err := SaveProvider(path, p); err == nil {
			t.Errorf("%+v saved", p)
		}
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("a rejected provider wrote the file")
	}
}

// providers.json is not secret: a key never lands in it, even when the
// stored credentials hold one and the list is derived from them.
func TestProvidersHoldNoSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	key := "sk-" + "ant-test-5555555555555555"
	creds := Credentials{Keys: map[string]string{KindAnthropic: key}}
	ps := DeriveProviders(Settings{}, creds, nil)
	if err := SaveProviders(path, ps); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), key) || strings.Contains(string(data), "ant-test") {
		t.Fatalf("providers.json holds a key: %s", data)
	}
	if !strings.Contains(string(data), `"anthropic"`) {
		t.Fatalf("anthropic missing: %s", data)
	}
}

// An existing setup shows up connected: a stored key, a ready CLI and a
// local ai_provider each become an entry.
func TestDeriveProviders(t *testing.T) {
	creds := Credentials{Keys: map[string]string{KindOpenRouter: "sk-" + "or-test-1", KindAnthropic: "sk-" + "ant-test-2"}}
	ready := func(cli string) bool { return cli == CLIClaude || cli == CLICodex }
	got := DeriveProviders(Settings{Provider: KindOllama, BaseURL: "http://box:11434/v1"}, creds, ready)
	want := map[string]ProviderEntry{
		KindAnthropic:  {Kind: KindAnthropic, Method: MethodKey}, // a key beats the CLI: one method per provider
		KindOpenRouter: {Kind: KindOpenRouter, Method: MethodKey},
		KindOpenAI:     {Kind: KindOpenAI, Method: MethodCLI, CLI: CLICodex},
		KindOllama:     {Kind: KindOllama, Method: MethodLocal, BaseURL: "http://box:11434/v1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	got = DeriveProviders(Settings{Provider: KindCompatible, BaseURL: "http://localhost:8000/v1"}, Credentials{}, nil)
	if !reflect.DeepEqual(got, map[string]ProviderEntry{KindCompatible: {Kind: KindCompatible, Method: MethodURL, BaseURL: "http://localhost:8000/v1"}}) {
		t.Fatalf("compatible: %+v", got)
	}
	got = DeriveProviders(Settings{Provider: KindLMStudio}, Credentials{}, func(string) bool { return false })
	if !reflect.DeepEqual(got, map[string]ProviderEntry{KindLMStudio: {Kind: KindLMStudio, Method: MethodLocal}}) {
		t.Fatalf("lmstudio: %+v", got)
	}
	// A base URL with a user or password is not copied (it may hold a key).
	got = DeriveProviders(Settings{Provider: KindCompatible, BaseURL: "http://u:p@h/v1"}, Credentials{}, nil)
	if len(got) != 0 {
		t.Fatalf("a URL with a password became a provider: %+v", got)
	}
}

func TestProviderCostTagAndMethodLabel(t *testing.T) {
	cases := []struct {
		p          ProviderEntry
		tag, label string
	}{
		{ProviderEntry{Kind: KindAnthropic, Method: MethodKey}, "API · pay per use", "API key"},
		{ProviderEntry{Kind: KindAnthropic, Method: MethodCLI, CLI: CLIClaude}, "your plan", "sign-in via Claude Code"},
		{ProviderEntry{Kind: KindOpenAI, Method: MethodCLI, CLI: CLICodex}, "your plan", "sign-in via Codex"},
		{ProviderEntry{Kind: KindOllama, Method: MethodLocal}, "local · free", "on this machine"},
		{ProviderEntry{Kind: KindCompatible, Method: MethodURL, BaseURL: "http://localhost:8000/v1"}, "local · free", "server URL"},
		{ProviderEntry{Kind: KindCompatible, Method: MethodURL, BaseURL: "https://llm.example.com/v1"}, "API · pay per use", "server URL"},
	}
	for _, c := range cases {
		if got := CostTag(c.p); got != c.tag {
			t.Errorf("%+v: CostTag %q, want %q", c.p, got, c.tag)
		}
		if got := MethodLabel(c.p); got != c.label {
			t.Errorf("%+v: MethodLabel %q, want %q", c.p, got, c.label)
		}
	}
}
