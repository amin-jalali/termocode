package dbclient

import (
	"context"
	"strings"
	"testing"
)

// With SkipBad a row that fails (a bad value, a wrong field count) is
// left out and listed; the other rows go in and the import commits.
func TestImportCSVSkipBadRowsKeepsTheGoodOnes(t *testing.T) {
	spec := importSpec(t, "id,email\n1,a\nBAD,b\n3\n4,d\n5,e\n", 2, false)
	spec.SkipBad = true
	rec := &importRec{failValue: "BAD"}
	res := ImportCSV(context.Background(), rec, spec, nil)
	if res.Err != nil || res.Cancelled || res.Rows != 3 || res.SkippedRows != 2 {
		t.Fatalf("%+v", res)
	}
	if len(res.Skipped) != 2 || res.Skipped[0].Row != 2 || res.Skipped[0].Line != 3 ||
		!strings.Contains(res.Skipped[0].Err, "invalid input syntax") ||
		res.Skipped[1].Row != 3 || res.Skipped[1].Line != 4 || !strings.Contains(res.Skipped[1].Err, "1 field") {
		t.Fatalf("skipped %+v", res.Skipped)
	}
	joined := strings.Join(rec.events, "\n")
	if rec.events[len(rec.events)-1] != "COMMIT" || strings.Contains(joined, "\nROLLBACK\n") {
		t.Fatalf("events:\n%s", joined)
	}
	// The bad row went back to its own savepoint; the good row of its batch stayed.
	if !strings.Contains(joined, "ROLLBACK TO SAVEPOINT termocode_import_row") {
		t.Fatalf("no row savepoint:\n%s", joined)
	}
	var good []any
	arg := -1 // rec.args has no entry for BEGIN / COMMIT / ROLLBACK
	for i, e := range rec.events {
		if e == "BEGIN" || e == "COMMIT" || e == "ROLLBACK" {
			continue
		}
		arg++
		if strings.HasPrefix(e, "INSERT") && !strings.HasPrefix(rec.events[i+1], "ROLLBACK TO SAVEPOINT") {
			good = append(good, rec.args[arg]...)
		}
	}
	var ids []string
	for i := 0; i < len(good); i += 2 {
		ids = append(ids, good[i].(string))
	}
	if strings.Join(ids, ",") != "1,4,5" {
		t.Fatalf("inserted ids %v\n%s", ids, joined)
	}
}

// Without SkipBad nothing changes: the first bad row stops the import.
func TestImportCSVWithoutSkipStillStopsAtTheFirstBadRow(t *testing.T) {
	spec := importSpec(t, "id,email\n1,a\nBAD,b\n3,c\n", 10, false)
	res := ImportCSV(context.Background(), &importRec{failValue: "BAD"}, spec, nil)
	if res.Err == nil || res.FailedRow != 2 || res.Rows != 0 || res.SkippedRows != 0 {
		t.Fatalf("%+v", res)
	}
}

// Only the first importSkipKeep skipped rows are kept in the list; the
// count has them all.
func TestImportCSVSkippedListIsCapped(t *testing.T) {
	var b strings.Builder
	b.WriteString("id,email\n")
	for i := 0; i < importSkipKeep+5; i++ {
		b.WriteString("BAD,x\n")
	}
	spec := importSpec(t, b.String(), 100, false)
	spec.SkipBad = true
	res := ImportCSV(context.Background(), &importRec{failValue: "BAD"}, spec, nil)
	if res.Err != nil || res.Rows != 0 || res.SkippedRows != importSkipKeep+5 || len(res.Skipped) != importSkipKeep {
		t.Fatalf("rows %d skipped %d kept %d err %v", res.Rows, res.SkippedRows, len(res.Skipped), res.Err)
	}
}

func TestImportErrorLog(t *testing.T) {
	res := ImportResult{Rows: 3, SkippedRows: 2, Skipped: []ImportSkip{{Row: 2, Line: 3, Err: "bad\nvalue"}, {Row: 5, Line: 6, Err: "x"}}}
	got := ImportErrorLog("data.csv", "app.users", res)
	for _, want := range []string{"data.csv", "app.users", "3 rows imported, 2 skipped", "row 2 (line 3): bad value", "row 5 (line 6): x"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log lacks %q:\n%s", want, got)
		}
	}
}
