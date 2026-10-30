package webproto

import (
	"encoding/json"
	"testing"
)

// Spec: files.list takes no arguments, is a v2 read for viewers that is
// not audited, and has a result type.
func TestFilesListRow(t *testing.T) {
	if _, err := DecodeArgs(OpFilesList, json.RawMessage(`{}`)); err != nil {
		t.Errorf("{}: %v", err)
	}
	p, ok := Lookup(OpFilesList)
	if !ok || p.Role != RoleViewer || p.Phase != PhaseV2 || p.Audit != AuditNo {
		t.Errorf("files.list row %+v: a v2 read for viewers, not audited", p)
	}
	if _, ok := ResultOf(OpFilesList); !ok {
		t.Error("files.list has no result type")
	}
}
