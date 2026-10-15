package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/keymap"
)

// Spec (2026-10-15, "all combination keys healthy"): every action whose
// shortcut only kitty-protocol terminals can send is still reachable on
// other terminals: through another default key, or the command palette.
func TestKittyOnlyActionsReachableElsewhere(t *testing.T) {
	b := keymap.Default().Bindings()
	inPalette := map[keymap.Action]bool{}
	for _, a := range paletteActions {
		inPalette[a] = true
	}
	for key := range keymap.KittyOnlyKeys {
		a, ok := b[key]
		if !ok {
			t.Errorf("%q is listed as kitty-only but is not a default binding", key)
			continue
		}
		other := false
		for k2, a2 := range b {
			if a2 == a && !keymap.KittyOnlyKeys[k2] {
				other = true
			}
		}
		if !other && !inPalette[a] {
			t.Errorf("%q (%v): no other key and not in the palette — unreachable on legacy terminals", key, a)
		}
	}
}
