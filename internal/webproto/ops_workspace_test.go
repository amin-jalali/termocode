package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-9, Editor and files): workspace.list names the workspace's
// folders and the laptop's recent workspaces (names only, never a path
// outside the workspace); adding or removing a folder and opening
// another workspace wait for a laptop card (opening one ends this web
// session, the laptop restarts there).
func TestWorkspaceOps(t *testing.T) {
	p, ok := Lookup(OpWorkspaceList)
	if !ok || p.Role != RoleViewer || p.LaptopCard || p.Phase != PhaseV2 {
		t.Errorf("list row %+v", p)
	}
	for _, op := range []string{OpWorkspaceAddFolder, OpWorkspaceRemoveFolder, OpWorkspaceOpen} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v: a laptop card", op, p)
		}
	}
	if _, err := DecodeArgs(OpWorkspaceAddFolder, json.RawMessage(`{"path":"/home/me/lib"}`)); err != nil {
		t.Error(err)
	}
	for _, bad := range []string{`{"path":""}`, `{"path":"rel/dir"}`, `{"path":"/a\u0000"}`, `{"path":"/` + strings.Repeat("x", 5000) + `"}`} {
		if _, err := DecodeArgs(OpWorkspaceAddFolder, json.RawMessage(bad)); err == nil {
			t.Errorf("add %s: accepted", bad)
		}
	}
	for _, op := range []string{OpWorkspaceRemoveFolder, OpWorkspaceOpen} {
		if _, err := DecodeArgs(op, json.RawMessage(`{"id":2}`)); err != nil {
			t.Errorf("%s: %v", op, err)
		}
		if _, err := DecodeArgs(op, json.RawMessage(`{"id":-1}`)); err == nil {
			t.Errorf("%s: a negative id is accepted", op)
		}
	}
}
