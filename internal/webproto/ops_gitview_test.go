package webproto

import (
	"encoding/json"
	"errors"
	"testing"
)

// checkRow wants op's table row to have the role, the card and the audit.
func checkRow(t *testing.T, op string, role Role, card bool, audit Audit) {
	t.Helper()
	p, ok := Lookup(op)
	if !ok || p.Role != role || p.Phase != PhaseV2 || p.Audit != audit || p.LaptopCard != card {
		t.Errorf("%s row %+v", op, p)
	}
	if _, ok := ResultOf(op); !ok {
		t.Errorf("%s has no result type", op)
	}
}

// checkArgs wants each bad args JSON refused and each good one decoded.
func checkArgs(t *testing.T, op string, bad, good []string) {
	t.Helper()
	for _, b := range bad {
		if _, err := DecodeArgs(op, json.RawMessage(b)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s %s: %v, want invalid", op, b, err)
		}
	}
	for _, g := range good {
		if _, err := DecodeArgs(op, json.RawMessage(g)); err != nil {
			t.Errorf("%s %s: %v", op, g, err)
		}
	}
}

// Spec (web Git Graph): git.graph reads one page of the commit graph (all
// branches, or HEAD's history only) with its lanes; a v2 read for
// viewers, not audited. The page starts at most MaxGraphSkip commits in.
func TestGitGraphOp(t *testing.T) {
	checkArgs(t, OpGitGraph, []string{`{"skip":-1}`, `{"skip":5001}`},
		[]string{`{}`, `{"skip":200}`, `{"skip":0,"head":true}`})
	checkRow(t, OpGitGraph, RoleViewer, false, AuditNo)
}
