package keymap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FuzzKeyString checks the key-string helpers on any text: no panic, a
// valid key keeps a valid canonical form, and canonical forms are stable.
func FuzzKeyString(f *testing.F) {
	for _, s := range []string{
		"", "a", "A", "ctrl+a", "alt+T", "ctrl++", "+", "ctrl+shift+f13",
		"alt+f24", "f1", "f05", "f013", "shift+enter", "ctrl+ctrl+a",
		" ctrl+a", "ctrl+", "ctrl+_", "pgdown", "ctrl+alt+shift+é", "a+b",
		"f25", "f-1", "+++",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_ = HumanKey(s)
		c := CanonicalKey(s)
		if CanonicalKey(c) != c {
			t.Fatalf("CanonicalKey not stable: %q -> %q -> %q", s, c, CanonicalKey(c))
		}
		if ValidateKey(s) != nil {
			return
		}
		if !IsKnownKey(s) {
			t.Fatalf("ValidateKey accepts %q but IsKnownKey does not", s)
		}
		if err := ValidateKey(c); err != nil {
			t.Fatalf("valid key %q has invalid canonical form %q: %v", s, c, err)
		}
		if HumanKey(s) == "" {
			t.Fatalf("HumanKey(%q) is empty", s)
		}
	})
}

// FuzzLoadOverrides feeds any bytes as keymap.json: loading never
// panics, every loaded key is valid, and a file with problems reports
// them instead of silently dropping entries.
func FuzzLoadOverrides(f *testing.F) {
	for _, s := range []string{
		``,
		`{}`,
		`{"ctrl+s":"Save"}`,
		`{"ctrl+s":"Save","bad key":"Quit","ctrl+q":"NoSuchAction"}`,
		`{"alt+x":"ext:demo.run"}`,
		`[1,2]`,
		`{"ctrl+s":1}`,
		`not json`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		path := filepath.Join(t.TempDir(), "keymap.json")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		out, problems := LoadOverridesReport(path)
		_ = LoadExtBindings(path)
		var raw overrideFile
		if err := json.Unmarshal(data, &raw); err != nil {
			if out != nil || len(problems) == 0 {
				t.Fatalf("bad JSON accepted silently: out=%v problems=%v", out, problems)
			}
			return
		}
		for k, a := range out {
			if err := ValidateKey(k); err != nil {
				t.Fatalf("loaded invalid key %q: %v", k, err)
			}
			if a == ActionNone {
				t.Fatalf("loaded ActionNone for %q", k)
			}
		}
		// Two raw keys may trim to the same key, so only fewer results
		// than distinct trimmed keys can be a silent drop.
		distinct := map[string]bool{}
		for k, v := range raw {
			if !strings.HasPrefix(v, ExtPrefix) {
				distinct[strings.TrimSpace(k)] = true
			}
		}
		if len(out)+len(problems) < len(distinct) {
			t.Fatalf("entries dropped without a problem: raw=%v out=%v problems=%v", raw, out, problems)
		}
	})
}

// FuzzSaveOverridesRoundTrip checks that a valid key saved with an action
// loads back as the same binding.
func FuzzSaveOverridesRoundTrip(f *testing.F) {
	acts := AllActions()
	for _, s := range []string{"ctrl+s", "alt+T", "ctrl++", "f13", "é", "shift+enter"} {
		f.Add(s, uint16(0))
	}
	f.Fuzz(func(t *testing.T, key string, ai uint16) {
		if ValidateKey(key) != nil {
			return
		}
		act := acts[int(ai)%len(acts)]
		path := filepath.Join(t.TempDir(), "keymap.json")
		if err := SaveOverrides(path, map[string]Action{key: act}); err != nil {
			t.Fatal(err)
		}
		out, problems := LoadOverridesReport(path)
		if len(problems) != 0 || out[key] != act || len(out) != 1 {
			t.Fatalf("round trip of %q=%s gave %v, problems %v", key, ActionName(act), out, problems)
		}
	})
}
