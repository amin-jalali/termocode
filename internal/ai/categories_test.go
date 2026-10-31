package ai

import (
	"encoding/json"
	"strings"
	"testing"
)

// OMX-16: the built-in categories, and ai_categories that changes them
// or adds new ones; bad entries are left out with a reason.
func TestCategories(t *testing.T) {
	var s Settings
	raw := `{"ai_categories": {
		"ultrabrain": {"slot": "opus", "temperature": 0.2},
		"docs-de": {"like": "quick", "prompt_append": "Write German.", "tools": "read-only", "description": "German docs"},
		"Bad Name": {"slot": "x"},
		"hot": {"temperature": 3},
		"odd": {"tools": "some"}
	}}`
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	cats, errs := s.CategoryList()
	by := map[string]Category{}
	var names []string
	for _, c := range cats {
		by[c.Name] = c
		names = append(names, c.Name)
	}
	if got := strings.Join(names, ","); got != "quick,deep,visual,writing,architect,ultrabrain,docs-de" {
		t.Fatalf("names %s", got)
	}
	if a := by["architect"]; a.Tools != "read-only" || a.Like != "deep" || a.PromptAppend == "" || !a.Builtin {
		t.Errorf("architect %+v", a)
	}
	if u := by["ultrabrain"]; u.Slot != "opus" || u.Temperature == nil || *u.Temperature != 0.2 || u.PromptAppend == "" {
		t.Errorf("ultrabrain %+v", u)
	}
	if d := by["docs-de"]; d.Like != "quick" || d.Tools != "read-only" || d.PromptAppend != "Write German." || d.Builtin {
		t.Errorf("docs-de %+v", d)
	}
	if len(errs) != 3 {
		t.Fatalf("errors %v", errs)
	}
	all := ""
	for _, e := range errs {
		all += e.Error() + "\n"
	}
	for _, want := range []string{`"Bad Name"`, `"hot": temperature`, `"odd": tools`} {
		if !strings.Contains(all, want) {
			t.Errorf("errors lack %q:\n%s", want, all)
		}
	}
}

// OMX-16: ESCALATE is on unless ai_step_escalate is false.
func TestEscalatesSetting(t *testing.T) {
	off := false
	if !(Settings{}).Escalates() || (Settings{StepEscalate: &off}).Escalates() {
		t.Fatal("ai_step_escalate")
	}
}
