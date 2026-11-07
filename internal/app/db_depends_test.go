package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

func TestDBDepsText(t *testing.T) {
	d := dbclient.Deps{Kind: "table", Name: "users",
		UsedBy: []dbclient.DepRef{{Kind: "table", Name: "orders", Via: "FK orders_user (user_id) → (id)"}, {Kind: "view", Name: "active_users", Via: "definition"}}}
	s := dbDepsText("public", d)
	for _, want := range []string{"table public.users", "Uses", "(nothing)", "Used by", "orders", "active_users", "definition"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
	if s := dbDepsText("public", dbclient.Deps{Name: "nope", Missing: true}); !strings.Contains(s, "not found") {
		t.Fatalf("missing object text = %q", s)
	}
}

func TestDBDependsPaletteAndPolicy(t *testing.T) {
	want := map[string]bool{"db-dependencies": false, "db-er-text": false}
	for _, it := range databasesPaletteItems() {
		if _, ok := want[it.ID]; ok {
			want[it.ID] = true
		}
	}
	for id, ok := range want {
		if !ok {
			t.Fatalf("no %s palette item", id)
		}
		if _, ok := aiActionPolicies[id]; !ok {
			t.Fatalf("%s has no AI policy", id)
		}
	}
}
