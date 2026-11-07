package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Copy Table Data (DBX-19): the rows of app.users go through the import
// card into app.users_copy on the same connection.
func TestDBCopyTableDataGoesThroughTheImportCard(t *testing.T) {
	s := newEditSession(3)
	d := importDriver(t, s, "", nil)
	d.Key("f1")
	d.Type("Copy Table Data to Another")
	d.Key("enter")
	if !d.WaitForText("Copy rows of app.users into", 3*time.Second) || !strings.Contains(frameText(d), "same database") {
		t.Fatalf("no target picker:\n%s", frameText(d))
	}
	d.Key("enter")
	if !d.WaitForText("Target table", 3*time.Second) {
		t.Fatalf("no target prompt:\n%s", frameText(d))
	}
	d.Type("app.users_copy")
	d.Key("enter")
	if !d.WaitForText("Mapping (file column", 5*time.Second) {
		t.Fatalf("no import card:\n%s", frameText(d))
	}
	card := frameText(d)
	for _, want := range []string{"Import 3 rows into app.users_copy", "Copied from", "app.users", "header row", "Cancel"} {
		if !strings.Contains(card, want) {
			t.Fatalf("card lacks %q:\n%s", want, card)
		}
	}
	if len(s.inserts()) != 0 {
		t.Fatal("rows were written before Import")
	}
	d.Key("enter") // Import
	if !d.WaitForText("Imported 3 rows into app.users_copy", 5*time.Second) {
		t.Fatalf("no done toast:\n%s", frameText(d))
	}
	ins := s.inserts()
	if len(ins) != 1 || !strings.HasPrefix(ins[0], "INSERT INTO `app`.`users_copy` (`id`, `email`, `plan`)") {
		t.Fatalf("inserts %q", ins)
	}
	args := s.insertArgs()
	want := []any{"1", "user-001@example.com", "pro", "2", "user-002@example.com", "team", "3", "user-003@example.com", "free"}
	if len(args) != 1 || fmt.Sprintf("%#v", args[0]) != fmt.Sprintf("%#v", want) {
		t.Fatalf("bound values %#v", args)
	}
}

func TestDBCopyTableDataRefusesTheSourceItself(t *testing.T) {
	s := newEditSession(3)
	d := importDriver(t, s, "", nil)
	d.Key("f1")
	d.Type("Copy Table Data to Another")
	d.Key("enter")
	if !d.WaitForText("Copy rows of app.users into", 3*time.Second) {
		t.Fatalf("no target picker:\n%s", frameText(d))
	}
	d.Key("enter")
	if !d.WaitForText("Target table", 3*time.Second) {
		t.Fatalf("no target prompt:\n%s", frameText(d))
	}
	d.Type("app.users")
	d.Key("enter")
	if !d.WaitForText("That is the source table", 3*time.Second) {
		t.Fatalf("no refusal:\n%s", frameText(d))
	}
	if len(s.inserts()) != 0 {
		t.Fatal("rows were written")
	}
}
