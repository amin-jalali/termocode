package dbclient

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestCSVTableName(t *testing.T) {
	cases := map[string]string{
		"/x/users.csv":        "users",
		"/x/Sales 2024.TSV":   "sales_2024",
		"/x/2024-orders.csv":  "t_2024_orders",
		"/x/.csv":             "data",
		"/x/naïve;drop.csv":   "na_ve_drop",
		"/x/report.final.csv": "report_final",
	}
	for in, want := range cases {
		if got := CSVTableName(in); got != want {
			t.Errorf("CSVTableName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCSVTableDDLQuotesAndDedupesColumns(t *testing.T) {
	sn := CSVSniff{Fields: 4, Columns: []string{"id", "Name", `a"b`, "name"}}
	got := CSVTableDDL("users", sn)
	want := `CREATE TABLE "users" ("id" TEXT, "Name" TEXT, "a""b" TEXT, "name_2" TEXT);`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestOpenCSVAsTableLoadsEveryRowIntoSQLite(t *testing.T) {
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not installed")
	}
	var sb strings.Builder
	sb.WriteString("id\tcity\tnote\n")
	for i := 1; i <= 1500; i++ {
		sb.WriteString(strings.Join([]string{strconv.Itoa(i), "c" + strconv.Itoa(i%7), "it's; ok"}, "\t") + "\n")
	}
	p := writeFile(t, "Cities.tsv", []byte(sb.String()))
	db := filepath.Join(t.TempDir(), "csv.db")
	tbl, err := OpenCSVAsTable(context.Background(), p, db)
	if err != nil {
		t.Fatal(err)
	}
	if tbl.Table != "cities" || tbl.Rows != 1500 || strings.Join(tbl.Columns, ",") != "id,city,note" {
		t.Fatalf("%+v", tbl)
	}
	sess, err := NewSQLiteSession(context.Background(), Conn{ID: "x", Driver: "sqlite", Path: db})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	r := sess.Run(context.Background(), `SELECT COUNT(*), COUNT(DISTINCT city), MAX(note) FROM "cities"`, RunOptions{})
	if r.Err != nil || r.Rows[0][0].Text != "1500" || r.Rows[0][1].Text != "7" || r.Rows[0][2].Text != "it's; ok" {
		t.Fatalf("%+v", r)
	}
}

func TestOpenCSVAsTableRefusesAnExistingDatabase(t *testing.T) {
	p := writeFile(t, "a.csv", []byte("x\n1\n"))
	db := filepath.Join(t.TempDir(), "keep.db")
	if err := os.WriteFile(db, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenCSVAsTable(context.Background(), p, db); err == nil {
		t.Fatal("an existing file must never be overwritten")
	}
	if b, _ := os.ReadFile(db); string(b) != "mine" {
		t.Fatalf("file changed: %q", b)
	}
}

func TestOpenCSVAsTableRemovesTheDatabaseOnFailure(t *testing.T) {
	db := filepath.Join(t.TempDir(), "gone.db")
	if _, err := OpenCSVAsTable(context.Background(), filepath.Join(t.TempDir(), "missing.csv"), db); err == nil {
		t.Fatal("a missing file must fail")
	}
	if _, err := os.Stat(db); !os.IsNotExist(err) {
		t.Fatalf("database left behind: %v", err)
	}
}
