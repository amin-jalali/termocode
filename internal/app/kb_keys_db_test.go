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
	if !d.WaitForText("DROP TABLE", 2*time.Second) {
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
