package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All fixtures under testdata/ are recorded runner output — no test
// process is spawned here.

func feed(t *testing.T, p LineParser, fixture string) []Event {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	var evs []Event
	for _, ln := range strings.Split(string(data), "\n") {
		evs = append(evs, p.Line(ln)...)
	}
	return append(evs, p.Flush()...)
}

func results(evs []Event) map[string]Event {
	out := map[string]Event{}
	for _, e := range evs {
		if e.Kind == EventResult {
			out[e.Test] = e
		}
	}
	return out
}

func TestGoParserRecorded(t *testing.T) {
	evs := feed(t, NewLineParser(FrameworkGo), "go_test.json")
	res := results(evs)
	if len(res) != 4 {
		t.Fatalf("results = %d (%v), want 4 top-level tests", len(res), res)
	}
	if res["TestAdd"].Status != StatusPassed || res["TestSkipped"].Status != StatusSkipped {
		t.Fatalf("TestAdd/TestSkipped = %v/%v", res["TestAdd"].Status, res["TestSkipped"].Status)
	}
	f := res["TestAddFails"]
	if f.Status != StatusFailed || f.Message != "calc_test.go:16: Add(2, 2) = 4, want 5" {
		t.Fatalf("TestAddFails = %v %q", f.Status, f.Message)
	}
	if f.FailFile != "calc_test.go" || f.FailLine != 16 {
		t.Fatalf("TestAddFails location = %s:%d", f.FailFile, f.FailLine)
	}
	// Subtest failure rolls up into the parent's message and location.
	tb := res["TestTable"]
	if tb.Status != StatusFailed || !strings.Contains(tb.Message, "sub failed") || tb.FailLine != 22 {
		t.Fatalf("TestTable = %v %q line %d", tb.Status, tb.Message, tb.FailLine)
	}
	// The build failure becomes a package event with the compiler error.
	var pkgFail *Event
	for i := range evs {
		if evs[i].Kind == EventPackage && evs[i].Status == StatusFailed {
			pkgFail = &evs[i]
		}
	}
	if pkgFail == nil || pkgFail.Package != "example.com/demo/broken" {
		t.Fatalf("missing build-failure package event: %+v", pkgFail)
	}
	if !strings.Contains(pkgFail.Message, "undefined: undefinedThing") || pkgFail.FailFile != "broken/broken_test.go" || pkgFail.FailLine != 5 {
		t.Fatalf("package event = %q at %s:%d", pkgFail.Message, pkgFail.FailFile, pkgFail.FailLine)
	}
}

func TestGoParserNonJSONIsOutput(t *testing.T) {
	evs := NewGoParser().Line("# some stderr line")
	if len(evs) != 1 || evs[0].Kind != EventOutput {
		t.Fatalf("got %+v", evs)
	}
}

func TestPytestParserRecorded(t *testing.T) {
	evs := feed(t, NewLineParser(FrameworkPytest), "pytest_v.txt")
	var got []string
	for _, e := range evs {
		if e.Kind == EventResult {
			got = append(got, e.File+"::"+e.Test+"="+map[Status]string{StatusPassed: "P", StatusFailed: "F"}[e.Status])
		}
	}
	eq(t, got, []string{
		"tests/test_math.py::test_add=P",
		"tests/test_math.py::test_div=F",
		"tests/test_math.py::TestOps::test_mul=P",
		"tests/test_math.py::test_param=P",
		"tests/test_math.py::test_param=F",
	})
}

func TestParseJUnitXMLRecorded(t *testing.T) {
	data, err := os.ReadFile("testdata/pytest_junit.xml")
	if err != nil {
		t.Fatal(err)
	}
	evs, err := ParseJUnitXML(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 6 {
		t.Fatalf("events = %d, want 6", len(evs))
	}
	div := evs[1]
	if div.Test != "test_div" || div.Status != StatusFailed || div.Message != "assert 1.0 == 2" {
		t.Fatalf("test_div = %+v", div)
	}
	if div.FailFile != "tests/test_math.py" || div.FailLine != 7 {
		t.Fatalf("test_div location = %s:%d", div.FailFile, div.FailLine)
	}
	if evs[2].Class != "tests.test_math.TestOps" {
		t.Fatalf("class = %q", evs[2].Class)
	}
	if evs[4].Test != "test_param" || evs[4].FailLine != 16 {
		t.Fatalf("param case = %+v", evs[4])
	}
	if evs[5].Status != StatusSkipped || evs[5].Message != "not ready" {
		t.Fatalf("skipped = %+v", evs[5])
	}
}

func TestCargoParserRecorded(t *testing.T) {
	evs := feed(t, NewLineParser(FrameworkCargo), "cargo_test.txt")
	var fails []Event
	statuses := map[string]Status{}
	for _, e := range evs {
		if e.Kind != EventResult {
			continue
		}
		statuses[e.Test] = MergeStatus(statuses[e.Test], e.Status)
		if e.Status == StatusFailed {
			fails = append(fails, e)
		}
	}
	if statuses["tests::it_adds"] != StatusPassed || statuses["tests::slow_one"] != StatusSkipped || statuses["smoke"] != StatusPassed {
		t.Fatalf("statuses = %v", statuses)
	}
	// One FAILED line + one failure-detail event.
	if len(fails) != 2 {
		t.Fatalf("fail events = %d, want 2", len(fails))
	}
	d := fails[1]
	if d.FailFile != "src/lib.rs" || d.FailLine != 17 || !strings.Contains(d.Message, "left: 4") {
		t.Fatalf("detail = %s:%d %q", d.FailFile, d.FailLine, d.Message)
	}
	if d.Package != "src/lib.rs" {
		t.Fatalf("binary source = %q", d.Package)
	}
}

func TestCargoPanicOldFormat(t *testing.T) {
	p := &CargoParser{}
	p.Line("---- t stdout ----")
	p.Line("thread 't' panicked at 'boom', src/x.rs:3:5")
	evs := p.Flush()
	if len(evs) != 1 || evs[0].FailFile != "src/x.rs" || evs[0].FailLine != 3 {
		t.Fatalf("got %+v", evs)
	}
}

func TestParseJestJSONRecorded(t *testing.T) {
	data, err := os.ReadFile("testdata/jest.json")
	if err != nil {
		t.Fatal(err)
	}
	evs, err := ParseJestJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 5 {
		t.Fatalf("events = %d, want 5", len(evs))
	}
	f := evs[1]
	if f.Test != "math › nested › subtracts" || f.Status != StatusFailed {
		t.Fatalf("failed case = %+v", f)
	}
	if f.FailFile != "/work/app/src/math.test.js" || f.FailLine != 9 {
		t.Fatalf("location = %s:%d", f.FailFile, f.FailLine)
	}
	if strings.Contains(f.Message, "\x1b") || !strings.Contains(f.Message, "Expected: 5") {
		t.Fatalf("message = %q", f.Message)
	}
	if evs[2].Status != StatusSkipped {
		t.Fatalf("pending → skipped, got %v", evs[2].Status)
	}
	if evs[4].Kind != EventPackage || !strings.Contains(evs[4].Message, "Cannot find module") {
		t.Fatalf("suite failure = %+v", evs[4])
	}
}

func TestMergeStatus(t *testing.T) {
	if MergeStatus(StatusRunning, StatusPassed) != StatusPassed {
		t.Fatal("running → passed")
	}
	if MergeStatus(StatusFailed, StatusPassed) != StatusFailed {
		t.Fatal("failed must stick")
	}
	if MergeStatus(StatusSkipped, StatusPassed) != StatusPassed {
		t.Fatal("passed beats skipped")
	}
}
