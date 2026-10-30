package app_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Edge cases of editing a data view: \N sets NULL, a deleted row is not
// edited, undo and revert with nothing to undo, the transposed view.
func TestCovSvcEditEdges(t *testing.T) {
	s := newEditSession(4)
	d := editDriver(t, 110, 32, s, "")
	openUsers(t, d)
	// u with nothing changed.
	d.Key("u")
	covSvcWaitFrame(t, d, "No pending changes")
	// \N is NULL.
	cursorTo(t, d, "user-002@example.com")
	d.Key("right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right", "right")
	setCell(t, d, `\N`)
	dbWaitPage(t, d, "<null>")
	// A row marked for deletion is not edited.
	cursorTo(t, d, "user-003@example.com")
	d.Key("-")
	d.Key("e")
	covSvcWaitFrame(t, d, "deleted")
	d.Key("esc")
	// The transposed view edits the field under the cursor.
	cursorTo(t, d, "user-001@example.com")
	d.Key("t")
	dbWaitPage(t, d, "email")
	cursorTo(t, d, "user-001@example.com")
	setCell(t, d, "first@example.com")
	dbWaitPage(t, d, "first@example.com")
	d.Key("t")
	// Revert all: the card, then the rows as the server has them.
	d.Palette("db-revert-changes")
	covSvcWaitFrame(t, d, "Revert")
	d.Key("right", "enter")
	if !d.WaitFor(func() bool { return !strings.Contains(dbText(t, d), "first@example.com") }, 4*time.Second) {
		t.Fatalf("revert kept the edit:\n%s\n%s", dbText(t, d), ansi.Strip(d.Frame()))
	}
	d.Palette("db-revert-changes")
	covSvcWaitFrame(t, d, "No pending changes")
}

// A read-only connection does not submit; an AI result is not editable.
func TestCovSvcEditReadOnly(t *testing.T) {
	s := newEditSession(3)
	d := editDriver(t, 110, 32, s, `{"id":"local","driver":"mysql","host":"localhost","port":3306,"readOnly":true}`)
	openUsers(t, d)
	cursorTo(t, d, "user-002@example.com")
	d.Key("e")
	covSvcWaitFrame(t, d, "read-only")
}
