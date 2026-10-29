package main

import (
	"strings"
	"testing"
)

// ev is one `go test -json` line.
func ev(action, pkg, test, output string) string {
	s := `{"Action":"` + action + `","Package":"` + pkg + `"`
	if test != "" {
		s += `,"Test":"` + test + `"`
	}
	if output != "" {
		s += `,"Output":` + quote(output)
	}
	return s + "}\n"
}

func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

// Spec: a test that fails in one of several runs (-count=3) is reported
// once, with how many runs failed, and the log of the failing run only.
func TestFailuresFlakyOneOfThree(t *testing.T) {
	in := ev("run", "p/a", "TestX", "") + ev("output", "p/a", "TestX", "run one ok\n") + ev("pass", "p/a", "TestX", "") +
		ev("run", "p/a", "TestX", "") + ev("output", "p/a", "TestX", "boom here\n") + ev("fail", "p/a", "TestX", "") +
		ev("run", "p/a", "TestX", "") + ev("output", "p/a", "TestX", "run three ok\n") + ev("pass", "p/a", "TestX", "") +
		ev("run", "p/a", "TestOK", "") + ev("pass", "p/a", "TestOK", "") +
		ev("fail", "p/a", "", "")
	got, err := Failures(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d failures, want 1: %+v", len(got), got)
	}
	f := got[0]
	if f.Package != "p/a" || f.Test != "TestX" || f.Failed != 1 || f.Runs != 3 {
		t.Errorf("failure = %+v, want p/a TestX failed 1 of 3", f)
	}
	if log := strings.Join(f.Log, ""); !strings.Contains(log, "boom here") || strings.Contains(log, "ok") {
		t.Errorf("log = %q, want only the failing run", log)
	}
}

// Spec: a failing subtest is reported under its top-level test (one
// issue per test), and the log holds the subtest's lines.
func TestFailuresSubtestUnderParent(t *testing.T) {
	in := ev("run", "p/a", "TestP", "") + ev("run", "p/a", "TestP/sub", "") +
		ev("output", "p/a", "TestP/sub", "sub broke\n") + ev("fail", "p/a", "TestP/sub", "") +
		ev("fail", "p/a", "TestP", "") + ev("fail", "p/a", "", "")
	got, _ := Failures(strings.NewReader(in))
	if len(got) != 1 || got[0].Test != "TestP" || !strings.Contains(strings.Join(got[0].Log, ""), "sub broke") {
		t.Fatalf("got %+v, want one TestP failure with the subtest log", got)
	}
}

// Spec: a package that times out or panics ends with tests still
// running; those tests count as failed, with the package's last lines.
func TestFailuresTimeoutBlamesRunningTest(t *testing.T) {
	in := ev("run", "p/b", "TestDone", "") + ev("pass", "p/b", "TestDone", "") +
		ev("run", "p/b", "TestHang", "") + ev("output", "p/b", "TestHang", "waiting\n") +
		ev("output", "p/b", "", "panic: test timed out after 10m0s\n") +
		ev("fail", "p/b", "", "")
	got, _ := Failures(strings.NewReader(in))
	if len(got) != 1 || got[0].Test != "TestHang" {
		t.Fatalf("got %+v, want TestHang", got)
	}
	if log := strings.Join(got[0].Log, ""); !strings.Contains(log, "waiting") || !strings.Contains(log, "timed out") {
		t.Errorf("log = %q, want the test lines and the timeout", log)
	}
}

// Spec: a package that fails with no test to blame (build error, TestMain
// exit) is reported as the package itself.
func TestFailuresPackageOnly(t *testing.T) {
	in := ev("output", "p/c", "", "FAIL p/c [setup failed]\n") + ev("fail", "p/c", "", "")
	got, _ := Failures(strings.NewReader(in))
	if len(got) != 1 || got[0].Test != "" || got[0].Name() != "p/c" {
		t.Fatalf("got %+v, want the package p/c", got)
	}
}

// Spec: passing runs, skips and non-JSON lines (go vet notes, build
// output) give no failures; Name is the test name for issue titles.
func TestFailuresNoneAndName(t *testing.T) {
	in := "some plain line\n" + ev("run", "p/d", "TestS", "") + ev("skip", "p/d", "TestS", "") + ev("pass", "p/d", "", "")
	got, err := Failures(strings.NewReader(in))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %+v, %v; want none", got, err)
	}
	if n := (Failure{Package: "p/d", Test: "TestS"}).Name(); n != "TestS" {
		t.Errorf("Name = %q, want TestS", n)
	}
}

// Spec: Tail keeps the last n lines.
func TestTail(t *testing.T) {
	f := Failure{Log: []string{"1\n", "2\n", "3\n"}}
	if got := strings.Join(f.Tail(2), ""); got != "2\n3\n" {
		t.Errorf("Tail(2) = %q", got)
	}
	if got := len(f.Tail(10)); got != 3 {
		t.Errorf("Tail(10) has %d lines", got)
	}
}
