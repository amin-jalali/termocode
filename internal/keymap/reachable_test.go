package keymap

import (
	"sort"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/keytest"
)

// Spec (bug report 2026-10-15, "make sure all combination keys are and
// will be healthy"): every default shortcut can be typed. The bytes a
// terminal sends for it, read by the real input parser, reach exactly the
// shortcut's action — in the legacy encoding unless the key is listed in
// kittyOnly, and in the kitty encoding whenever that encoding can express
// the key.
func TestEveryDefaultKeyReachesItsAction(t *testing.T) {
	km := Default()
	keys := make([]string, 0)
	for k := range km.Bindings() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var legacyMissing []string
	for _, key := range keys {
		want := km.Bindings()[key]
		legacy, kitty := keytest.Encode(key)
		check := func(enc, b string) {
			t.Helper()
			msgs := tea.ParseInput([]byte(b))
			if len(msgs) != 1 {
				t.Errorf("%s (%s %q): parsed into %d messages %v", key, enc, b, len(msgs), msgs)
				return
			}
			km2, ok := msgs[0].(tea.KeyMsg)
			if !ok {
				t.Errorf("%s (%s %q): not a key: %T", key, enc, b, msgs[0])
				return
			}
			if got := km.Match(km2); got != want {
				t.Errorf("%s (%s %q): arrives as %q → action %v, want %v", key, enc, b, km2.String(), got, want)
			}
		}
		switch b := legacy; {
		case KittyOnlyKeys[key]:
			// Legacy terminals cannot send it (or send it as another key).
		case b != "":
			check("legacy", b)
		default:
			legacyMissing = append(legacyMissing, key)
		}
		if b := kitty; b != "" {
			check("kitty", b)
		}
	}
	if len(legacyMissing) > 0 {
		t.Errorf("no legacy terminal can send these default keys; add them to KittyOnlyKeys on purpose (and make sure the action is in the palette): %q", legacyMissing)
	}
}
