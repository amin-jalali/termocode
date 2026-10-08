package keymap

import (
	"encoding/json"
	"os"
	"strings"
)

// ExtPrefix marks a keymap.json value that runs an extension command
// instead of a built-in Action (Group I):
//
//	{ "alt+h": "ext:hello.say" }
//
// LoadOverrides ignores these entries (they are not Action names), so the
// two loaders never fight over a key.
const ExtPrefix = "ext:"

// LoadExtBindings reads keymap.json and returns key string → extension
// command id for every "ext:<id>" value. Unlike Action overrides, any
// non-empty key string is accepted: the key is matched against the raw
// Bubble Tea key name, so it does not need a default binding. Missing or
// broken files yield nil (config never breaks startup, ADR 0005). Pass an
// empty path for the default OverridesPath().
func LoadExtBindings(path string) map[string]string {
	if path == "" {
		p, err := OverridesPath()
		if err != nil {
			return nil
		}
		path = p
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw overrideFile
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil
	}
	var out map[string]string
	for key, val := range raw {
		key = strings.TrimSpace(key)
		if key == "" || !strings.HasPrefix(val, ExtPrefix) {
			continue
		}
		id := strings.TrimSpace(strings.TrimPrefix(val, ExtPrefix))
		if id == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		out[key] = id
	}
	return out
}
