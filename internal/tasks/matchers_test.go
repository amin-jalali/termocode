package tasks

import (
	"reflect"
	"testing"
)

func TestParseLinks(t *testing.T) {
	cases := []struct {
		in   string
		want []Link
	}{
		{"lib/foo.dart:42", []Link{{Path: "lib/foo.dart", Line: 42, Start: 0, End: 15}}},
		{"at src/main.go:10:5: boom", []Link{{Path: "src/main.go", Line: 10, Col: 5, Start: 3, End: 19}}},
		{"    main_test.go:12: want 1", []Link{{Path: "main_test.go", Line: 12, Start: 4, End: 19}}},
		{"(pkg/a.rs:7)", []Link{{Path: "pkg/a.rs", Line: 7, Start: 1, End: 11}}},
		{"12:30:00 started", nil},
		{"version 1.2.3:4", nil},
		{"foo.txt:3", nil},
		{"x.go:0", nil},
		{"a/b.go:1 and c/d.py:2", []Link{
			{Path: "a/b.go", Line: 1, Start: 0, End: 8},
			{Path: "c/d.py", Line: 2, Start: 13, End: 21},
		}},
	}
	for _, c := range cases {
		if got := ParseLinks(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseLinks(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
	if l, ok := LinkAt("see a/b.go:3 now", 6); !ok || l.Path != "a/b.go" {
		t.Errorf("LinkAt hit = %+v %v", l, ok)
	}
	if _, ok := LinkAt("see a/b.go:3 now", 1); ok {
		t.Error("LinkAt miss returned a link")
	}
}

func TestResolvePath(t *testing.T) {
	if got := ResolvePath("./a/b.go", "/root"); got != "/root/a/b.go" {
		t.Errorf("got %q", got)
	}
	if got := ResolvePath("/abs/x.go", "/root"); got != "/abs/x.go" {
		t.Errorf("got %q", got)
	}
}

func TestMatchDisabled(t *testing.T) {
	if got := Match(nil, []string{"a.go:1:1: x"}); got != nil {
		t.Errorf("nil matchers: %v", got)
	}
	if got := Match([]string{"$none"}, []string{"a.go:1:1: x"}); got != nil {
		t.Errorf("$none: %v", got)
	}
}

func TestMatchFormats(t *testing.T) {
	lines := []string{
		"# termocode/x",
		"./main.go:5:2: undefined: foo",
		"\x1b[31mmain.c:3:10: warning: unused variable 'x'\x1b[0m",
		"    calc_test.go:12: got 1, want 2",
		"src/app.ts(4,7): error TS2322: Type 'string' is not assignable",
		"error[E0308]: mismatched types",
		"  --> src/main.rs:4:5",
		"warning: `x` (bin) generated 1 warning",
		"tests/test_a.py:10: AssertionError",
		"lib.go:7:1: note: declared here",
		"12:30:01 server started",
		"ok  \ttermocode/internal/tasks\t0.01s",
		"./main.go:5:2: undefined: foo",
	}
	got := Match([]string{"$go"}, lines)
	want := []Problem{
		{Path: "./main.go", Line: 5, Col: 2, Severity: SevError, Message: "undefined: foo"},
		{Path: "main.c", Line: 3, Col: 10, Severity: SevWarning, Message: "unused variable 'x'"},
		{Path: "calc_test.go", Line: 12, Col: 1, Severity: SevError, Message: "got 1, want 2"},
		{Path: "src/app.ts", Line: 4, Col: 7, Severity: SevError, Message: "TS2322: Type 'string' is not assignable"},
		{Path: "src/main.rs", Line: 4, Col: 5, Severity: SevError, Message: "mismatched types"},
		{Path: "tests/test_a.py", Line: 10, Col: 1, Severity: SevError, Message: "AssertionError"},
		{Path: "lib.go", Line: 7, Col: 1, Severity: SevInfo, Message: "declared here"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Match =\n%+v\nwant\n%+v", got, want)
	}
}
