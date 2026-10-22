package app_test

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// A SQLite table without a primary key is edited by its rowid: the rowid is
// selected hidden, never shown, and finds the row for UPDATE and DELETE.
func TestDBEditSQLiteWithoutKeyUsesRowid(t *testing.T) {
	bin, err := exec.LookPath("sqlite3")
	if err != nil {
		t.Skip("sqlite3 is not installed")
	}
	file := filepath.Join(t.TempDir(), "notes.db")
	script := `CREATE TABLE notes (title TEXT, body TEXT);
INSERT INTO notes VALUES ('alpha','one'),('beta','two'),('gamma','three');`
	if out, err := exec.Command(bin, file, script).CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v\n%s", err, out)
	}
	d := testkit.New(t, testkit.Options{W: 100, H: 30, Nvim: true, Files: map[string]string{
		"README.md":                 "x\n",
		".termocode/databases.json": fmt.Sprintf(`{"connections":[{"id":"notes","driver":"sqlite","path":%q}]}`, file),
	}})
	d.Key("f1")
	d.Type("Databases: Focus")
	d.Key("enter")
	d.Key("enter")
	if !d.WaitForText("main", 5*time.Second) {
		t.Fatalf("no database row:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("down", "enter")
	if !d.WaitForText("notes", 5*time.Second) {
		t.Fatalf("no table row:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("down", "down", "o")
	dbWaitPage(t, d, "gamma")
	text := dbText(t, d)
	if strings.Contains(text, "read-only") || strings.Contains(text, "title | body | ") {
		t.Fatalf("the page is read-only or shows a rowid column:\n%s", text)
	}
	cursorTo(t, d, "two")
	setCell(t, d, "TWO")
	dbWaitPage(t, d, "1 change")
	cursorTo(t, d, "three")
	d.Key("-")
	dbWaitPage(t, d, "2 changes")
	d.Key("w")
	if !d.WaitForText("Submit 2 changes", 2*time.Second) {
		t.Fatalf("no submit card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("right", "right", "enter") // Submit is the last button
	query := func() string {
		out, _ := exec.Command(bin, file, `SELECT title||'='||body FROM notes ORDER BY rowid`).CombinedOutput()
		return strings.TrimSpace(string(out))
	}
	if !d.WaitFor(func() bool { return query() == "alpha=one\nbeta=TWO" }, 3*time.Second) {
		t.Fatalf("rows after submit: %q\n%s", query(), ansi.Strip(d.Frame()))
	}
}
