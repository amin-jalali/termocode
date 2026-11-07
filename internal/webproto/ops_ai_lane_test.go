package webproto

import (
	"strings"
	"testing"
)

// Spec (MOB-65, mobile plan 3.9): ai.lane_start makes a worktree, a
// branch and an agent process on the laptop, so it is a controller op
// behind a laptop card, audited and slow; ai.lane_agents is a viewer
// read. The args are an agent id, a task (no control characters: it is
// typed into a terminal) and an optional start point git takes as a ref.
func TestAILaneOps(t *testing.T) {
	p, ok := Lookup(OpAILaneStart)
	if !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes {
		t.Fatalf("ai.lane_start: %+v", p)
	}
	if Allowed(RoleViewer, OpAILaneStart) {
		t.Error("a viewer may start a lane")
	}
	if p, ok := Lookup(OpAILaneAgents); !ok || p.Role != RoleViewer || p.LaptopCard {
		t.Errorf("ai.lane_agents: %+v", p)
	}
	for _, b := range []string{
		`{"agent":"claude","task":"fix the login bug"}`,
		`{"agent":"codex","task":"fix it\nthen test it","from":"main"}`,
		`{"agent":"gemini","task":"x","from":"origin/feat/a-1"}`,
	} {
		if _, err := DecodeArgs(OpAILaneStart, []byte(b)); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	for _, b := range []string{
		`{"agent":"","task":"x"}`,
		`{"agent":"-rm","task":"x"}`,
		`{"agent":"Claude","task":"x"}`,
		`{"agent":"../x","task":"x"}`,
		`{"agent":"claude","task":"   "}`,
		`{"agent":"claude","task":"a\u001b[2Jb"}`,
		`{"agent":"claude","task":"a\rb"}`,
		`{"agent":"claude","task":"` + strings.Repeat("a", MaxAILaneTask+1) + `"}`,
		`{"agent":"claude","task":"x","from":"-b"}`,
		`{"agent":"claude","task":"x","from":"a..b"}`,
		`{"agent":"claude","task":"x","from":"a b"}`,
		`{"agent":"claude","task":"x","path":"/etc"}`,
	} {
		if _, err := DecodeArgs(OpAILaneStart, []byte(b)); err == nil {
			t.Errorf("%s accepted", b)
		}
	}
}
