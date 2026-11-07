package tests

import (
	"strings"
	"testing"
	"time"
)

func TestTSQLtRunSQLAll(t *testing.T) {
	got := TSQLtRunSQL("", "")
	for _, want := range []string{"BEGIN TRY", "EXEC tSQLt.RunAll;", "END CATCH", "FROM tSQLt.TestResult", "ORDER BY Id"} {
		if !strings.Contains(got, want) {
			t.Errorf("RunAll SQL lacks %q:\n%s", want, got)
		}
	}
}

func TestTSQLtRunSQLClassAndTestQuoted(t *testing.T) {
	cls := TSQLtRunSQL("Order'Tests", "")
	if !strings.Contains(cls, "EXEC tSQLt.Run N'Order''Tests';") {
		t.Errorf("class run:\n%s", cls)
	}
	one := TSQLtRunSQL("OrderTests", "test total ]is[ sum")
	if !strings.Contains(one, "EXEC tSQLt.Run N'[OrderTests].[test total ]]is[ sum]';") {
		t.Errorf("single test run:\n%s", one)
	}
	if strings.Contains(TSQLtRunSQL("", "test x"), "tSQLt.Run N") {
		t.Error("a test name without a class must run all")
	}
}

func TestTSQLtEventsFromResultRows(t *testing.T) {
	cols := []string{"Class", "TestCase", "Result", "Msg", "TestStartTime", "TestEndTime"}
	rows := [][]string{
		{"OrderTests", "test total", "Success", "", "2026-11-07 10:00:00.000", "2026-11-07 10:00:00.250"},
		{"OrderTests", "test tax", "Failure", "Expected: <7> but was: <6>", "", ""},
		{"UserTests", "test login", "Error", "Invalid object name 'dbo.Users'.", "", ""},
		{"UserTests", "test later", "Skipped", "", "", ""},
	}
	evs := TSQLtEvents(cols, rows)
	if len(evs) != 4 {
		t.Fatalf("events = %d, want 4: %+v", len(evs), evs)
	}
	want := []struct {
		class, test string
		st          Status
	}{
		{"OrderTests", "test total", StatusPassed},
		{"OrderTests", "test tax", StatusFailed},
		{"UserTests", "test login", StatusFailed},
		{"UserTests", "test later", StatusSkipped},
	}
	for i, w := range want {
		e := evs[i]
		if e.Kind != EventResult || e.Package != TSQLtPackage || e.Class != w.class || e.Test != w.test || e.Status != w.st {
			t.Errorf("event %d = %+v, want %s/%s %v", i, e, w.class, w.test, w.st)
		}
	}
	if evs[0].Duration != 250*time.Millisecond {
		t.Errorf("duration = %v, want 250ms", evs[0].Duration)
	}
	if evs[1].Message != "Expected: <7> but was: <6>" {
		t.Errorf("failure message = %q", evs[1].Message)
	}
	if !strings.HasPrefix(evs[2].Message, "Error: ") {
		t.Errorf("error message = %q, want an Error: prefix", evs[2].Message)
	}
}

// Column order and case come from the server; missing columns do not panic.
func TestTSQLtEventsColumnOrderAndMissing(t *testing.T) {
	evs := TSQLtEvents([]string{"result", "testcase", "class"}, [][]string{{"Success", "t1", "C"}, {"Success"}})
	if len(evs) != 2 || evs[0].Test != "t1" || evs[0].Class != "C" || evs[0].Status != StatusPassed {
		t.Fatalf("events = %+v", evs)
	}
	if evs[1].Test == "" {
		t.Errorf("a short row has no test name: %+v", evs[1])
	}
	if got := TSQLtEvents([]string{"Foo"}, [][]string{{"x"}}); len(got) != 0 {
		t.Errorf("rows without Result/TestCase columns gave events: %+v", got)
	}
}
