package tests

import (
	"strconv"
	"strings"
	"time"
)

// TSQLtPackage is the Event.Package of every tSQLt result, so the test
// view groups SQL Server database tests under one node.
const TSQLtPackage = "tSQLt"

// TSQLtRunSQL returns the T-SQL batch that runs tSQLt tests and then
// reads their results: all tests (class ""), one test class, or one test
// in a class. tSQLt.Run raises an error when a test fails, so the run is
// wrapped in TRY/CATCH and the results always come from
// tSQLt.TestResult. Names are quoted as N” literals; a single test is
// passed as [class].[test] with "]" doubled.
func TSQLtRunSQL(class, test string) string {
	run := "EXEC tSQLt.RunAll;"
	switch {
	case class != "" && test != "":
		run = "EXEC tSQLt.Run " + nLiteral(bracket(class)+"."+bracket(test)) + ";"
	case class != "":
		run = "EXEC tSQLt.Run " + nLiteral(class) + ";"
	}
	return "BEGIN TRY " + run + " END TRY BEGIN CATCH END CATCH;\n" +
		"SELECT Class, TestCase, Result, Msg, TestStartTime, TestEndTime FROM tSQLt.TestResult ORDER BY Id;"
}

func nLiteral(s string) string { return "N" + sqlLiteral(s) }

func bracket(s string) string { return "[" + strings.ReplaceAll(s, "]", "]]") + "]" }

// TSQLtEvents turns tSQLt.TestResult rows (as text, with their column
// names in any case and order) into result events. Result "Success" is
// passed, "Skipped" skipped, "Failure" failed with Msg, and "Error"
// failed with "Error: "+Msg. Without a Result or TestCase column there
// are no events.
func TSQLtEvents(cols []string, rows [][]string) []Event {
	idx := map[string]int{}
	for i, c := range cols {
		idx[strings.ToLower(strings.TrimSpace(c))] = i
	}
	_, okR := idx["result"]
	_, okT := idx["testcase"]
	if !okR || !okT {
		return nil
	}
	cell := func(row []string, name string) string {
		i, ok := idx[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	evs := make([]Event, 0, len(rows))
	for n, row := range rows {
		test := cell(row, "testcase")
		if test == "" {
			test = "test " + strconv.Itoa(n+1)
		}
		e := Event{Kind: EventResult, Package: TSQLtPackage, Class: cell(row, "class"), Test: test}
		msg := cell(row, "msg")
		switch strings.ToLower(cell(row, "result")) {
		case "success":
			e.Status = StatusPassed
		case "skipped":
			e.Status = StatusSkipped
		case "error":
			e.Status, e.Message = StatusFailed, "Error: "+msg
		default: // "Failure" and anything unknown
			e.Status, e.Message = StatusFailed, msg
		}
		if a, b := sqlTime(cell(row, "teststarttime")), sqlTime(cell(row, "testendtime")); !a.IsZero() && b.After(a) {
			e.Duration = b.Sub(a)
		}
		evs = append(evs, e)
	}
	return evs
}

var sqlTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999 -07:00",
	time.RFC3339Nano,
}

// sqlTime parses a SQL Server datetime as text; zero when it cannot.
func sqlTime(s string) time.Time {
	for _, l := range sqlTimeLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
