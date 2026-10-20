//go:build dbint

package app

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Live PostgreSQL: TERMOCODE_DBINT_PG_URL=postgres://user:pass@host:port/db
// go test -tags dbint -run TestDBTableLivePostgres ./internal/app
func TestDBTableLivePostgres(t *testing.T) {
	url := os.Getenv("TERMOCODE_DBINT_PG_URL")
	if url == "" {
		t.Skip("TERMOCODE_DBINT_PG_URL is not set")
	}
	conn, err := dbclient.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	conn.ID = "live"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	sess, err := dbclient.NewSQLSession(ctx, conn, conn.Password)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	for _, q := range []string{
		`DROP SCHEMA IF EXISTS "Shop Data" CASCADE`,
		`CREATE SCHEMA "Shop Data"`,
		`CREATE TABLE "Shop Data".orgs (id bigint PRIMARY KEY, part int NOT NULL DEFAULT 0, UNIQUE (id, part))`,
		`CREATE TABLE "Shop Data"."Order Items" (id bigserial PRIMARY KEY, org_id bigint NOT NULL, org_part int NOT NULL, "Item Name" text, qty int DEFAULT 5, FOREIGN KEY (org_id, org_part) REFERENCES "Shop Data".orgs (id, part))`,
		`CREATE INDEX items_name_idx ON "Shop Data"."Order Items" ("Item Name")`,
		`CREATE VIEW "Shop Data".big_items AS SELECT * FROM "Shop Data"."Order Items" WHERE qty > 10`,
		`INSERT INTO "Shop Data".orgs VALUES (1, 0)`,
		`INSERT INTO "Shop Data"."Order Items" (org_id, org_part, "Item Name") SELECT 1, 0, 'item-' || i FROM generate_series(1, 250) i`,
		`CREATE FUNCTION "Shop Data".touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$`,
		`CREATE TRIGGER items_touch BEFORE INSERT OR UPDATE ON "Shop Data"."Order Items" FOR EACH ROW EXECUTE FUNCTION "Shop Data".touch()`,
	} {
		if r := sess.Run(ctx, q, dbclient.RunOptions{}); r.Err != nil {
			t.Fatalf("%s: %v", q, r.Err)
		}
	}
	defer sess.Run(context.Background(), `DROP SCHEMA IF EXISTS "Shop Data" CASCADE`, dbclient.RunOptions{})

	tbl := dbTable{driver: "postgres", schema: "Shop Data", name: "Order Items"}
	req := dbclient.IntrospectionRequest{Schema: "Shop Data", Object: "Order Items", Kind: "table"}
	meta, err := dbLoadTableMeta(ctx, sess, tbl, req)
	if err != nil {
		t.Fatal(err)
	}
	res := dbDescribeResult(tbl, meta)
	var lines []string
	for _, row := range res.Rows {
		var cells []string
		for _, c := range row {
			cells = append(cells, c.String())
		}
		lines = append(lines, strings.Join(cells, "|"))
	}
	text := strings.Join(lines, "\n")
	t.Logf("describe:\n%s", text)
	for _, want := range []string{"Column|id|bigint|not null · default nextval", "primary key", "Column|Item Name|text|can be null", "Column|qty|integer|can be null · default 5", "Index|items_name_idx", "Foreign key|", "Trigger|items_touch||BEFORE INSERT OR UPDATE"} {
		if !strings.Contains(text, want) {
			t.Errorf("describe lacks %q", want)
		}
	}
	ddl := dbBuildDDL(tbl, meta)
	t.Logf("ddl:\n%s", ddl)
	// The rebuilt DDL must run on a clean schema.
	if r := sess.Run(ctx, `CREATE SCHEMA "Copy Data"`, dbclient.RunOptions{}); r.Err != nil {
		t.Fatal(r.Err)
	}
	defer sess.Run(context.Background(), `DROP SCHEMA IF EXISTS "Copy Data" CASCADE`, dbclient.RunOptions{})
	_ = sess.Run(ctx, `SET search_path TO "Copy Data"`, dbclient.RunOptions{})

	// Data view: ordered pages, filter, count.
	tv := newDBTableView(tbl)
	tv.keys, tv.cols = []string{"id"}, []string{"id", "org_id", "org_part", "Item Name", "qty"}
	tv.where = `qty = 5 AND "Item Name" LIKE 'item-1%'`
	sql, err := tv.selectSQL(101)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("sql: %s", sql)
	r := sess.Run(ctx, sql, dbclient.RunOptions{MaxRows: 101})
	if r.Err != nil || len(r.Rows) != 101 {
		t.Fatalf("page: %d rows, %v", len(r.Rows), r.Err)
	}
	tv.offset = 100
	sql, _ = tv.selectSQL(101)
	if r = sess.Run(ctx, sql, dbclient.RunOptions{MaxRows: 101}); r.Err != nil || len(r.Rows) != 11 {
		t.Fatalf("second page: %d rows, %v", len(r.Rows), r.Err)
	}
	csql, _ := tv.countSQL()
	if r = sess.Run(ctx, csql, dbclient.RunOptions{}); r.Err != nil || r.Rows[0][0].Text != "111" {
		t.Fatalf("count: %+v %v", r.Rows, r.Err)
	}
	tv.orderCol, tv.desc, tv.where, tv.offset = "Item Name", true, "", 0
	sql, _ = tv.selectSQL(3)
	if r = sess.Run(ctx, sql, dbclient.RunOptions{MaxRows: 3}); r.Err != nil || len(r.Rows) != 3 {
		t.Fatalf("sorted by a quoted column: %v %v", r.Rows, r.Err)
	}
	// A view works the same way.
	vt := dbTable{driver: "postgres", schema: "Shop Data", name: "big_items", view: true}
	vmeta, err := dbLoadTableMeta(ctx, sess, vt, dbclient.IntrospectionRequest{Schema: "Shop Data", Object: "big_items", Kind: "view"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("view ddl:\n%s", dbBuildDDL(vt, vmeta))
	if !strings.Contains(dbBuildDDL(vt, vmeta), "CREATE VIEW") {
		t.Error("no view definition")
	}
}
