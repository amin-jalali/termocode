package assets

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Spec (WEB-15, WFX-19): every card kind the cards topic can send (but
// laptop, which has its own sheet) has a title and a safe-first pair of
// choices in Card.tsx, so a new kind such as workflow is never drawn
// without words; and the More page reaches the Workflows page.
func TestCardKnowsEveryKind(t *testing.T) {
	b, err := os.ReadFile("../../webproto/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Enum []any `json:"enum"`
			} `json:"properties"`
		} `json:"defs"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, k := range s.Defs["Card"].Properties["kind"].Enum {
		if k, ok := k.(string); ok {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		t.Fatal("no card kinds in schema.json")
	}
	card, err := os.ReadFile("src/ui/Card.tsx")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range kinds {
		if k == "laptop" {
			continue
		}
		if !strings.Contains(string(card), "  "+k+`: "card.`+k+`.title",`) {
			t.Errorf("Card.tsx has no title for kind %s", k)
		}
		if k != "trust" && !strings.Contains(string(card), "  "+k+": { safe: ") {
			t.Errorf("Card.tsx has no choices for kind %s", k)
		}
	}
	views, err := os.ReadFile("src/views/views.tsx")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(views), `rest: "workflows"`) || !strings.Contains(string(views), "<WorkflowsView />") {
		t.Error("the More page does not reach the Workflows page")
	}
}
