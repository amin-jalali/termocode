package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (MOB-64, mobile plan §3.9): ai.run_start asks the laptop for a
// coordinator run; it is a controller op behind a laptop card that
// answers with the card's id. runs.send hands a running step a message;
// a controller op, audited. Both refuse empty, too long or unknown
// args, and a run lists the steps that can get a message.
func TestCoordinatorRows(t *testing.T) {
	p, ok := Lookup(OpAIRunStart)
	if !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 || p.Rate.N != 1 {
		t.Fatalf("ai.run_start row %+v", p)
	}
	if res, ok := ResultOf(OpAIRunStart); !ok || res.Name() != "AskRef" {
		t.Fatalf("ai.run_start result %v %v", res, ok)
	}
	p, ok = Lookup(OpRunsSend)
	if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes || p.Phase != PhaseV2 {
		t.Fatalf("runs.send row %+v", p)
	}

	long := strings.Repeat("é", MaxRunTask+1)
	for _, a := range []string{
		`{}`, `{"task":""}`, `{"task":"   "}`, `{"task":"` + long + `"}`,
		`{"task":"x","profile":"a\nb"}`, `{"task":"x","ref":"` + strings.Repeat("r", MaxRunTaskRef+1) + `"}`,
		`{"task":"x","cmd":"rm -rf /"}`,
	} {
		if _, err := DecodeArgs(OpAIRunStart, json.RawMessage(a)); err == nil {
			t.Errorf("run_start %.60s: accepted", a)
		}
	}
	for _, a := range []string{`{"task":"Split the login work\nand test it"}`, `{"task":"x","profile":"until-done","ref":"TASKS.md#API-7"}`} {
		if _, err := DecodeArgs(OpAIRunStart, json.RawMessage(a)); err != nil {
			t.Errorf("run_start %s: %v", a, err)
		}
	}

	for _, a := range []string{
		`{"key":"","text":"x"}`, `{"key":"c/r","text":""}`, `{"key":"c/r","text":" \n "}`,
		`{"key":"c/r","text":"` + strings.Repeat("a", MaxRunSend+1) + `"}`, `{"key":"c/r","step":"a\tb","text":"x"}`,
		`{"key":"c/r","text":"x","to":"all"}`,
	} {
		if _, err := DecodeArgs(OpRunsSend, json.RawMessage(a)); err == nil {
			t.Errorf("runs.send %.60s: accepted", a)
		}
	}
	if _, err := DecodeArgs(OpRunsSend, json.RawMessage(`{"key":"chat-1/run-2","step":"map","text":"Skip the vendor folder."}`)); err != nil {
		t.Errorf("runs.send: %v", err)
	}

	ts, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"ai.run_start": AIRunStartArgs;`, `"runs.send": RunsSendArgs;`, `"ai.run_start": AskRef;`, "running?: RunStepRef[];"} {
		if !strings.Contains(string(ts), want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
}
