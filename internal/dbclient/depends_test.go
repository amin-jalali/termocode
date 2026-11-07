package dbclient

import (
	"reflect"
	"strings"
	"testing"
)

func depSchema() CanonSchema {
	return CanonSchema{
		Driver: "postgres", Schema: "public",
		Tables: []CanonTable{
			{Name: "customers", Columns: []CanonColumn{{Name: "id", Type: "integer", NotNull: true}, {Name: "name", Type: "text"}},
				Constraints: []CanonConstraint{{Kind: "PRIMARY KEY", Columns: []string{"id"}}}},
			{Name: "orders", Columns: []CanonColumn{{Name: "id", Type: "integer"}, {Name: "customer_id", Type: "integer"}},
				Constraints: []CanonConstraint{
					{Kind: "PRIMARY KEY", Columns: []string{"id"}},
					{Kind: "FOREIGN KEY", Name: "orders_customer_fk", Columns: []string{"customer_id"}, RefTable: "customers", RefColumns: []string{"id"}},
				}},
			{Name: "notes", Columns: []CanonColumn{{Name: "body", Type: "text"}}},
		},
		Views: []CanonView{
			{Name: "big_orders", Definition: `SELECT o.id FROM "orders" o JOIN customers c ON c.id = o.customer_id -- notes
 WHERE o.id > 'customers'`},
		},
		Routines: []CanonRoutine{
			{Name: "order_count", Kind: "FUNCTION", Body: "BEGIN RETURN (SELECT count(*) FROM big_orders); END"},
		},
	}
}

func refNames(rs []DepRef) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Kind+" "+r.Name)
	}
	return out
}

func TestDependenciesOfTable(t *testing.T) {
	d := Dependencies(depSchema(), "customers")
	if got := refNames(d.UsedBy); !reflect.DeepEqual(got, []string{"table orders", "view big_orders"}) {
		t.Fatalf("UsedBy = %v", got)
	}
	if len(d.Uses) != 0 {
		t.Fatalf("Uses = %v", d.Uses)
	}
	if d.UsedBy[0].Via != "FK orders_customer_fk (customer_id) → (id)" {
		t.Fatalf("Via = %q", d.UsedBy[0].Via)
	}
	o := Dependencies(depSchema(), "orders")
	if got := refNames(o.Uses); !reflect.DeepEqual(got, []string{"table customers"}) {
		t.Fatalf("orders Uses = %v", got)
	}
}

func TestDependenciesSkipCommentsAndStrings(t *testing.T) {
	d := Dependencies(depSchema(), "notes")
	if len(d.UsedBy) != 0 {
		t.Fatalf("a name in a comment is not a use: %v", refNames(d.UsedBy))
	}
}

func TestDependenciesOfViewAndRoutine(t *testing.T) {
	v := Dependencies(depSchema(), "big_orders")
	if got := refNames(v.Uses); !reflect.DeepEqual(got, []string{"table customers", "table orders"}) {
		t.Fatalf("view Uses = %v", got)
	}
	if got := refNames(v.UsedBy); !reflect.DeepEqual(got, []string{"function order_count"}) {
		t.Fatalf("view UsedBy = %v", got)
	}
	f := Dependencies(depSchema(), "ORDER_COUNT")
	if got := refNames(f.Uses); !reflect.DeepEqual(got, []string{"view big_orders"}) {
		t.Fatalf("routine Uses = %v", got)
	}
	if !Dependencies(depSchema(), "missing").Missing {
		t.Fatal("unknown name must say Missing")
	}
}

func TestERDiagramBoxesAndEdges(t *testing.T) {
	got := ERDiagramText(depSchema(), "", 0)
	for _, want := range []string{
		"┌─ customers ", "│ PK id integer", "│ FK customer_id integer → customers.id",
		"orders.customer_id ──▶ customers.id",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("diagram misses %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, "notes") {
		t.Fatal("whole-schema diagram shows every table")
	}
	focus := ERDiagramText(depSchema(), "customers", 1)
	if strings.Contains(focus, "notes") || !strings.Contains(focus, "orders") {
		t.Fatalf("focus diagram shows only neighbours:\n%s", focus)
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, "┌") || strings.HasPrefix(line, "└") || strings.HasPrefix(line, "│") {
			if !strings.HasSuffix(line, "┐") && !strings.HasSuffix(line, "┘") && !strings.HasSuffix(line, "│") {
				t.Fatalf("box line not closed: %q", line)
			}
		}
	}
}
