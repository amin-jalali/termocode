package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEBP-9, Editor and files): search.replace replaces a text in
// every workspace file only after a laptop card that counts the matches
// and files.
func TestSearchReplaceOp(t *testing.T) {
	p, ok := Lookup(OpSearchReplace)
	if !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 {
		t.Errorf("row %+v", p)
	}
	if _, err := DecodeArgs(OpSearchReplace, json.RawMessage(`{"find":"oldName","replace":"newName"}`)); err != nil {
		t.Error(err)
	}
	if _, err := DecodeArgs(OpSearchReplace, json.RawMessage(`{"find":"x","replace":""}`)); err != nil {
		t.Errorf("an empty replacement: %v", err)
	}
	for _, bad := range []string{`{"find":"","replace":"y"}`, `{"find":"a\u0000","replace":"y"}`} {
		if _, err := DecodeArgs(OpSearchReplace, json.RawMessage(bad)); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}
