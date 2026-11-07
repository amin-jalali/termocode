package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (MOB-54): board.projects is a read for any viewer, never a laptop
// card, never audited, and it takes no args (the phone cannot name a
// folder); a project's state comes from its counts, waiting first.
func TestBoardProjectsRow(t *testing.T) {
	p, ok := Lookup(OpBoardProjects)
	if !ok || p.Role != RoleViewer || p.LaptopCard || p.Audit != AuditNo || p.Phase != PhaseV2 {
		t.Fatalf("row %+v", p)
	}
	if _, err := DecodeArgs(OpBoardProjects, json.RawMessage(`{}`)); err != nil {
		t.Errorf("empty args: %v", err)
	}
	for _, a := range []string{`{"ws":"0123456789abcdef"}`, `{"path":"/home/x"}`} {
		if _, err := DecodeArgs(OpBoardProjects, json.RawMessage(a)); err == nil {
			t.Errorf("%s: accepted", a)
		}
	}
	for _, c := range []struct {
		working, waiting int
		want             string
	}{{0, 0, BoardIdle}, {2, 0, BoardWorking}, {2, 1, BoardNeeds}, {0, 1, BoardNeeds}} {
		if got := BoardState(c.working, c.waiting); got != c.want {
			t.Errorf("BoardState(%d, %d) = %q, want %q", c.working, c.waiting, got, c.want)
		}
	}
}
