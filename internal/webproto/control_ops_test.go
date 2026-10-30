package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEB-34): asking for control is the gateway's own business (it
// holds the passkeys): a viewer may call both ops, they are marked
// Gateway so the hub refuses them, and the answer of the challenge is a
// typed result the page can read.
func TestControlOps(t *testing.T) {
	for _, op := range []string{OpControlChallenge, OpControlAsk} {
		p, ok := Lookup(op)
		if !ok || !p.Gateway || p.Role != RoleViewer || p.Audit != AuditYes {
			t.Errorf("%s: row %+v", op, p)
		}
	}
	for _, p := range Ops() {
		if p.Gateway && p.Op != OpControlChallenge && p.Op != OpControlAsk {
			t.Errorf("%s is answered by the gateway", p.Op)
		}
	}
	if _, ok := resultData[OpControlChallenge]; !ok {
		t.Error("control.challenge has no result type")
	}
	if _, err := DecodeArgs(OpControlAsk, json.RawMessage(`{"credential":"{\"id\":\"x\"}"}`)); err != nil {
		t.Errorf("control.ask: %v", err)
	}
	for _, bad := range []string{`{}`, `{"credential":"x"}`, `{"credential":"{not json"}`, `{"credential":{"id":"x"}}`} {
		if _, err := DecodeArgs(OpControlAsk, json.RawMessage(bad)); err == nil {
			t.Errorf("control.ask %s decoded", bad)
		}
	}
}

// Spec (WEB-34): 4013 tells the page its role changed; it reconnects at
// once and gets the new role in meta.
func TestCloseRoleChanged(t *testing.T) {
	if CloseRoleChanged != 4013 || CloseRoleChanged.Reason() == "" {
		t.Fatalf("4013 %q", CloseRoleChanged.Reason())
	}
}
