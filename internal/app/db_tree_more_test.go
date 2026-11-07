package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-13: a schema shows Triggers and Extensions groups when the server
// has some, and a table shows its check constraints.
func TestDatabaseTreeShowsTriggersExtensionsAndChecks(t *testing.T) {
	st := newDBState()
	st.nodes["app"] = dbNode{kind: dbNodeConnection, name: "app", path: "app", conn: "app", loaded: true, children: []string{"app/schema"}}
	st.nodes["app/schema"] = dbNode{kind: dbNodeSchema, name: "public", path: "app/schema", conn: "app"}
	st.addIntrospection("app/schema", dbclient.IntrospectionResult{
		Objects:    []dbclient.SchemaObject{{Name: "orders", Kind: "BASE TABLE"}, {Name: "totals", Kind: "MATERIALIZED VIEW"}},
		Triggers:   []string{"orders_audit"},
		Extensions: []string{"pgcrypto"},
	})
	for path, kind := range map[string]dbNodeKind{
		"app/schema/Triggers":              dbNodeGroup,
		"app/schema/Triggers/orders_audit": dbNodeTrigger,
		"app/schema/Extensions":            dbNodeGroup,
		"app/schema/Extensions/pgcrypto":   dbNodeExtension,
		"app/schema/Views/totals":          dbNodeView,
	} {
		if n, ok := st.nodes[path]; !ok || n.kind != kind {
			t.Errorf("%s = %+v (ok %v), want kind %v", path, n, ok, kind)
		}
	}
	// A schema without triggers or extensions gets no empty groups.
	st.nodes["app/other"] = dbNode{kind: dbNodeSchema, name: "other", path: "app/other", conn: "app"}
	st.addIntrospection("app/other", dbclient.IntrospectionResult{Objects: []dbclient.SchemaObject{{Name: "t", Kind: "BASE TABLE"}}})
	if _, ok := st.nodes["app/other/Triggers"]; ok {
		t.Error("empty Triggers group")
	}
	if _, ok := st.nodes["app/other/Extensions"]; ok {
		t.Error("empty Extensions group")
	}

	st.addIntrospection("app/schema/Tables/orders", dbclient.IntrospectionResult{
		Columns: []dbclient.Column{{Name: "qty", Type: "integer"}},
		Checks:  []dbclient.SchemaCheck{{Name: "qty_positive", Expr: "CHECK ((qty > 0))"}},
	})
	n, ok := st.nodes["app/schema/Tables/orders/check/qty_positive"]
	if !ok || n.kind != dbNodeDetail || !strings.Contains(n.name, "Check · qty_positive") || !strings.Contains(n.name, "qty > 0") {
		t.Fatalf("check node = %+v (ok %v)", n, ok)
	}

	clean := sanitizeDBIntrospection(dbclient.IntrospectionResult{
		Triggers:   []string{"a\x1b[31mb"},
		Extensions: []string{"c\nd"},
		Checks:     []dbclient.SchemaCheck{{Name: "e\x1b[31m", Expr: "f\ng"}},
	})
	for _, s := range []string{clean.Triggers[0], clean.Extensions[0], clean.Checks[0].Name, clean.Checks[0].Expr} {
		if strings.ContainsAny(s, "\x1b\n") {
			t.Errorf("not sanitized: %q", s)
		}
	}
}
