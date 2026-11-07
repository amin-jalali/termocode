package dbclient

import "testing"

func objSearchFixture() []ObjectSource {
	return []ObjectSource{
		{Conn: "prod", Schemas: []CanonSchema{{
			Schema: "public",
			Tables: []CanonTable{
				{Name: "users", Columns: []CanonColumn{{Name: "id"}, {Name: "user_email"}}},
				{Name: "orders", Columns: []CanonColumn{{Name: "user_id"}}},
			},
			Views:    []CanonView{{Name: "active_users"}},
			Routines: []CanonRoutine{{Name: "count_users", Kind: "FUNCTION"}},
		}}},
		{Conn: "local", Schemas: []CanonSchema{{
			Schema: "main",
			Tables: []CanonTable{{Name: "Users"}},
		}}},
	}
}

// DBX-14: object search across every connection and schema finds
// tables, views, columns and routines; exact names first, then names
// that start with the query, then the rest.
func TestSearchObjects(t *testing.T) {
	hits := SearchObjects(objSearchFixture(), "users", 0)
	got := make([]string, len(hits))
	for i, h := range hits {
		got[i] = h.Conn + "/" + h.Schema + "/" + h.Kind + "/" + h.Path()
	}
	want := []string{
		"local/main/table/Users",
		"prod/public/table/users",
		"prod/public/view/active_users",
		"prod/public/routine/count_users",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}

	cols := SearchObjects(objSearchFixture(), "user_", 0)
	if len(cols) != 2 || cols[0].Kind != "column" || cols[0].Path() != "orders.user_id" || cols[1].Path() != "users.user_email" {
		t.Errorf("columns %+v", cols)
	}
	if n := len(SearchObjects(objSearchFixture(), "u", 2)); n != 2 {
		t.Errorf("limit: %d hits", n)
	}
	if n := len(SearchObjects(objSearchFixture(), "  ", 0)); n != 0 {
		t.Errorf("blank query: %d hits", n)
	}
}

func TestListObjectsAll(t *testing.T) {
	src := []ObjectSource{{Conn: "b", Schemas: []CanonSchema{{Schema: "public", Tables: []CanonTable{{Name: "users", Columns: []CanonColumn{{Name: "id"}}}}}}},
		{Conn: "a", Schemas: []CanonSchema{{Schema: "app", Views: []CanonView{{Name: "v"}}, Routines: []CanonRoutine{{Name: "f"}}}}}}
	hits := ListObjects(src)
	if len(hits) != 4 {
		t.Fatalf("hits = %+v", hits)
	}
	if hits[0].Conn != "a" || hits[len(hits)-1].Conn != "b" {
		t.Fatalf("not sorted by connection: %+v", hits)
	}
}
