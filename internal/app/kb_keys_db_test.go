package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// KB-1: D means remove everywhere. On a table it starts the drop (a card
// with the exact SQL, Cancel first); Show DDL moved to v.
func TestKB1DOnATableAsksBeforeDropping(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("D")
	if !d.WaitForText("Runs:", 2*time.Second) {
		t.Fatalf("D on a table did not open the drop card:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("esc")
	for _, q := range s.ran() {
		if strings.Contains(q, "DROP") {
			t.Fatalf("a drop ran before the owner said yes: %s", q)
		}
	}
}

func TestKB1VShowsTheTableDDL(t *testing.T) {
	d, _ := tableDriver(t, 80, 24)
	d.Key("v")
	if !d.WaitFor(func() bool { return strings.HasSuffix(dbBufName(t, d), "SQL DDL: app.users") }, 2*time.Second) {
		t.Fatalf("v did not open the DDL buffer: %s\n%s", dbBufName(t, d), ansi.Strip(d.Frame()))
	}
}

// KB-2: M is the transaction mode (t is a terminal, as in Containers) and
// Q runs the select (s means start).
func TestKB2MOpensTheTransactionModePicker(t *testing.T) {
	d, _ := tableDriver(t, 100, 30)
	d.Key("g") // the connection row
	d.Key("M")
	if !d.WaitForText("Transaction mode for local", 2*time.Second) {
		t.Fatalf("M did not open the mode picker:\n%s", ansi.Strip(d.Frame()))
	}
}

func TestKB2QSelectsRowsInTheConsole(t *testing.T) {
	d, s := tableDriver(t, 100, 30)
	d.Key("Q")
	dbWaitPage(t, d, "user-000@example.com")
	if got := s.selects(); len(got) != 1 || got[0] != "SELECT * FROM `app`.`users` LIMIT 100;" {
		t.Fatalf("Q ran %v", got)
	}
}
