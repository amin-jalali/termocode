package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (MOB-13, "What can I do?"): ai.catalog reads cached lists only
// (no MCP server starts), so a viewer may ask; it changes nothing, so no
// audit and no laptop card. Feature and query are short names / text.
func TestAICatalogRow(t *testing.T) {
	p, ok := Lookup(OpAICatalog)
	if !ok || p.Role != RoleViewer || p.LaptopCard || p.Audit != AuditNo || p.Live != 1 {
		t.Fatalf("ai.catalog row %+v", p)
	}
	for _, b := range []string{`{"feature":"a\u0007"}`, `{"query":"` + strings.Repeat("x", MaxCatalogQuery+1) + `"}`, `{"query":"a\u0000"}`, `{"server":"x"}`} {
		if _, err := DecodeArgs(OpAICatalog, json.RawMessage(b)); err == nil {
			t.Errorf("%s accepted", b)
		}
	}
	for _, b := range []string{`{}`, `{"feature":"mcp"}`, `{"feature":"git","query":"diff"}`} {
		if _, err := DecodeArgs(OpAICatalog, json.RawMessage(b)); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
}

// Spec (MOB-13, pinned tracker blocks): a viewer lists the pins; only a
// controller re-runs one (it calls an MCP tool on the laptop: audited)
// or unpins one (audited; the page asks Cancel first, no laptop card:
// pinning itself went through the AI's card). A pin id is 12 lower hex.
func TestTrackerRows(t *testing.T) {
	if p, ok := Lookup(OpAITrackerPins); !ok || p.Role != RoleViewer || p.LaptopCard || p.Audit != AuditNo {
		t.Fatalf("ai.tracker_pins row %+v", p)
	}
	for _, op := range []string{OpAITrackerRun, OpAITrackerUnpin} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes {
			t.Fatalf("%s row %+v", op, p)
		}
		if Allowed(RoleViewer, op) {
			t.Errorf("a viewer may call %s", op)
		}
		for _, b := range []string{`{}`, `{"id":"0123456789AB"}`, `{"id":"0123456789a"}`, `{"id":"0123456789abc"}`, `{"id":"../../etc/pw"}`} {
			if _, err := DecodeArgs(op, json.RawMessage(b)); err == nil {
				t.Errorf("%s %s accepted", op, b)
			}
		}
		if _, err := DecodeArgs(op, json.RawMessage(`{"id":"0123456789ab"}`)); err != nil {
			t.Errorf("%s: %v", op, err)
		}
	}
}
