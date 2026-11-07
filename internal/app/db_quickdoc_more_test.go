package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dbJoinTestState has users (id PK, email unique), orders (user_id → users.id)
// and tags (no keys), all loaded.
func dbJoinTestState() *dbState {
	s := dbQuickDocTestState()
	g := "pg/db/shop/schema/sales/Tables"
	add := func(name string, cols [][2]string, extra ...dbNode) {
		p := g + "/" + name
		n := dbNode{kind: dbNodeTable, name: name, path: p, conn: "pg", loaded: true}
		for _, c := range cols {
			cp := p + "/col/" + c[0]
			s.nodes[cp] = dbNode{kind: dbNodeColumn, name: c[0] + "  int", path: cp, conn: "pg", detail: c[1]}
			n.children = append(n.children, cp)
		}
		for _, e := range extra {
			e.path = p + "/" + e.path
			s.nodes[e.path] = e
			n.children = append(n.children, e.path)
		}
		s.nodes[p] = n
		gn := s.nodes[g]
		gn.children = append(gn.children, p)
		s.nodes[g] = gn
	}
	add("users", [][2]string{{"id", "PK"}, {"email", ""}})
	add("lines", [][2]string{{"order_id", ""}, {"user_id", ""}}, dbNode{kind: dbNodeDetail, name: "Foreign key · user_id → users.id", path: "fk/l1", conn: "pg"})
	add("tags", [][2]string{{"a", ""}, {"b", ""}})
	s.setUniqueIndexes(g+"/users", [][]string{{"email"}})
	return s
}

func TestDBJoinCardinality(t *testing.T) {
	s := dbJoinTestState()
	side := func(table string, cols ...string) dbclient.JoinSide {
		return dbclient.JoinSide{Table: table, Columns: cols}
	}
	cases := []struct {
		joined, other dbclient.JoinSide
		want          string
	}{
		{side("users", "id"), side("lines", "user_id"), "N:1"},
		{side("lines", "user_id"), side("users", "id"), "1:N"},
		{side("users", "email"), side("users", "id"), "1:1"},
		{side("tags", "a"), side("lines", "order_id"), "N:M"},
		{side("nope", "id"), side("lines", "user_id"), ""},
	}
	for _, c := range cases {
		if got := dbJoinCardinality(s, "pg", dbclient.JoinHint{Joined: c.joined, Other: c.other}); got != c.want {
			t.Errorf("%s ⋈ %s: got %q want %q", c.other.Table, c.joined.Table, got, c.want)
		}
	}
	// A foreign key alone marks the referenced side unique.
	lines := s.nodes["pg/db/shop/schema/sales/Tables/users/col/id"]
	lines.detail = ""
	s.nodes["pg/db/shop/schema/sales/Tables/users/col/id"] = lines
	if got := dbJoinCardinality(s, "pg", dbclient.JoinHint{Joined: side("users", "id"), Other: side("lines", "user_id")}); got != "N:1" {
		t.Fatalf("fk only: %q", got)
	}
}

func TestDBJoinHintPositions(t *testing.T) {
	s := dbJoinTestState()
	lines := []string{"SELECT * FROM lines l", "JOIN users u ON u.id = l.user_id"}
	pos := dbJoinHintPositions(s, "pg", lines, "postgres")
	if len(pos) != 1 || pos[0] != (dbHintPos{Row: 1, Col: 5, Text: "N:1"}) {
		t.Fatalf("positions %+v", pos)
	}
}

func TestDBParamDoc(t *testing.T) {
	title, lines := dbParamDoc("substr", []string{"string", "start", "length"}, 1)
	text := strings.Join(lines, "\n")
	if title != "Parameters of substr" || !strings.Contains(text, "substr(string, start, length)") || !strings.Contains(text, "▸ 2. start") || !strings.Contains(text, "  1. string") {
		t.Fatalf("doc %q\n%s", title, text)
	}
	// Past the end of a variadic list: the … parameter is current.
	_, lines = dbParamDoc("coalesce", []string{"value", "…"}, 4)
	if !strings.Contains(strings.Join(lines, "\n"), "▸ 2. …") {
		t.Fatalf("variadic %v", lines)
	}
	_, lines = dbParamDoc("now", nil, 0)
	if !strings.Contains(strings.Join(lines, "\n"), "No parameters") {
		t.Fatalf("no params %v", lines)
	}
}

func TestDBCellDoc(t *testing.T) {
	cols := []dbclient.Column{{Name: "id", Type: "int8"}, {Name: "note", Type: "text"}, {Name: "gone", Type: "date"}}
	row := []dbclient.Cell{{Text: "7"}, {Text: "line one\nline two"}, {Null: true}}
	title, lines := dbCellDoc(cols, row, 1, 3)
	text := strings.Join(lines, "\n")
	if title != "Cell note · row 3" {
		t.Fatalf("title %q", title)
	}
	for _, w := range []string{"Type: text", "line one", "line two", "Length: 17", "Row 3:", "id = 7", "gone = <null>"} {
		if !strings.Contains(text, w) {
			t.Errorf("no %q in\n%s", w, text)
		}
	}
	if _, lines := dbCellDoc(cols, row, 2, 3); !strings.Contains(strings.Join(lines, "\n"), "NULL") {
		t.Fatalf("null cell %v", lines)
	}
}

func TestDBQuickDocRows(t *testing.T) {
	cols := []dbclient.Column{{Name: "id"}, {Name: "total"}}
	rows := [][]dbclient.Cell{{{Text: "1"}, {Text: "9.50"}}, {{Text: "2"}, {Null: true}}}
	text := strings.Join(dbQuickDocRows(cols, rows), "\n")
	for _, w := range []string{"First 2 rows:", "id=1 · total=9.50", "id=2 · total=<null>"} {
		if !strings.Contains(text, w) {
			t.Errorf("no %q in\n%s", w, text)
		}
	}
	if got := dbQuickDocRows(cols, nil); len(got) != 2 || got[1] != "The table is empty." {
		t.Fatalf("empty %v", got)
	}
	long := [][]dbclient.Cell{{{Text: strings.Repeat("x", 500)}, {Text: "1"}}}
	for _, l := range dbQuickDocRows(cols, long) {
		if len([]rune(l)) > dbQuickDocRowWidth+2 {
			t.Fatalf("row not cut: %d", len([]rune(l)))
		}
	}
}

func TestDBQuickDocMorePaletteItems(t *testing.T) {
	want := map[string]string{"db-param-info": "Databases: Show Parameter Info", "db-join-hints": "Databases: Show JOIN Cardinality Hints"}
	for _, it := range databasesPaletteItems() {
		if w, ok := want[it.ID]; ok {
			if it.Title != w {
				t.Fatalf("%s title %q", it.ID, it.Title)
			}
			delete(want, it.ID)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing palette items %v", want)
	}
	for _, id := range []string{"db-param-info", "db-join-hints"} {
		if _, ok := aiActionPolicies[id]; !ok {
			t.Errorf("%s has no AI policy", id)
		}
	}
}
