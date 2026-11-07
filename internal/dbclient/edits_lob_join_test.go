package dbclient

import (
	"bytes"
	"strings"
	"testing"
)

// DBX-55: a binary cell (a LOB loaded from a file) is bound as bytes, and
// the preview shows a short binary literal, not the whole value.
func TestEditStatementsBindBinaryAsBytes(t *testing.T) {
	data := bytes.Repeat([]byte{0xde, 0xad}, 40)
	upd := RowEdit{Kind: EditUpdate, Key: []EditValue{{Column: "id", Value: Cell{Text: "1"}}},
		Values: []EditValue{{Column: "img", Value: CellOfBytes(data)}}}
	cases := []struct {
		d    Dialect
		head string
	}{
		{PostgresDialect{}, `'\xdead`},
		{MySQLDialect{}, `X'dead`},
		{SQLiteDialect{}, `X'dead`},
		{SQLServerDialect{}, `0xdead`},
		{OracleDialect{}, `HEXTORAW('dead`},
	}
	for _, c := range cases {
		stmts, err := EditStatements(c.d, "", "t", []RowEdit{upd})
		if err != nil {
			t.Fatal(err)
		}
		b, ok := stmts[0].Args[0].([]byte)
		if !ok || !bytes.Equal(b, data) {
			t.Fatalf("%T: arg %#v, want the bytes", c.d, stmts[0].Args[0])
		}
		if !strings.Contains(stmts[0].Preview, c.head) || !strings.Contains(stmts[0].Preview, "80 bytes") {
			t.Fatalf("%T preview %q", c.d, stmts[0].Preview)
		}
		if strings.Contains(stmts[0].Preview, strings.Repeat("dead", 20)) {
			t.Fatalf("%T preview spells out the whole value: %q", c.d, stmts[0].Preview)
		}
	}
	if _, ok := CellBytes(Cell{Text: "0xzz", Kind: "binary"}); ok {
		t.Fatal("bad hex is not bytes")
	}
	if _, ok := CellBytes(Cell{Text: "0x00"}); ok {
		t.Fatal("a text cell is not bytes")
	}
}

// DBX-55: JoinSelect reads the tables of a SELECT over joined tables and
// the qualified select items; anything else says why.
func TestJoinSelect(t *testing.T) {
	jt, why := JoinSelect(`SELECT o.id, o.status, u.name FROM orders o JOIN users AS u ON u.id = o.user_id WHERE o.total > 3 ORDER BY o.id LIMIT 10`, "postgres")
	if why != "" {
		t.Fatal(why)
	}
	if len(jt.Tables) != 2 || jt.Tables[0] != (JoinTable{Table: "orders", Alias: "o"}) || jt.Tables[1] != (JoinTable{Table: "users", Alias: "u"}) {
		t.Fatalf("tables %+v", jt.Tables)
	}
	if len(jt.Quals) != 3 || jt.Quals[2] != (JoinColumn{Qual: "u", Name: "name"}) || jt.TableOf("u") != 1 || jt.TableOf("orders") != -1 {
		t.Fatalf("quals %+v", jt.Quals)
	}
	ok := []string{
		`SELECT * FROM a, b WHERE a.id = b.a_id`,
		`SELECT a.x, b.* FROM s.a LEFT OUTER JOIN s.b ON (a.id = b.a_id AND b.k = 'JOIN') INNER JOIN c ON c.id = b.c_id`,
		`SELECT id, v FROM a JOIN b USING (id);`,
		`SELECT a.id FROM a CROSS JOIN b`,
		`SELECT a.id FROM a JOIN b ON LEFT(a.n, 2) = b.n`,
	}
	for _, q := range ok {
		if _, why := JoinSelect(q, "postgres"); why != "" {
			t.Errorf("%s: %s", q, why)
		}
	}
	bad := []string{
		`SELECT id FROM a`,
		`SELECT DISTINCT a.id FROM a JOIN b ON a.id = b.id`,
		`SELECT a.id FROM a JOIN (SELECT id FROM b) x ON x.id = a.id`,
		`SELECT a.id FROM a NATURAL JOIN b`,
		`SELECT a.id, count(*) FROM a JOIN b ON a.id = b.id GROUP BY a.id`,
		`SELECT a.id FROM a JOIN b ON a.id = b.id UNION SELECT 1`,
		`SELECT z.id FROM a JOIN b ON a.id = b.id`,
		`SELECT a.id AS k FROM a JOIN b ON a.id = b.id`,
	}
	for _, q := range bad {
		if _, why := JoinSelect(q, "postgres"); why == "" {
			t.Errorf("%s: accepted", q)
		}
	}
}
