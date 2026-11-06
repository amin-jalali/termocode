package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEBP-9, Editor and files): file.text reads a workspace file as it
// is on disk (File: Compare Two Files…); viewers may read it.
func TestFileTextOp(t *testing.T) {
	p, ok := Lookup(OpFileText)
	if !ok || p.Role != RoleViewer || p.Audit != AuditNo || p.LaptopCard || p.Phase != PhaseV2 {
		t.Errorf("row %+v", p)
	}
	if _, err := DecodeArgs(OpFileText, json.RawMessage(`{"path":"pkg/a.go"}`)); err != nil {
		t.Error(err)
	}
	if _, err := DecodeArgs(OpFileText, json.RawMessage(`{"path":"../x"}`)); err == nil {
		t.Error("a path out of the workspace is accepted")
	}
	if _, ok := ResultOf(OpFileText); !ok {
		t.Error("no result type")
	}
}
