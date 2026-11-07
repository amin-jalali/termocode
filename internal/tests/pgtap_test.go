package tests

import (
	"strings"
	"testing"
)

func feedPgTAP(t *testing.T, out string) []Event {
	t.Helper()
	p := NewPgTAPParser()
	var evs []Event
	for _, ln := range strings.Split(out, "\n") {
		evs = append(evs, p.Line(ln)...)
	}
	return append(evs, p.Flush()...)
}

func pgtapResults(evs []Event) []Event {
	var out []Event
	for _, e := range evs {
		if e.Kind == EventResult {
			out = append(out, e)
		}
	}
	return out
}

// runtests() output: one subtest per test function, nested TAP inside.
func TestPgTAPParserRuntestsSubtests(t *testing.T) {
	out := `# Subtest: public.test_users()
    ok 1 - users table exists
    not ok 2 - users has a primary key
    # Failed test 2: "users has a primary key"
    #         have: none
    #         want: id
    1..2
    # Looks like you failed 1 test of 2
not ok 1 - public.test_users
# Subtest: public.test_orders()
    ok 1 - orders table exists
    1..1
ok 2 - public.test_orders
1..2`
	res := pgtapResults(feedPgTAP(t, out))
	if len(res) != 2 {
		t.Fatalf("want 2 results, got %d: %+v", len(res), res)
	}
	a, b := res[0], res[1]
	if a.Package != PgTAPPackage || a.Test != "public.test_users" || a.Status != StatusFailed {
		t.Fatalf("first result = %+v", a)
	}
	if !strings.Contains(a.Message, "users has a primary key") || !strings.Contains(a.Message, "have: none") {
		t.Fatalf("failure message missing details: %q", a.Message)
	}
	if strings.Contains(a.Message, "users table exists") {
		t.Fatalf("message should hold only the failing assertion: %q", a.Message)
	}
	if len(a.Output) == 0 {
		t.Fatal("subtest output lines should be kept")
	}
	if b.Test != "public.test_orders" || b.Status != StatusPassed || b.Message != "" {
		t.Fatalf("second result = %+v", b)
	}
}

// Plain TAP (a test script run with psql): every assertion is one result.
func TestPgTAPParserPlainAssertions(t *testing.T) {
	out := `1..4
ok 1 - schema public exists
not ok 2 - function f exists
# Failed test 2: "function f exists"
ok 3 - slow check # SKIP no data
not ok 4 - known bug # TODO fix later`
	res := pgtapResults(feedPgTAP(t, out))
	if len(res) != 4 {
		t.Fatalf("want 4 results, got %d: %+v", len(res), res)
	}
	want := []struct {
		name string
		st   Status
	}{
		{"schema public exists", StatusPassed},
		{"function f exists", StatusFailed},
		{"slow check", StatusSkipped},
		{"known bug", StatusSkipped},
	}
	for i, w := range want {
		if res[i].Test != w.name || res[i].Status != w.st {
			t.Fatalf("result %d = %q/%v, want %q/%v", i, res[i].Test, res[i].Status, w.name, w.st)
		}
	}
	if !strings.Contains(res[1].Message, `Failed test 2`) {
		t.Fatalf("diagnostic not attached to the failing test: %q", res[1].Message)
	}
}

// psql decorations (header, row count) and empty lines are output only;
// an assertion without a description gets a numbered name.
func TestPgTAPParserIgnoresPsqlNoise(t *testing.T) {
	out := `             runtests
----------------------------------
 ok 1
 not ok 2

(2 rows)`
	evs := feedPgTAP(t, out)
	res := pgtapResults(evs)
	if len(res) != 2 || res[0].Test != "test 1" || res[0].Status != StatusPassed || res[1].Status != StatusFailed {
		t.Fatalf("results = %+v", res)
	}
	var outLines int
	for _, e := range evs {
		if e.Kind == EventOutput {
			outLines++
		}
	}
	if outLines == 0 {
		t.Fatal("raw lines should also reach the run log")
	}
}

// A subtest cut off by a crash (no closing line) is reported as failed at Flush.
func TestPgTAPParserUnfinishedSubtestFails(t *testing.T) {
	out := `# Subtest: public.test_crash()
    ok 1 - first`
	res := pgtapResults(feedPgTAP(t, out))
	if len(res) != 1 || res[0].Test != "public.test_crash" || res[0].Status != StatusFailed {
		t.Fatalf("results = %+v", res)
	}
}

func TestPgTAPRunSQL(t *testing.T) {
	cases := []struct{ schema, pattern, want string }{
		{"", "", "SELECT * FROM runtests();"},
		{"tests", "", "SELECT * FROM runtests('tests'::name);"},
		{"", "^test_", "SELECT * FROM runtests('^test_'::text);"},
		{"tests", "^it's", "SELECT * FROM runtests('tests'::name, '^it''s'::text);"},
	}
	for _, c := range cases {
		if got := PgTAPRunSQL(c.schema, c.pattern); got != c.want {
			t.Fatalf("PgTAPRunSQL(%q,%q) = %q, want %q", c.schema, c.pattern, got, c.want)
		}
	}
}

func FuzzPgTAPParser(f *testing.F) {
	f.Add("# Subtest: a()\n    ok 1 - x\nnot ok 1 - a\n1..1")
	f.Add("ok 1 - x # SKIP\nnot ok")
	f.Fuzz(func(t *testing.T, s string) {
		_ = feedPgTAP(t, s)
	})
}
