package webproto

import (
	"strings"
	"testing"
)

// Spec (MOB-61, mobile plan §3.9): the races topic (v2) lists the races
// with their lanes for the phone. It is read only (no op of its own) and
// its rows carry no lane folder, no file text and no verify command line
// (only whether one is set).
func TestRacesTopic(t *testing.T) {
	tp, err := ParseTopic("races")
	if err != nil || tp.Name != "races" {
		t.Fatalf("races: %v %+v", err, tp)
	}
	if d, ok := topicDef("races"); !ok || d.Phase != PhaseV2 || d.Keyed {
		t.Fatalf("races def %+v", d)
	}
	for _, p := range Ops() {
		if strings.HasPrefix(p.Op, "races.") {
			t.Errorf("the races topic is read only, but %s is an op", p.Op)
		}
	}
	ts, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"races": { snap: RacesSnap; delta: RacesSnap };`, `| "races";`,
		`state: "starting" | "working" | "waiting" | "done" | "stopped" | "failed" | "merged" | "discarded";`,
		`verify?: "running" | "pass" | "fail";`, `state: "none" | "loading" | "error" | "ready";`} {
		if !strings.Contains(string(ts), want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
	block := tsBlocks(string(ts), "RacesSnap", "RaceItem", "RaceLane", "RaceFile")
	for _, field := range []string{"worktree", "dir", "body", "text", "content", "command", "cmd", "env"} {
		if strings.Contains(block, "  "+field+":") || strings.Contains(block, "  "+field+"?:") {
			t.Errorf("the race rows carry %s", field)
		}
	}
	if !strings.Contains(block, "  verify?: boolean;") {
		t.Error("a race says only whether a verify command is set")
	}
}

// tsBlocks is the TypeScript of the named interfaces.
func tsBlocks(ts string, names ...string) string {
	var b strings.Builder
	for _, name := range names {
		i := strings.Index(ts, "export interface "+name+" {")
		if i < 0 {
			continue
		}
		j := strings.Index(ts[i:], "\n}")
		b.WriteString(ts[i : i+j])
	}
	return b.String()
}
