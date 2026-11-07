package app_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/testkit"
)

// lastParamRun is the last statement the fake server saw that holds
// "plan = ?", with its bound values.
func lastParamRun(s *editSession) (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		if strings.Contains(s.events[i], "plan = ?") {
			return s.events[i], fmt.Sprintf("%#v", s.argsFor(i))
		}
	}
	return "", ""
}

func answerParam(t *testing.T, d *testkit.Driver, title, value string) {
	t.Helper()
	if !d.WaitForText(title, 2*time.Second) {
		t.Fatalf("no %q prompt:\n%s", title, ansi.Strip(d.Frame()))
	}
	if value != "" {
		d.Type(value)
	}
	d.Key("enter")
}

func TestDBConsoleAsksForQueryParamsAndRemembersThem(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 120, 30, s, "")
	consoleRun(t, d, "SELECT * FROM users WHERE id = :id AND plan = ?;", false)
	answerParam(t, d, "Query parameter 1 of 2: :id", "2")
	answerParam(t, d, "Query parameter 2 of 2: ?1", "pro")
	if !d.WaitFor(func() bool { sql, _ := lastParamRun(s); return sql != "" }, 2*time.Second) {
		t.Fatalf("the statement never ran: %q", s.log())
	}
	sql, args := lastParamRun(s)
	if !strings.Contains(sql, "id = ? AND plan = ?") || strings.Contains(sql, ":id") {
		t.Fatalf("sql = %q", sql)
	}
	if args != `[]interface {}{2, "pro"}` {
		t.Fatalf("args = %s", args)
	}
	// The second run offers the values used last time in this console.
	n := len(s.log())
	consoleRun(t, d, "SELECT * FROM users WHERE id = :id AND plan = ?;", false)
	answerParam(t, d, "Query parameter 1 of 2: :id", "")
	answerParam(t, d, "Query parameter 2 of 2: ?1", "")
	if !d.WaitFor(func() bool { return len(s.log()) > n }, 2*time.Second) {
		t.Fatal("the second run never ran")
	}
	if _, args := lastParamRun(s); args != `[]interface {}{2, "pro"}` {
		t.Fatalf("remembered args = %s", args)
	}
}

func TestDBConsoleParamPromptEscRunsNothing(t *testing.T) {
	s := newEditSession(5)
	d := editDriver(t, 120, 30, s, "")
	consoleRun(t, d, "SELECT * FROM users WHERE plan = :plan;", false)
	if !d.WaitForText("Query parameter 1 of 1: :plan", 2*time.Second) {
		t.Fatalf("no prompt:\n%s", ansi.Strip(d.Frame()))
	}
	d.Key("esc")
	time.Sleep(150 * time.Millisecond)
	for _, e := range s.log() {
		if strings.Contains(e, "plan") {
			t.Fatalf("Esc still ran %q", e)
		}
	}
}
