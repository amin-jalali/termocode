package app_test

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/testkit"
)

// importSession is the fake server of editSession with two changes for
// imports: an INSERT that binds "BAD" fails, and with started set an INSERT
// never answers until the context ends.
type importSession struct {
	*editSession
	started chan struct{}
	once    sync.Once
}

func (s *importSession) Run(ctx context.Context, sql string, o dbclient.RunOptions) dbclient.Result {
	if strings.HasPrefix(sql, "INSERT") {
		for _, a := range o.Args {
			if a == "BAD" {
				s.editSession.Run(ctx, sql, o) // recorded
				return dbclient.Result{Err: errors.New(`ERROR: invalid input syntax for type integer: "BAD"`)}
			}
		}
		if s.started != nil {
			s.editSession.Run(ctx, sql, o)
			s.once.Do(func() { close(s.started) })
			<-ctx.Done()
			return dbclient.Result{Err: ctx.Err()}
		}
	}
	return s.editSession.Run(ctx, sql, o)
}

func importDriver(t *testing.T, sess dbclient.Session, conn string, files map[string]string) *testkit.Driver {
	t.Helper()
	return xferDriver(t, 100, 40, sess, conn, files)
}

const importCSV = "id;email;plan\n1;a@x.io;free\n2;b@x.io;pro\n3;c@x.io;\n"

// importCard presses i and picks the first file; it returns the frame with
// the preview card.
func importCard(t *testing.T, d *testkit.Driver) string {
	t.Helper()
	d.Key("i")
	if !d.WaitForText("data.csv", 3*time.Second) {
		t.Fatalf("no file picker:\n%s", frameText(d))
	}
	d.Key("enter")
	if !d.WaitForText("Mapping (file column", 5*time.Second) {
		t.Fatalf("no preview card:\n%s", frameText(d))
	}
	return frameText(d)
}

func (s *editSession) inserts() []string {
	var out []string
	for _, e := range s.log() {
		if strings.HasPrefix(e, "INSERT") {
			out = append(out, e)
		}
	}
	return out
}

func (s *editSession) insertArgs() [][]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out [][]any
	for i, e := range s.events {
		if strings.HasPrefix(e, "INSERT") {
			out = append(out, s.argsForLocked(i))
		}
	}
	return out
}

func (s *editSession) argsForLocked(event int) []any {
	n := -1
	for i := 0; i <= event; i++ {
		if e := s.events[i]; e != "BEGIN" && e != "COMMIT" && e != "ROLLBACK" {
			n++
		}
	}
	if n < 0 || n >= len(s.args) {
		return nil
	}
	return s.args[n]
}

func TestDBImportPreviewCardSniffsAndImportsInOneTransaction(t *testing.T) {
	s := newEditSession(0)
	d := importDriver(t, s, "", map[string]string{"data.csv": importCSV})
	card := importCard(t, d)
	for _, want := range []string{
		"Import 3 rows into app.users", "data.csv", "UTF-8", `delimiter ";"`, "header row", "3 rows",
		"id", "email", "plan", "First 3 rows", "1 | a@x.io | free",
		"inserted in groups, in one transaction", "nothing is written if any row fails",
		"Import", "Map columns", "Header: on", "Cancel",
	} {
		if !strings.Contains(card, want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}
	if !regexp.MustCompile(`(←|<-) plan`).MatchString(card) {
		t.Fatalf("card shows no mapping for plan:\n%s", card)
	}
	if len(s.log()) != 0 && s.has("BEGIN") {
		t.Fatal("the preview wrote something")
	}
	d.Key("enter") // Import
	if !d.WaitForText("Imported 3 rows", 5*time.Second) {
		t.Fatalf("no done toast:\n%s", frameText(d))
	}
	var tx []string
	for _, e := range s.log() {
		if !strings.HasPrefix(e, "SELECT") {
			tx = append(tx, e)
		}
	}
	want := []string{
		"BEGIN",
		"SAVEPOINT termocode_import_batch",
		"INSERT INTO `app`.`users` (`id`, `email`, `plan`) VALUES (?, ?, ?), (?, ?, ?), (?, ?, ?)",
		"RELEASE SAVEPOINT termocode_import_batch",
		"COMMIT",
	}
	if strings.Join(tx, "\n") != strings.Join(want, "\n") {
		t.Fatalf("server saw:\n%s", strings.Join(tx, "\n"))
	}
	args := s.insertArgs()
	if len(args) != 1 || fmt.Sprintf("%#v", args[0]) != fmt.Sprintf("%#v", []any{"1", "a@x.io", "free", "2", "b@x.io", "pro", "3", "c@x.io", nil}) {
		t.Fatalf("bound values %#v", args)
	}
}

func TestDBImportMappingCanSkipAColumn(t *testing.T) {
	s := newEditSession(0)
	d := importDriver(t, s, "", map[string]string{"data.csv": importCSV})
	importCard(t, d)
	d.Key("right", "enter") // Map columns
	if !d.WaitForText("Map columns (Enter changes one", 2*time.Second) {
		t.Fatalf("no mapping picker:\n%s", frameText(d))
	}
	d.Key("down", "down", "enter") // plan
	if !d.WaitForText("File column for plan", 2*time.Second) || !strings.Contains(frameText(d), "(skip this column)") {
		t.Fatalf("no source picker:\n%s", frameText(d))
	}
	d.Key("enter") // skip
	if !d.WaitForText("Map columns (Enter changes one", 2*time.Second) {
		t.Fatalf("no mapping picker after the change:\n%s", frameText(d))
	}
	d.Key("esc")
	if !d.WaitForText("(skipped)", 2*time.Second) || !regexp.MustCompile(`plan\s+(←|<-) \(skipped\)`).MatchString(frameText(d)) {
		t.Fatalf("card does not show the skipped column:\n%s", frameText(d))
	}
	d.Key("enter") // Import
	if !d.WaitForText("Imported 3 rows", 5*time.Second) {
		t.Fatalf("no done toast:\n%s", frameText(d))
	}
	ins := s.inserts()
	if len(ins) != 1 || !strings.Contains(ins[0], "(`id`, `email`) VALUES (?, ?), (?, ?), (?, ?)") {
		t.Fatalf("inserts %q", ins)
	}
}

func TestDBImportHeaderToggleChangesRowsAndColumns(t *testing.T) {
	d := importDriver(t, newEditSession(0), "", map[string]string{"data.csv": importCSV})
	importCard(t, d)
	d.Key("right", "right", "enter") // Header: on -> off
	if !d.WaitForText("Header: off", 2*time.Second) {
		t.Fatalf("no toggle:\n%s", frameText(d))
	}
	card := frameText(d)
	if !strings.Contains(card, "Import 4 rows into app.users") || !strings.Contains(card, "no header") || !strings.Contains(card, "column 1") {
		t.Fatalf("card after the toggle:\n%s", card)
	}
}

func TestDBImportBadRowRollsBackAndNamesTheRow(t *testing.T) {
	s := &importSession{editSession: newEditSession(0)}
	d := importDriver(t, s, "", map[string]string{"data.csv": "id,email,plan\n1,a,x\n2,b,y\nBAD,c,z\n4,d,w\n"})
	importCard(t, d)
	d.Key("enter")
	if !d.WaitForText("Import failed: row 3 (line 4)", 5*time.Second) {
		t.Fatalf("no failure card:\n%s", frameText(d))
	}
	card := frameText(d)
	if !strings.Contains(card, "invalid input syntax") || !strings.Contains(card, "Nothing was imported") {
		t.Fatalf("failure card:\n%s", card)
	}
	if s.has("COMMIT") || !s.has("ROLLBACK") {
		t.Fatalf("server saw: %q", s.log())
	}
	if len(s.rows) != 0 {
		t.Fatalf("rows written: %v", s.rows)
	}
}

func TestDBImportReadOnlyConnectionNeverImports(t *testing.T) {
	s := newEditSession(0)
	d := importDriver(t, s, `{"id":"local","driver":"mysql","host":"localhost","port":3306,"readOnly":true}`, map[string]string{"data.csv": importCSV})
	d.Key("i")
	if !d.WaitForText("Cannot import", 2*time.Second) {
		t.Fatalf("no refusal:\n%s", frameText(d))
	}
	if strings.Contains(frameText(d), "data.csv") || s.has("BEGIN") {
		t.Fatalf("a read-only connection offered an import:\n%s", frameText(d))
	}
}

func TestDBImportProductionGetsTheStrongCard(t *testing.T) {
	s := newEditSession(0)
	d := importDriver(t, s, `{"id":"local","driver":"mysql","host":"localhost","port":3306,"environment":"production"}`, map[string]string{"data.csv": importCSV})
	card := importCard(t, d)
	if !strings.Contains(card, "Write to production (local)? Import 3 rows into app.users") {
		t.Fatalf("card:\n%s", card)
	}
	d.Key("enter") // Cancel is the first button
	d.Settle(300 * time.Millisecond)
	if s.has("BEGIN") {
		t.Fatalf("Enter on the default button imported: %q", s.log())
	}
}

func TestDBImportCancelRollsBack(t *testing.T) {
	s := &importSession{editSession: newEditSession(0), started: make(chan struct{})}
	d := importDriver(t, s, "", map[string]string{"data.csv": importCSV})
	importCard(t, d)
	d.Key("enter")
	select {
	case <-s.started:
	case <-time.After(5 * time.Second):
		t.Fatalf("import never started:\n%s", frameText(d))
	}
	if !d.WaitForText("Importing into app.users", 2*time.Second) || !strings.Contains(frameText(d), "Esc cancels") {
		t.Fatalf("no progress card:\n%s", frameText(d))
	}
	d.Key("esc")
	if !d.WaitForText("Import cancelled", 5*time.Second) {
		t.Fatalf("no cancel toast:\n%s", frameText(d))
	}
	if s.has("COMMIT") || !s.has("ROLLBACK") {
		t.Fatalf("server saw: %q", s.log())
	}
}

func TestDBImportWithoutCSVFilesAsksForAPath(t *testing.T) {
	d := importDriver(t, newEditSession(0), "", nil)
	d.Key("i")
	if !d.WaitForText("CSV file (in the workspace", 3*time.Second) {
		t.Fatalf("no path prompt:\n%s", frameText(d))
	}
	d.Type("missing.csv")
	d.Key("enter")
	if !d.WaitForText("Cannot import", 3*time.Second) {
		t.Fatalf("no refusal for a missing file:\n%s", frameText(d))
	}
}

func TestDBImportInsideAManualTransactionJoinsIt(t *testing.T) {
	s := newEditSession(0)
	d := importDriver(t, s, "", map[string]string{"data.csv": importCSV})
	d.Key("t") // transaction mode picker
	if !d.WaitForText("Manual", 2*time.Second) {
		t.Fatalf("no mode picker:\n%s", frameText(d))
	}
	d.Key("down", "enter")
	d.Settle(300 * time.Millisecond)
	importCard(t, d)
	if !strings.Contains(frameText(d), "join your open transaction") {
		t.Fatalf("card does not say the rows join the transaction:\n%s", frameText(d))
	}
	d.Key("enter")
	if !d.WaitForText("Imported 3 rows", 5*time.Second) {
		t.Fatalf("no done toast:\n%s", frameText(d))
	}
	if s.has("COMMIT") {
		t.Fatalf("the import committed the user's transaction: %q", s.log())
	}
	if !s.has("SAVEPOINT termocode_import") || !s.has("RELEASE SAVEPOINT termocode_import") {
		t.Fatalf("no savepoint around the import: %q", s.log())
	}
}
