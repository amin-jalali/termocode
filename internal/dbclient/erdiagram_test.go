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
