package ai

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// slotCfg writes a config.json with a user's own keys and one slot that
// has a field termocode does not know.
func slotCfg(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{
  "ai_models": {
    "gpt": {
      "model": "gpt-5-mini",
      "note": "mine",
      "provider": "openai"
    },
    "other": {
      "provider": "ollama"
    }
  },
  "ai_provider": "anthropic",
  "theme": {
    "name": "dark",
    "size": 3
  }
}
`
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// compactKeys decodes path into its top-level keys, compacted.
func compactKeys(t *testing.T, path string) map[string]string {
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
		if err := json.Compact(&c, v); err != nil {
			t.Fatal(err)
		}
		out[k] = c.String()
	}
	return out
}

func slotsOf(t *testing.T, path string) map[string]map[string]any {
	t.Helper()
	b, _ := os.ReadFile(path)
	var cfg struct {
		Models map[string]map[string]any `json:"ai_models"`
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg.Models
}

// Spec: adding a slot writes ai_models.<name>; every other key and slot is
// byte-equal and the file parses with ResolveSlots.
func TestSaveModelSlotAdd(t *testing.T) {
	path := slotCfg(t)
	before := compactKeys(t, path)
	if err := SaveModelSlot(path, "work", ModelSlot{Provider: " OpenRouter ", Model: "x/y", BaseURL: ""}); err != nil {
		t.Fatal(err)
	}
	after := compactKeys(t, path)
	for k, v := range before {
		if k != "ai_models" && after[k] != v {
			t.Errorf("key %s changed: %s -> %s", k, v, after[k])
		}
	}
	m := slotsOf(t, path)
	if m["work"]["provider"] != "openrouter" || m["work"]["model"] != "x/y" {
		t.Errorf("work = %v", m["work"])
	}
	if _, ok := m["work"]["base_url"]; ok {
		t.Error("an empty base_url was written")
	}
	if len(m["gpt"]) != 3 || m["gpt"]["note"] != "mine" || len(m["other"]) != 1 {
		t.Errorf("the other slots changed: %v", m)
	}
	s := LoadSettings(path)
	cfgs, errs := ResolveSlots(s, Config{Kind: KindAnthropic}, Credentials{}, func(string) string { return "" })
	if len(errs) > 0 || cfgs["work"].Kind != KindOpenRouter {
		t.Errorf("ResolveSlots: %v %+v", errs, cfgs["work"])
	}
}

// Spec: a canonical file only changes at the new slot.
func TestSaveModelSlotByteEqualRest(t *testing.T) {
	path := slotCfg(t)
	old, _ := os.ReadFile(path)
	if err := SaveModelSlot(path, "gpt", ModelSlot{Provider: "openai", Model: "gpt-5-mini"}); err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(path)
	if !bytes.Equal(old, now) {
		t.Errorf("saving the same slot changed the file:\n%s\n---\n%s", old, now)
	}
}

// Spec: Edit keeps the unknown fields of that slot.
func TestSaveModelSlotEditKeepsUnknown(t *testing.T) {
	path := slotCfg(t)
	if err := SaveModelSlot(path, "gpt", ModelSlot{Provider: "openai-compatible", Model: "", BaseURL: "http://10.0.0.2:8000/v1"}); err != nil {
		t.Fatal(err)
	}
	g := slotsOf(t, path)["gpt"]
	if g["note"] != "mine" || g["provider"] != "openai-compatible" || g["base_url"] != "http://10.0.0.2:8000/v1" {
		t.Errorf("gpt = %v", g)
	}
	if _, ok := g["model"]; ok {
		t.Error("an empty model stays")
	}
}

// Spec: a bad slot is not written: the file stays byte-equal.
func TestSaveModelSlotInvalid(t *testing.T) {
	cases := []struct {
		name string
		sl   ModelSlot
		want string
	}{
		{"main", ModelSlot{Provider: "openai"}, "built in"},
		{"bad name", ModelSlot{Provider: "openai"}, "slot name"},
		{"-x", ModelSlot{Provider: "openai"}, "slot name"},
		{"ok", ModelSlot{Provider: "gemini"}, "unknown provider"},
		{"ok", ModelSlot{Provider: "openai-compatible"}, "needs base_url"},
		{"ok", ModelSlot{Provider: "openai", BaseURL: "ftp://x"}, "http"},
		{"ok", ModelSlot{Provider: "openai", BaseURL: "localhost:8000"}, "http"},
		{"ok", ModelSlot{Provider: "openai", BaseURL: "https://me:s3cr3tpass@host/v1"}, "user or password"},
	}
	for _, c := range cases {
		path := slotCfg(t)
		old, _ := os.ReadFile(path)
		err := SaveModelSlot(path, c.name, c.sl)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%q %+v: err %v, want %q", c.name, c.sl, err, c.want)
		}
		if err != nil && strings.Contains(err.Error(), "s3cr3tpass") {
			t.Error("the error repeats the password")
		}
		if now, _ := os.ReadFile(path); !bytes.Equal(old, now) {
			t.Errorf("%q: the file changed", c.name)
		}
	}
}

// Spec: a broken config.json is never overwritten.
func TestSaveModelSlotBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(path, []byte(`{"ai_models": [1]}`), 0o644)
	if err := SaveModelSlot(path, "x", ModelSlot{Provider: "openai"}); err == nil {
		t.Error("an ai_models that is not an object was overwritten")
	}
	if b, _ := os.ReadFile(path); string(b) != `{"ai_models": [1]}` {
		t.Errorf("file changed: %s", b)
	}
}

// Spec: Remove deletes only that slot; the last one takes ai_models along.
func TestRemoveModelSlot(t *testing.T) {
	path := slotCfg(t)
	before := compactKeys(t, path)
	if err := RemoveModelSlot(path, "gpt"); err != nil {
		t.Fatal(err)
	}
	m := slotsOf(t, path)
	if _, ok := m["gpt"]; ok || len(m["other"]) != 1 {
		t.Errorf("slots %v", m)
	}
	after := compactKeys(t, path)
	for k, v := range before {
		if k != "ai_models" && after[k] != v {
			t.Errorf("key %s changed", k)
		}
	}
	if err := RemoveModelSlot(path, "gpt"); err == nil {
		t.Error("removing a missing slot is no error")
	}
	if err := RemoveModelSlot(path, "other"); err != nil {
		t.Fatal(err)
	}
	if _, ok := compactKeys(t, path)["ai_models"]; ok {
		t.Error("an empty ai_models stays")
	}
}
