package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-9, Editor and files): Snippets: Manage… from the web lists
// the user's own snippets, saves one (a body may hold new lines and
// tabs) and deletes one (the page asks first); changes need a controller.
func TestUserSnippetOps(t *testing.T) {
	for op, w := range map[string]struct {
		role  Role
		audit Audit
		args  string
	}{
		OpSnippetsUser:   {RoleViewer, AuditNo, `{}`},
		OpSnippetsSave:   {RoleController, AuditYes, `{"scope":"go","trigger":"iferr","body":"if err != nil {\n\treturn $1\n}"}`},
		OpSnippetsDelete: {RoleController, AuditYes, `{"scope":"*","trigger":"todo"}`},
	} {
		p, ok := Lookup(op)
		if !ok || p.Role != w.role || p.Audit != w.audit || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
		if _, err := DecodeArgs(op, json.RawMessage(w.args)); err != nil {
			t.Errorf("%s %s: %v", op, w.args, err)
		}
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
	for _, bad := range []string{`{"scope":"go","trigger":"","body":"x"}`, `{"scope":"go","trigger":"a b","body":"x"}`, `{"scope":"go","trigger":"x","body":""}`,
		`{"scope":"go","trigger":"x","body":"a\u0000"}`, `{"scope":"go","trigger":"x","body":"` + strings.Repeat("x", MaxSnippetBody+1) + `"}`} {
		if _, err := DecodeArgs(OpSnippetsSave, json.RawMessage(bad)); err == nil {
			t.Errorf("save %s: accepted", bad)
		}
	}
}
