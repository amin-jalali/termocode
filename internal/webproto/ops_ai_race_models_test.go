package webproto

import (
	"strings"
	"testing"
)

// Spec (MOB-63, mobile plan 3.9): Compare models from the phone starts a
// race, so it is a controller op behind a laptop card, audited; the
// model list starts nothing and a viewer may read it.
func TestAIRaceModelsOps(t *testing.T) {
	p, ok := Lookup(OpAIRaceModels)
	if !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes || Allowed(RoleViewer, OpAIRaceModels) {
		t.Fatalf("ai.race_models: %+v", p)
	}
	p, ok = Lookup(OpAIRaceModelList)
	if !ok || p.LaptopCard || !Allowed(RoleViewer, OpAIRaceModelList) {
		t.Fatalf("ai.race_model_list: %+v", p)
	}
}

// Spec (MOB-63): a compare names a task (≤ 2000), one agent id, 2–4
// different model ids that are safe after --model, and at most one
// verify line. Nothing else.
func TestAIRaceModelsArgs(t *testing.T) {
	good := []string{
		`{"task":"fix the login bug","agent":"claude","models":["opus","sonnet"]}`,
		`{"task":"x","agent":"codex","models":["gpt-6-astra","gpt-6-luna","gpt-6.1-sol","openai/gpt-5"],"verify":"go test ./..."}`,
	}
	bad := []string{
		`{"task":" ","agent":"claude","models":["opus","sonnet"]}`,
		`{"task":"x","agent":"claude","models":["opus"]}`,
		`{"task":"x","agent":"claude","models":["a","b","c","d","e"]}`,
		`{"task":"x","agent":"claude","models":["opus","opus"]}`,
		`{"task":"x","agent":"claude","models":["opus","--help"]}`,
		`{"task":"x","agent":"claude","models":["opus","a b"]}`,
		`{"task":"x","agent":"claude","models":["opus","a;b"]}`,
		`{"task":"x","agent":"claude","models":["opus","` + strings.Repeat("a", MaxAIRaceModelID+1) + `"]}`,
		`{"task":"x","agent":"Claude","models":["opus","sonnet"]}`,
		`{"task":"x","agent":"-x","models":["opus","sonnet"]}`,
		`{"task":"x","agent":"","models":["opus","sonnet"]}`,
		`{"task":"x","agent":"claude","models":["opus","sonnet"],"verify":"a\nb"}`,
		`{"task":"` + strings.Repeat("a", MaxAIRaceModelsTask+1) + `","agent":"claude","models":["opus","sonnet"]}`,
		`{"task":"x","agent":"claude","models":["opus","sonnet"],"agents":["codex"]}`,
	}
	for _, b := range good {
		if _, err := DecodeArgs(OpAIRaceModels, []byte(b)); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	for _, b := range bad {
		if _, err := DecodeArgs(OpAIRaceModels, []byte(b)); err == nil {
			t.Errorf("%.80s accepted", b)
		}
	}
}
