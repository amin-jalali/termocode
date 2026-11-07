package dbclient

import (
	"reflect"
	"testing"
)

func paramNames(refs []ParamRef) []string {
	var out []string
	for _, r := range refs {
		out = append(out, r.Name)
	}
	return out
}

func TestFindParams(t *testing.T) {
	cases := []struct {
		driver, sql string
		want        []string
	}{
		{"mysql", "SELECT * FROM t WHERE a = ? AND b = :name", []string{"?1", ":name"}},
		{"mysql", "SELECT @v, @@version FROM t", nil}, // MySQL user variables are the server's
		{"postgres", "SELECT $1, $2, $1 FROM t WHERE x = :who", []string{"$1", "$2", "$1", ":who"}},
		{"postgres", "SELECT data ? 'k', a::text, arr[1:n], b @> c FROM t", nil},
		{"postgres", "SELECT @who", []string{"@who"}},
		{"sqlite", "SELECT ?, @x, $1, :y", []string{"?1", "@x", "$1", ":y"}},
		{"postgres", "SELECT ':no', \"?x\", $$ :no $$, $t$ ? $t$ -- :no\n/* :no */ , :yes", []string{":yes"}},
		{"mysql", "SELECT 'it''s ?', `a?`, \"q\\\"?\" # :no\n, :ok", []string{":ok"}},
		{"mysql", "SET x := 1", nil},
		{"sqlite", "SELECT 1", nil},
	}
	for _, c := range cases {
		got := paramNames(FindParams(c.sql, c.driver))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %q: got %q, want %q", c.driver, c.sql, got, c.want)
		}
	}
}

func TestFindParamsOffsets(t *testing.T) {
	sql := "SELECT :a, ?"
	refs := FindParams(sql, "mysql")
	if len(refs) != 2 || sql[refs[0].Start:refs[0].End] != ":a" || sql[refs[1].Start:refs[1].End] != "?" {
		t.Fatalf("refs = %+v", refs)
	}
}

func TestUniqueParamNames(t *testing.T) {
	refs := FindParams("SELECT $1, :a, $1, :a, ?", "sqlite")
	if got := UniqueParamNames(refs); !reflect.DeepEqual(got, []string{"$1", ":a", "?1"}) {
		t.Fatalf("got %q", got)
	}
}

func TestBindParams(t *testing.T) {
	vals := map[string]string{":id": "7", "?1": "pro", ":name": "NULL", "$1": "x", "@w": "1.5"}
	sql, args := BindParams("SELECT * FROM t WHERE id = :id AND plan = ? AND n = :name OR id = :id", FindParams("SELECT * FROM t WHERE id = :id AND plan = ? AND n = :name OR id = :id", "mysql"), vals, "mysql")
	if sql != "SELECT * FROM t WHERE id = ? AND plan = ? AND n = ? OR id = ?" {
		t.Fatalf("mysql sql = %q", sql)
	}
	if !reflect.DeepEqual(args, []any{int64(7), "pro", nil, int64(7)}) {
		t.Fatalf("mysql args = %#v", args)
	}
	src := "SELECT :id, $1, :id, @w"
	sql, args = BindParams(src, FindParams(src, "postgres"), vals, "postgres")
	if sql != "SELECT $1, $2, $1, $3" {
		t.Fatalf("pg sql = %q", sql)
	}
	if !reflect.DeepEqual(args, []any{int64(7), "x", "1.5"}) {
		t.Fatalf("pg args = %#v", args)
	}
}

func TestParamValue(t *testing.T) {
	for in, want := range map[string]any{"12": int64(12), "-3": int64(-3), "NULL": nil, "null": "null", "": "", "007x": "007x", "1.5": "1.5"} {
		if got := ParamValue(in); got != want {
			t.Errorf("ParamValue(%q) = %#v, want %#v", in, got, want)
		}
	}
}
