package dbclient

import (
	"strings"
	"testing"
)

func erSchema() CanonSchema {
	return CanonSchema{Driver: "postgres", Schema: "public", Tables: []CanonTable{
		{Name: "users", Columns: []CanonColumn{{Name: "id", Type: "integer"}, {Name: "email", Type: "character varying(200)"}},
			Constraints: []CanonConstraint{{Kind: "PRIMARY KEY", Name: "users_pkey", Columns: []string{"id"}}}},
		{Name: "order items", Columns: []CanonColumn{{Name: "id", Type: "integer"}, {Name: "user_id", Type: "integer"}},
			Constraints: []CanonConstraint{
				{Kind: "PRIMARY KEY", Name: "oi_pkey", Columns: []string{"id"}},
				{Kind: "FOREIGN KEY", Name: "oi_user", Columns: []string{"user_id"}, RefTable: "users", RefColumns: []string{"id"}},
			}},
	}}
}

// DBX-47: the ER diagram of a schema as Mermaid, PlantUML or DOT text:
// every table with its columns (PK / FK marked) and one line per
// foreign key, child to parent.
func TestERDiagramFormats(t *testing.T) {
	cases := map[string][]string{
		"mermaid": {
			"erDiagram",
			"  users {\n    integer id PK\n    character_varying_200 email\n  }",
			"  order_items {\n    integer id PK\n    integer user_id FK\n  }",
			`  users ||--o{ order_items : "oi_user"`,
		},
		"plantuml": {
			"@startuml",
			"entity \"users\" as t0 {\n  * id : integer <<PK>>\n  email : character varying(200)\n}",
			"entity \"order items\" as t1 {\n  * id : integer <<PK>>\n  user_id : integer <<FK>>\n}",
			"t0 ||--o{ t1 : oi_user",
			"@enduml",
		},
		"dot": {
			"digraph er {",
			`"users" [shape=box, label="users\l\lid : integer PK\lemail : character varying(200)\l"];`,
			`"order items" -> "users" [label="oi_user"];`,
			"}",
		},
	}
	for format, want := range cases {
		got, err := ERDiagram(erSchema(), format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in\n%s", format, w, got)
			}
		}
	}
	if _, err := ERDiagram(erSchema(), "svg"); err == nil {
		t.Error("unknown format accepted")
	}
}

// Names from the server cannot break out of the diagram text: quotes,
// line breaks and braces are neutralized.
func TestERDiagramEscapesNames(t *testing.T) {
	s := CanonSchema{Tables: []CanonTable{{Name: "a\"b\n}{x", Columns: []CanonColumn{{Name: "c\"\nd", Type: "t}"}}}}}
	for _, format := range []string{"mermaid", "plantuml", "dot"} {
		got, err := ERDiagram(s, format)
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(got, "\n") {
			if strings.Contains(line, "x") && strings.Contains(line, "}{") {
				t.Errorf("%s: raw braces from a name: %q", format, line)
			}
		}
		if strings.Contains(got, "a\"b") || strings.Contains(got, "c\"") && format != "dot" {
			t.Errorf("%s: raw quote from a name:\n%s", format, got)
		}
		if format == "dot" && !strings.Contains(got, `a\"b`) {
			t.Errorf("dot: quote not escaped:\n%s", got)
		}
	}
}

func erChain() CanonSchema {
	fk := func(name, ref string) CanonConstraint {
		return CanonConstraint{Kind: "FOREIGN KEY", Name: name, Columns: []string{ref + "_id"}, RefTable: ref, RefColumns: []string{"id"}}
	}
	return CanonSchema{Tables: []CanonTable{
		{Name: "shop_users", Columns: []CanonColumn{{Name: "id", Type: "int"}}},
		{Name: "shop_orders", Columns: []CanonColumn{{Name: "shop_users_id", Type: "int"}}, Constraints: []CanonConstraint{fk("o_u", "shop_users")}},
		{Name: "lines", Columns: []CanonColumn{{Name: "shop_orders_id", Type: "int"}, {Name: "items_id", Type: "int"}},
			Constraints: []CanonConstraint{fk("l_o", "shop_orders"), fk("l_i", "items")}},
		{Name: "items", Columns: []CanonColumn{{Name: "id", Type: "int"}}},
		{Name: "audit", Columns: []CanonColumn{{Name: "id", Type: "int"}}},
	}}
}

// DBX-47: the shortest foreign-key path between two tables, in either
// direction along the keys.
func TestERPath(t *testing.T) {
	s := erChain()
	cases := []struct {
		from, to, want string
		ok             bool
	}{
		{"shop_users", "items", "shop_users,shop_orders,lines,items", true},
		{"items", "shop_users", "items,lines,shop_orders,shop_users", true},
		{"lines", "lines", "lines", true},
		{"shop_users", "audit", "", false},
		{"nope", "items", "", false},
	}
	for _, c := range cases {
		got, ok := ERPath(s, c.from, c.to)
		if ok != c.ok || strings.Join(got, ",") != c.want {
			t.Errorf("%s→%s = %v %v, want %q %v", c.from, c.to, got, ok, c.want, c.ok)
		}
	}
}

// DBX-47: Only keeps the listed tables and the keys between them;
// Colors gives tables of one name prefix the same colour; Highlight wins.
func TestERDiagramStyled(t *testing.T) {
	s := erChain()
	only := ERStyle{Only: []string{"shop_orders", "lines"}}
	got, err := ERDiagramStyled(s, "mermaid", only)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "items {") || strings.Contains(got, "shop_users {") || !strings.Contains(got, `shop_orders ||--o{ lines : "l_o"`) || strings.Contains(got, "l_i") {
		t.Errorf("only:\n%s", got)
	}
	col := ERStyle{Colors: true, Highlight: []string{"audit"}}
	cases := map[string][]string{
		"mermaid":  {"  style shop_users fill:" + erColors[0], "  style shop_orders fill:" + erColors[0], "  style lines fill:" + erColors[1], "  style audit fill:" + erHighlight},
		"plantuml": {`entity "shop_users" as t0 ` + erColors[0] + " {", `entity "lines" as t2 ` + erColors[1] + " {", `entity "audit" as t4 ` + erHighlight + " {"},
		"dot":      {`"shop_orders" [shape=box, style=filled, fillcolor="` + erColors[0] + `"`, `"audit" [shape=box, style=filled, fillcolor="` + erHighlight + `"`},
	}
	for format, want := range cases {
		got, err := ERDiagramStyled(s, format, col)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("%s: missing %q in\n%s", format, w, got)
			}
		}
	}
	plain, _ := ERDiagram(s, "mermaid")
	if strings.Contains(plain, "style ") {
		t.Errorf("plain diagram has colours:\n%s", plain)
	}
}

// DBX-31: a virtual foreign key is drawn as a dashed line in every format.
func TestERDiagramVirtualKeysAreDashed(t *testing.T) {
	s := erSchema()
	s.Tables[1].Constraints[1].Virtual = true
	cases := map[string]string{
		"mermaid":  `  users ||..o{ order_items : "oi_user"`,
		"plantuml": "t0 ||..o{ t1 : oi_user",
		"dot":      `"order items" -> "users" [label="oi_user", style=dashed];`,
	}
	for format, want := range cases {
		got, err := ERDiagram(s, format)
		if err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if !strings.Contains(got, want) {
			t.Errorf("%s: missing %q in\n%s", format, want, got)
		}
	}
}
