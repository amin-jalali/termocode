package webproto

import (
	"strings"
	"testing"
)

// Spec (web palette): the page lists the laptop palette's commands that
// are safe from the web (a viewer may read the list); running one is a
// controller op; one that is risky runs only after a laptop card. A
// command is named only by its palette id: lowercase letters, digits and
// dashes, never a path, an option or free text.
func TestPaletteOps(t *testing.T) {
	if p, ok := Lookup(OpPaletteList); !ok || p.Role != RoleViewer || p.LaptopCard {
		t.Errorf("palette.list: %+v", p)
	}
	if p, ok := Lookup(OpPaletteRun); !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes {
		t.Errorf("palette.run: %+v", p)
	}
	if p, ok := Lookup(OpPaletteAsk); !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes {
		t.Errorf("palette.ask: %+v", p)
	}
	for _, op := range []string{OpPaletteRun, OpPaletteAsk} {
		for _, b := range []string{`{"id":"save-all"}`, `{"id":"git-push"}`, `{"id":"run-tests"}`} {
			if _, err := DecodeArgs(op, []byte(b)); err != nil {
				t.Errorf("%s %s: %v", op, b, err)
			}
		}
		for _, b := range []string{`{"id":""}`, `{"id":"Save"}`, `{"id":"a b"}`, `{"id":"../x"}`, `{"id":"--force"}`, `{"id":"x\u0000"}`, `{"id":"` + strings.Repeat("a", 65) + `"}`} {
			if _, err := DecodeArgs(op, []byte(b)); err == nil {
				t.Errorf("%s %q accepted", op, b)
			}
		}
	}
}
