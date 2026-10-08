package keymap

import "testing"

func TestConflict(t *testing.T) {
	km := KeyMap{bindings: map[string]Action{
		"ctrl+s": ActionSave,
		"ctrl+x": ActionCut,
	}}
	if owner, ok := km.Conflict("ctrl+x", ActionSave); !ok || owner != ActionCut {
		t.Fatalf("ctrl+x for Save: got (%v, %v), want (Cut, true)", owner, ok)
	}
	if _, ok := km.Conflict("ctrl+s", ActionSave); ok {
		t.Fatal("rebinding an action to its own key must not conflict")
	}
	if _, ok := km.Conflict("f5", ActionSave); ok {
		t.Fatal("free key must not conflict")
	}
	if owner, ok := km.Conflict(" ctrl+x ", ActionSave); !ok || owner != ActionCut {
		t.Fatal("key should be trimmed before lookup")
	}
}
