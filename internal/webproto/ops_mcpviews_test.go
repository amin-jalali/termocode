package webproto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// Spec (MOBUX-14): mcp.views is a viewer read; mcp.view_run is a
// controller run, audited, of one "mcp." view with at most
// MaxMCPViewArgs distinct args.
func TestMCPViewOpRows(t *testing.T) {
	p, ok := Lookup(OpMCPViews)
	if !ok || p.Role != RoleViewer || p.Audit != AuditNo || p.LaptopCard {
		t.Fatalf("mcp.views row %+v", p)
	}
	p, ok = Lookup(OpMCPViewRun)
	if !ok || p.Role != RoleController || p.Audit != AuditYes || p.LaptopCard {
		t.Fatalf("mcp.view_run row %+v", p)
	}
	for _, op := range []string{OpMCPViews, OpMCPViewRun} {
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
}

func TestMCPViewRunArgsRules(t *testing.T) {
	good := []string{
		`{"view":"mcp.github.list_issues"}`,
		`{"view":"mcp.docs.@resources","args":[{"id":"uri","value":"file:///a.md"}],"compact":true}`,
	}
	for _, s := range good {
		if _, err := DecodeArgs(OpMCPViewRun, json.RawMessage(s)); err != nil {
			t.Errorf("%s: %v", s, err)
		}
	}
	many := make([]string, MaxMCPViewArgs+1)
	for i := range many {
		many[i] = `{"id":"a` + strings.Repeat("x", i) + `","value":"1"}`
	}
	bad := []string{
		`{"view":""}`,
		`{"view":"git.status"}`,
		`{"view":"mcp.s.t","args":[{"id":"a","value":"1"},{"id":"a","value":"2"}]}`,
		`{"view":"mcp.s.t","args":[{"id":"","value":"1"}]}`,
		`{"view":"mcp.s.t","args":[{"id":"a","value":"x\u0000"}]}`,
		`{"view":"mcp.s.t","args":[` + strings.Join(many, ",") + `]}`,
	}
	for _, s := range bad {
		if _, err := DecodeArgs(OpMCPViewRun, json.RawMessage(s)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", s, err)
		}
	}
}
