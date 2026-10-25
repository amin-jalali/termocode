package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/amin-jalali/termocode/internal/cfgfile"
)

// ── Writing model slots (config.json "ai_models") ─────────────────────
//
// The Models section of the Itermoc view adds, edits and removes slots
// through these helpers: a read-modify-write of config.json that keeps
// every other key, every other slot and the unknown fields of the slot it
// changes. Keys are never written here: they live in the secret store.

// CheckModelSlot reports why a slot can not be used, with the rules
// ResolveSlots applies (nil = fine). The provider is compared lower-case.
func CheckModelSlot(name string, sl ModelSlot) error {
	kind := strings.ToLower(strings.TrimSpace(sl.Provider))
	switch {
	case name == SlotMain:
		return errors.New(`"main" is built in (it is ai_provider / ai_model)`)
	case !profileIDRe.MatchString(name):
		return errors.New("a slot name may use letters, digits, '-', '_' and '.'")
	case !knownKind(kind):
		return fmt.Errorf("unknown provider %q (%s)", sl.Provider, strings.Join(Kinds, ", "))
	case kind == KindCompatible && strings.TrimSpace(sl.BaseURL) == "":
		return errors.New("openai-compatible needs base_url")
	}
	return nil
}

// CheckSlotBaseURL reports why a base URL typed into the slot form is not
// accepted ("" is fine: the provider's default). It must be http(s) with a
// host and carry no user or password: a key belongs in the secret store.
// The error never repeats the URL.
func CheckSlotBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("base_url must start with http:// or https:// and name a host")
	}
	if u.User != nil {
		return errors.New("base_url must not hold a user or password; set the key with Set key")
	}
	return nil
}

// SaveModelSlot writes ai_models.<name> in config.json at path: provider,
// model and base_url are set (an empty model or base_url is removed), the
// slot's other fields stay, and so do every other slot and key. A slot
// that fails CheckModelSlot or CheckSlotBaseURL is not written.
func SaveModelSlot(path, name string, sl ModelSlot) error {
	sl.Provider = strings.ToLower(strings.TrimSpace(sl.Provider))
	sl.Model = strings.TrimSpace(sl.Model)
	sl.BaseURL = strings.TrimSpace(sl.BaseURL)
	if err := CheckModelSlot(name, sl); err != nil {
		return err
	}
	if err := CheckSlotBaseURL(sl.BaseURL); err != nil {
		return err
	}
	return cfgfile.UpdateRaw(path, func(cfg map[string]json.RawMessage) error {
		models, err := rawObject(cfg["ai_models"], "ai_models")
		if err != nil {
			return err
		}
		slot, err := rawObject(models[name], "ai_models."+name)
		if err != nil {
			return err
		}
		set := func(k, v string) {
			if v == "" {
				delete(slot, k)
				return
			}
			b, _ := json.Marshal(v)
			slot[k] = b
		}
		set("provider", sl.Provider)
		set("model", sl.Model)
		set("base_url", sl.BaseURL)
		b, err := json.Marshal(slot)
		if err != nil {
			return err
		}
		models[name] = b
		mb, err := json.Marshal(models)
		if err != nil {
			return err
		}
		cfg["ai_models"] = mb
		return nil
	})
}

// RemoveModelSlot deletes ai_models.<name> from config.json at path;
// everything else stays. An empty ai_models is removed too. A slot that
// is not there is an error (nothing is written).
func RemoveModelSlot(path, name string) error {
	return cfgfile.UpdateRaw(path, func(cfg map[string]json.RawMessage) error {
		models, err := rawObject(cfg["ai_models"], "ai_models")
		if err != nil {
			return err
		}
		if _, ok := models[name]; !ok {
			return fmt.Errorf("ai_models has no slot %q", name)
		}
		delete(models, name)
		if len(models) == 0 {
			delete(cfg, "ai_models")
			return nil
		}
		mb, err := json.Marshal(models)
		if err != nil {
			return err
		}
		cfg["ai_models"] = mb
		return nil
	})
}

// rawObject decodes a JSON object kept raw (missing or null = empty).
func rawObject(b json.RawMessage, what string) (map[string]json.RawMessage, error) {
	out := map[string]json.RawMessage{}
	if len(b) == 0 || string(b) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object; nothing was changed", what)
	}
	if out == nil {
		out = map[string]json.RawMessage{}
	}
	return out, nil
}
