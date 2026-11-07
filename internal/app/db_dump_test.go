package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// dumpSession fails the statement that says FAIL and records the rest.
type dumpSession struct {
	dbclient.Session
	ran []string
}

func (f *dumpSession) Run(_ context.Context, sql string, _ dbclient.RunOptions) dbclient.Result {
	f.ran = append(f.ran, sql)
	if strings.Contains(sql, "FAIL") {
		return dbclient.Result{Err: errors.New("syntax error")}
	}
	return dbclient.Result{Affected: 1}
}

// The dump card counts the statements and names the destructive ones.
func TestDBDumpSummary(t *testing.T) {
	stmts, err := dbDumpStatements("CREATE TABLE a (id int);\nINSERT INTO a VALUES (1);\nDROP TABLE b;\nTRUNCATE c;\n", "postgres")
	if err != nil || len(stmts) != 4 {
		t.Fatalf("statements: %v %v", stmts, err)
	}
	sum := dbDumpSummary(stmts, "postgres")
	if sum.total != 4 || sum.destructive != 2 {
		t.Fatalf("summary: %+v", sum)
	}
	if _, err := dbDumpStatements("  \n-- only a comment\n", "postgres"); err == nil {
		t.Fatal("an empty dump must refuse")
	}
}

// The dump runner sends the statements one by one with no card each and
// stops at the first error, naming it.
func TestDBRunDumpStopsAtTheFirstError(t *testing.T) {
	sess := &dumpSession{}
	var done int
	rep := dbRunDump(context.Background(), sess, []string{"INSERT 1", "INSERT FAIL", "INSERT 3"}, func(n int) { done = n })
	if rep.ran != 1 || rep.failed != 2 || rep.err == nil || len(sess.ran) != 2 || done != 1 {
		t.Fatalf("report %+v, ran %v, progress %d", rep, sess.ran, done)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if rep := dbRunDump(ctx, &dumpSession{}, []string{"INSERT 1"}, nil); !errors.Is(rep.err, context.Canceled) || rep.ran != 0 {
		t.Fatalf("cancelled: %+v", rep)
	}
}

// Import as new table: a CREATE TABLE of text columns named like the
// file's header, the name from the file name.
func TestDBNewTableFromFile(t *testing.T) {
	if got := dbNewTableName("/x/Sales Report-2026.csv"); got != "sales_report_2026" {
		t.Fatalf("name: %q", got)
	}
	if got := dbNewTableName("/x/2026.csv"); got != "t_2026" {
		t.Fatalf("name starting with a digit: %q", got)
	}
	sql := dbNewTableSQL(dbclient.DialectFor("postgres"), "public", "sales", []string{"id", "Full Name", "id"})
	for _, want := range []string{`CREATE TABLE "public"."sales"`, `"id" TEXT`, `"Full Name" TEXT`, `"id_2" TEXT`} {
		if !strings.Contains(sql, want) {
			t.Fatalf("%q not in %s", want, sql)
		}
	}
	if k, _ := dbclient.Classify(sql, "postgres"); k == dbclient.KindRead {
		t.Fatal("CREATE TABLE is a write")
	}
}
