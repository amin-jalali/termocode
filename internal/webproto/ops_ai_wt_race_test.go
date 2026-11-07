package webproto

import (
	"strings"
	"testing"
)

// Spec (MOB-62, mobile plan 3.9): starting a race from the phone is a
// laptop card (it makes worktrees and opens tabs); summarise, verify and
// stop are controller ops, audited, never for a viewer. Summarise spends
// tokens, so one runs at a time.
func TestAIRaceOps(t *testing.T) {
	p, ok := Lookup(OpAIRaceStart)
	if !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes {
		t.Fatalf("ai.race_start: %+v", p)
	}
	for _, op := range []string{OpAIRaceSummary, OpAIRaceVerify, OpAIRaceStop} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes {
			t.Errorf("%s: %+v", op, p)
		}
	}
	if p, _ := Lookup(OpAIRaceSummary); p.Live != 1 {
		t.Errorf("summary live %d", p.Live)
	}
	for _, op := range []string{OpAIRaceStart, OpAIRaceSummary, OpAIRaceVerify, OpAIRaceStop} {
		if Allowed(RoleViewer, op) {
			t.Errorf("%s: a viewer may call it", op)
		}
	}
}

// Spec (MOB-62): a race start names a task (≤ 2000), 2–4 agent ids and
// at most one verify line; a race is named "race-<n>". Nothing else.
func TestAIRaceArgs(t *testing.T) {
	good := map[string][]string{
		OpAIRaceStart: {
			`{"task":"fix the login bug","agents":["claude","codex"]}`,
			`{"task":"x","agents":["claude","claude","codex","gemini"],"verify":"go test ./..."}`,
		},
		OpAIRaceSummary: {`{"race":"race-1"}`, `{"race":"race-204"}`},
		OpAIRaceVerify:  {`{"race":"race-3"}`},
		OpAIRaceStop:    {`{"race":"race-3"}`},
	}
	bad := map[string][]string{
		OpAIRaceStart: {
			`{"task":"  ","agents":["claude","codex"]}`,
			`{"task":"x","agents":["claude"]}`,
			`{"task":"x","agents":["a","b","c","d","e"]}`,
			`{"task":"x","agents":["Claude","codex"]}`,
			`{"task":"x","agents":["-rm","codex"]}`,
			`{"task":"x","agents":["../bin","codex"]}`,
			`{"task":"x","agents":["claude","codex"],"verify":"a\nb"}`,
			`{"task":"x","agents":["claude","codex"],"verify":"` + strings.Repeat("a", MaxAIRaceVerify+1) + `"}`,
			`{"task":"` + strings.Repeat("a", MaxAIRaceTask+1) + `","agents":["claude","codex"]}`,
			`{"task":"x","agents":["claude","codex"],"lanes":2}`,
		},
		OpAIRaceSummary: {`{"race":""}`, `{"race":"race-"}`, `{"race":"race-01"}`, `{"race":"race-1x"}`, `{"race":"../race-1"}`, `{"race":"race-1","lane":"a"}`},
		OpAIRaceStop:    {`{}`, `{"race":"run-1"}`},
	}
	for op, bs := range good {
		for _, b := range bs {
			if _, err := DecodeArgs(op, []byte(b)); err != nil {
				t.Errorf("%s %s: %v", op, b, err)
			}
		}
	}
	for op, bs := range bad {
		for _, b := range bs {
			if _, err := DecodeArgs(op, []byte(b)); err == nil {
				t.Errorf("%s %.80s accepted", op, b)
			}
		}
	}
}
