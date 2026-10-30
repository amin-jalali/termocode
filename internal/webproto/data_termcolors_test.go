package webproto

import (
	"encoding/json"
	"errors"
	"testing"
)

// Spec (WEB-40): term.colors takes a terminal buffer; a v2 read for
// viewers, not audited, with a result type.
func TestTermColorsOp(t *testing.T) {
	if _, err := DecodeArgs(OpTermColors, json.RawMessage(`{"buf":0}`)); !errors.Is(err, ErrInvalid) {
		t.Errorf("buf 0: %v", err)
	}
	if _, err := DecodeArgs(OpTermColors, json.RawMessage(`{"buf":4}`)); err != nil {
		t.Error(err)
	}
	p, _ := Lookup(OpTermColors)
	if p.Role != RoleViewer || p.Phase != PhaseV2 || p.Audit != AuditNo {
		t.Errorf("term.colors row %+v", p)
	}
	if _, ok := ResultOf(OpTermColors); !ok {
		t.Error("term.colors has no result type")
	}
}
