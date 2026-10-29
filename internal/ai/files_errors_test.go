package ai

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// unchanged fails the test when the file at p no longer holds body.
func unchanged(t *testing.T, p, body string) {
	t.Helper()
	if b, _ := os.ReadFile(p); string(b) != body {
		t.Errorf("file changed: %s", b)
	}
}

func TestModelSlotsRefuseWrongShapes(t *testing.T) {
	sl := ModelSlot{Provider: KindOllama, Model: "qwen"}
	for _, body := range []string{`{"ai_models": 3}`, `{"ai_models": {"fast": "x"}}`} {
		p := writeConfig(t, body)
		if err := SaveModelSlot(p, "fast", sl); err == nil || !strings.Contains(err.Error(), "not a JSON object") {
			t.Errorf("%s: err = %v", body, err)
		}
		unchanged(t, p, body)
	}
	p := writeConfig(t, `{"ai_models": [1]}`)
	if err := RemoveModelSlot(p, "fast"); err == nil {
		t.Error("remove from a list must fail")
	}
	// Removing the last slot drops ai_models; other keys stay.
	p = writeConfig(t, `{"theme": "dark", "ai_models": {"fast": {"provider": "ollama"}}}`)
	if err := RemoveModelSlot(p, "fast"); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	b, _ := os.ReadFile(p)
	_ = json.Unmarshal(b, &doc)
	if _, ok := doc["ai_models"]; ok || doc["theme"] != "dark" {
		t.Errorf("doc = %v", doc)
	}
}

func TestProvidersFileErrors(t *testing.T) {
	entry := ProviderEntry{Kind: KindOllama, Method: MethodLocal}
	for _, body := range []string{`{"providers": "x"}`, `{"providers": {"ollama": 1}}`} {
		p := writeConfig(t, body)
		if err := SaveProvider(p, entry); err == nil {
			t.Errorf("%s: save accepted", body)
		}
		unchanged(t, p, body)
	}
	p := writeConfig(t, `{"providers": []}`)
	if err := RemoveProvider(p, KindOllama); err == nil {
		t.Error("remove from a list must fail")
	}
	if err := RemoveProvider(filepath.Join(t.TempDir(), "none.json"), KindOllama); err != nil {
		t.Errorf("remove from a missing file: %v", err)
	}
	p = writeConfig(t, `{"providers": `)
	if _, err := LoadProviders(p); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("load broken: %v", err)
	}
}

// Saving an empty list writes "providers": {} so the list is not derived
// again on the next start; a bad entry is reported, good ones still saved.
func TestSaveProvidersEmptyAndPartlyBad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ai", "providers.json")
	if err := SaveProviders(p, nil); err != nil {
		t.Fatal(err)
	}
	got, err := LoadProviders(p)
	if err != nil || len(got) != 0 {
		t.Fatalf("empty list = %v, %v", got, err)
	}
	err = SaveProviders(p, map[string]ProviderEntry{
		KindOllama: {Method: MethodLocal},
		"mystery":  {Method: MethodKey},
	})
	if err == nil || !strings.Contains(err.Error(), "mystery") {
		t.Errorf("bad entry not reported: %v", err)
	}
	if got, _ := LoadProviders(p); len(got) != 1 || got[KindOllama].Method != MethodLocal {
		t.Errorf("good entry not saved: %v", got)
	}
}

func TestPathsWithoutHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "")
	for name, f := range map[string]func() (string, error){
		"providers": ProvidersPath, "profiles": ProfilesPath, "models cache": ModelsCachePath,
	} {
		if _, err := f(); err == nil {
			t.Errorf("%s path without HOME must fail", name)
		}
	}
	if _, err := LoadModelsCache(); err == nil {
		t.Error("load cache without HOME must fail")
	}
	if err := SaveModelsCache(ModelsCache{}); err == nil {
		t.Error("save cache without HOME must fail")
	}
}

func TestSaveModelsCacheUnwritable(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	// A file where the termocode folder goes.
	if err := os.WriteFile(filepath.Join(cache, "termocode"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveModelsCache(ModelsCache{}); err == nil {
		t.Error("save under a file must fail")
	}
}

func TestChatStoreErrors(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	c := NewConversation()
	c.Messages = []StoredMessage{{Role: StoredUser, Text: "hi"}}
	if err := (ChatStore{Dir: blocker}).Save(&c); err == nil {
		t.Error("save into a file must fail")
	}

	s := ChatStore{Dir: filepath.Join(dir, "chats")}
	if s.List() != nil {
		t.Error("a missing folder lists nothing")
	}
	if err := s.Save(&c); err != nil {
		t.Fatal(err)
	}
	// Junk next to the chats is skipped.
	for name, body := range map[string]string{"notes.txt": "x", "broken.json": "{", "noid.json": `{"title":"x"}`} {
		if err := os.WriteFile(filepath.Join(s.Dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(s.Dir, "folder.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := s.List(); len(got) != 1 || got[0].ID != c.ID {
		t.Errorf("list = %+v", got)
	}
	if _, err := s.Load("broken"); err == nil {
		t.Error("loading a corrupt chat must fail")
	}
	if err := s.Delete("not-there"); err != nil {
		t.Errorf("delete missing: %v", err)
	}
	// A save whose target is a folder fails at the rename.
	c2 := NewConversation()
	c2.ID = "folder"
	c2.Messages = c.Messages
	if err := s.Save(&c2); err == nil {
		t.Error("save over a folder must fail")
	}
}

// An id from outside (a session file, a link) cannot reach files outside
// the chat folder.
func TestChatStoreIDCannotEscape(t *testing.T) {
	root := t.TempDir()
	s := ChatStore{Dir: filepath.Join(root, "chats")}
	if err := os.WriteFile(filepath.Join(root, "secret.json"), []byte(`{"id":"x","title":"secret"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load("../secret"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("load escaped: %v", err)
	}
	if err := s.Delete("../secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "secret.json")); err != nil {
		t.Error("delete escaped the chat folder")
	}
}

func TestDeriveTitleSkipsAnswersAndBlankLines(t *testing.T) {
	msgs := []StoredMessage{
		{Role: StoredAssistant, Text: "Welcome"},
		{Role: StoredUser, Text: "   \n"},
		{Role: StoredUser, Text: "Real question"},
	}
	if got := DeriveTitle(msgs); got != "Real question" {
		t.Errorf("title = %q", got)
	}
}
